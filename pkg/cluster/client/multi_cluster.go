package client

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/llm-d/fleet-llm-d/pkg/placement/solver"
)

// ClusterRegistration holds the details needed to register a cluster with
// the multi-cluster control plane.
type ClusterRegistration struct {
	ID             string
	Name           string
	Region         string
	KubeconfigPath string
	Labels         map[string]string
}

// MultiClusterClient manages the lifecycle of registered clusters and
// provides access to per-cluster API clients.
type MultiClusterClient interface {
	RegisterCluster(ctx context.Context, cluster ClusterRegistration) error
	DeregisterCluster(ctx context.Context, clusterID string) error
	ListClusters(ctx context.Context) ([]solver.ClusterInfo, error)
	GetClusterClient(ctx context.Context, clusterID string) (interface{}, error)
}

// ResourceApplier is an optional capability implemented by cluster clients
// that can reconcile product-owned Kubernetes resources on member clusters.
type ResourceApplier interface {
	ApplyResource(ctx context.Context, clusterID string, resource []byte) error
}

// ResourceReader is an optional capability used to ingest status from a
// lifecycle owner after applying its resource. apiPath must be an absolute
// Kubernetes API path assembled from operator-owned resource identity.
type ResourceReader interface {
	GetResource(ctx context.Context, clusterID, apiPath string) ([]byte, error)
}

// clusterRegistry is the shared, package-level registry used by all
// defaultMultiClusterClient instances so that clusters registered through
// one client instance are visible from another.
var (
	registryMu sync.RWMutex
	registry   = make(map[string]ClusterRegistration)
)

// defaultMultiClusterClient is the built-in implementation of
// MultiClusterClient.
type defaultMultiClusterClient struct{}

// NewMultiClusterClient returns a new MultiClusterClient using the default
// implementation.
func NewMultiClusterClient() MultiClusterClient {
	return &defaultMultiClusterClient{}
}

// NormalizeClusterRegistration fills derived fields and validates the minimum
// required identity for a cluster registration.
func NormalizeClusterRegistration(cluster ClusterRegistration) (ClusterRegistration, error) {
	if strings.TrimSpace(cluster.Name) == "" {
		return cluster, fmt.Errorf("cluster name must not be empty")
	}
	if strings.TrimSpace(cluster.ID) == "" {
		cluster.ID = stableClusterID(cluster.Name)
	}
	if cluster.ID == "" {
		return cluster, fmt.Errorf("cluster ID must not be empty")
	}
	return cluster, nil
}

func stableClusterID(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// RegisterCluster stores a ClusterRegistration in the shared registry.
func (c *defaultMultiClusterClient) RegisterCluster(ctx context.Context, cluster ClusterRegistration) error {
	normalized, err := NormalizeClusterRegistration(cluster)
	if err != nil {
		return err
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[normalized.ID]; exists {
		return fmt.Errorf("cluster %q already exists", normalized.ID)
	}
	registry[normalized.ID] = normalized
	return nil
}

// DeregisterCluster removes a cluster from the shared registry.
func (c *defaultMultiClusterClient) DeregisterCluster(ctx context.Context, clusterID string) error {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, ok := registry[clusterID]; !ok {
		return fmt.Errorf("cluster %q not found", clusterID)
	}
	delete(registry, clusterID)
	return nil
}

// ListClusters returns all registered clusters as ClusterInfo values.
func (c *defaultMultiClusterClient) ListClusters(ctx context.Context) ([]solver.ClusterInfo, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	clusters := make([]solver.ClusterInfo, 0, len(registry))
	for _, reg := range registry {
		clusters = append(clusters, solver.ClusterInfo{
			ID:     reg.ID,
			Name:   reg.Name,
			Region: reg.Region,
			Labels: reg.Labels,
		})
	}
	return clusters, nil
}

// GetClusterClient returns the Kubernetes client for the given cluster.
// Currently returns an error because no real K8s client is configured.
func (c *defaultMultiClusterClient) GetClusterClient(ctx context.Context, clusterID string) (interface{}, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if _, ok := registry[clusterID]; !ok {
		return nil, fmt.Errorf("cluster %q not found", clusterID)
	}
	return nil, fmt.Errorf("kubernetes client not configured for cluster %q", clusterID)
}
