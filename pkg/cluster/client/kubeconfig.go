package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/llm-d/fleet-llm-d/pkg/placement/solver"
	"github.com/llm-d/fleet-llm-d/pkg/tlsutil"
)

// ClusterConnection holds the runtime connection state for a registered cluster.
type ClusterConnection struct {
	Registration ClusterRegistration
	APIServer    string
	Token        string
	CACert       string // path to CA cert, if any
	HTTPClient   *http.Client
	Healthy      bool
	LastCheck    time.Time
}

// KubeconfigClusterClient manages connections to multiple Kubernetes clusters
// using kubeconfig files or direct API server URLs + tokens.
type KubeconfigClusterClient struct {
	mu       sync.RWMutex
	clusters map[string]*ClusterConnection
	tlsOpts  tlsutil.TLSOptions
}

// NewKubeconfigClusterClient returns a new KubeconfigClusterClient with an
// empty cluster map. An optional tlsutil.TLSOptions can be passed to configure
// TLS behavior; when omitted, certificate verification uses the system trust store.
func NewKubeconfigClusterClient(tlsOpts ...tlsutil.TLSOptions) *KubeconfigClusterClient {
	opts := tlsutil.TLSOptions{}
	if len(tlsOpts) > 0 {
		opts = tlsOpts[0]
	}
	return &KubeconfigClusterClient{
		clusters: make(map[string]*ClusterConnection),
		tlsOpts:  opts,
	}
}

// kubeconfigFile represents a minimal kubeconfig structure for JSON parsing.
type kubeconfigFile struct {
	Clusters []kubeconfigCluster `json:"clusters"`
	Users    []kubeconfigUser    `json:"users"`
}

type kubeconfigCluster struct {
	Cluster struct {
		Server               string `json:"server"`
		CertificateAuthority string `json:"certificate-authority,omitempty"`
	} `json:"cluster"`
}

type kubeconfigUser struct {
	User struct {
		Token string `json:"token"`
	} `json:"user"`
}

// parseKubeconfig reads a JSON-format kubeconfig file and extracts the API
// server URL and bearer token from the first cluster and user entries.
func parseKubeconfig(path string) (server, token, caCert string, err error) {
	// #nosec G304 -- kubeconfig path is operator-supplied configuration,
	// never derived from request input.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", fmt.Errorf("reading kubeconfig: %w", err)
	}

	var kc kubeconfigFile
	if err := json.Unmarshal(data, &kc); err != nil {
		return "", "", "", fmt.Errorf("parsing kubeconfig (JSON format required): %w", err)
	}

	if len(kc.Clusters) == 0 {
		return "", "", "", fmt.Errorf("kubeconfig contains no cluster entries")
	}
	if len(kc.Users) == 0 {
		return "", "", "", fmt.Errorf("kubeconfig contains no user entries")
	}

	server = kc.Clusters[0].Cluster.Server
	token = kc.Users[0].User.Token
	caCert = kc.Clusters[0].Cluster.CertificateAuthority
	if caCert != "" && !filepath.IsAbs(caCert) {
		caCert = filepath.Join(filepath.Dir(path), caCert)
	}
	return server, token, caCert, nil
}

// RegisterCluster registers a cluster with the client. If the registration
// includes a KubeconfigPath, the file is parsed to extract the API server URL
// and bearer token. Clusters without a kubeconfig can still be registered but
// will lack API connectivity until the server and token are set directly.
func (c *KubeconfigClusterClient) RegisterCluster(ctx context.Context, reg ClusterRegistration) error {
	if reg.ID == "" {
		return fmt.Errorf("cluster ID must not be empty")
	}

	var server, token, caCert string

	if reg.KubeconfigPath != "" {
		var err error
		server, token, caCert, err = parseKubeconfig(reg.KubeconfigPath)
		if err != nil {
			return fmt.Errorf("loading kubeconfig for cluster %q: %w", reg.ID, err)
		}
	}

	tlsOpts := c.tlsOpts
	if caCert != "" {
		tlsOpts.CAPath = caCert
	}
	tlsCfg, tlsErr := tlsutil.NewTLSConfig(tlsOpts)
	if tlsErr != nil {
		return fmt.Errorf("building TLS trust for cluster %q: %w", reg.ID, tlsErr)
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	conn := &ClusterConnection{
		Registration: reg,
		APIServer:    server,
		Token:        token,
		CACert:       caCert,
		HTTPClient:   httpClient,
		Healthy:      false,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clusters[reg.ID] = conn

	return nil
}

// DeregisterCluster removes the cluster with the given ID from the client.
func (c *KubeconfigClusterClient) DeregisterCluster(ctx context.Context, clusterID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.clusters[clusterID]; !ok {
		return fmt.Errorf("cluster %q not found", clusterID)
	}
	delete(c.clusters, clusterID)
	return nil
}

// ListClusters returns all registered clusters as solver.ClusterInfo values.
func (c *KubeconfigClusterClient) ListClusters(ctx context.Context) ([]solver.ClusterInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clusters := make([]solver.ClusterInfo, 0, len(c.clusters))
	for _, conn := range c.clusters {
		clusters = append(clusters, solver.ClusterInfo{
			ID:     conn.Registration.ID,
			Name:   conn.Registration.Name,
			Region: conn.Registration.Region,
			Labels: conn.Registration.Labels,
		})
	}
	return clusters, nil
}

// GetClusterClient returns the *ClusterConnection for the given cluster ID.
func (c *KubeconfigClusterClient) GetClusterClient(ctx context.Context, clusterID string) (interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	conn, ok := c.clusters[clusterID]
	if !ok {
		return nil, fmt.Errorf("cluster %q not found", clusterID)
	}
	return conn, nil
}

// resourceMeta is used to extract identifying fields from a Kubernetes resource
// JSON blob for URL construction.
type resourceMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

// ApplyResource reconciles a JSON-encoded Kubernetes resource using
// server-side apply. The fixed field manager and operator-owned resource
// identity make create and update idempotent without copying resourceVersion.
func (c *KubeconfigClusterClient) ApplyResource(ctx context.Context, clusterID string, resource []byte) error {
	c.mu.RLock()
	conn, ok := c.clusters[clusterID]
	c.mu.RUnlock()

	if !ok {
		return fmt.Errorf("cluster %q not found", clusterID)
	}
	if conn.APIServer == "" {
		return fmt.Errorf("no API server configured for cluster %q", clusterID)
	}

	var meta resourceMeta
	if err := json.Unmarshal(resource, &meta); err != nil {
		return fmt.Errorf("parsing resource metadata: %w", err)
	}

	apiPath := buildAPIPath(meta)
	url := conn.APIServer + apiPath + "?fieldManager=llm-d-fleet&force=false"

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(resource))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/apply-patch+yaml")
	if conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.Token)
	}

	resp, err := conn.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("applying resource to cluster %q: %w", clusterID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		return fmt.Errorf("API server returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildAPIPath constructs a simplified Kubernetes API path from resource
// metadata. It handles core API resources (apiVersion "v1") and custom or
// extended API groups.
func buildAPIPath(meta resourceMeta) string {
	var base string
	if meta.APIVersion == "v1" {
		base = "/api/v1"
	} else {
		base = "/apis/" + meta.APIVersion
	}

	kind := pluralise(meta.Kind)

	if meta.Metadata.Namespace != "" {
		return fmt.Sprintf("%s/namespaces/%s/%s/%s", base, meta.Metadata.Namespace, kind, meta.Metadata.Name)
	}
	return fmt.Sprintf("%s/%s/%s", base, kind, meta.Metadata.Name)
}

// pluralise applies a naive lowercase-and-append-s rule for mapping Kubernetes
// Kind names to resource path segments.
func pluralise(kind string) string {
	if kind == "" {
		return ""
	}
	lower := toLower(kind)
	// Handle a few common irregular K8s plurals.
	switch lower {
	case "ingress":
		return "ingresses"
	case "endpoints":
		return "endpoints"
	default:
		return lower + "s"
	}
}

// toLower is a simple ASCII-lowercase helper to avoid importing strings.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// GetResource performs a GET request against the cluster's API server at the
// given API path and returns the raw response body.
func (c *KubeconfigClusterClient) GetResource(ctx context.Context, clusterID, apiPath string) ([]byte, error) {
	c.mu.RLock()
	conn, ok := c.clusters[clusterID]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("cluster %q not found", clusterID)
	}
	if conn.APIServer == "" {
		return nil, fmt.Errorf("no API server configured for cluster %q", clusterID)
	}

	url := conn.APIServer + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.Token)
	}

	resp, err := conn.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting resource from cluster %q: %w", clusterID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response from cluster %q: %w", clusterID, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cluster %q returned HTTP %d: %s", clusterID, resp.StatusCode, string(body))
	}

	return body, nil
}

// HealthCheck probes the /healthz endpoint of the cluster's API server and
// updates the connection's Healthy flag and LastCheck timestamp.
func (c *KubeconfigClusterClient) HealthCheck(ctx context.Context, clusterID string) error {
	c.mu.RLock()
	conn, ok := c.clusters[clusterID]
	c.mu.RUnlock()

	if !ok {
		return fmt.Errorf("cluster %q not found", clusterID)
	}
	if conn.APIServer == "" {
		return fmt.Errorf("no API server configured for cluster %q", clusterID)
	}

	url := conn.APIServer + "/healthz"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating health check request: %w", err)
	}
	if conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.Token)
	}

	resp, err := conn.HTTPClient.Do(req)
	if err != nil {
		c.mu.Lock()
		conn.Healthy = false
		conn.LastCheck = time.Now()
		c.mu.Unlock()
		return fmt.Errorf("health check failed for cluster %q: %w", clusterID, err)
	}
	defer resp.Body.Close()

	c.mu.Lock()
	conn.LastCheck = time.Now()
	if resp.StatusCode == http.StatusOK {
		conn.Healthy = true
		c.mu.Unlock()
		return nil
	}
	conn.Healthy = false
	c.mu.Unlock()

	return fmt.Errorf("health check returned status %d for cluster %q", resp.StatusCode, clusterID)
}
