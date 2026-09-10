package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// KubernetesConfigMapPublisher atomically replaces a ConfigMap's data map.
// Kubernetes projected volumes expose each update through an atomic symlink
// swap, which the Router's watched file discovery plugin supports.
type KubernetesConfigMapPublisher struct {
	apiURL    string
	namespace string
	name      string
	token     string
	tokenFile string
	client    *http.Client
}

func NewKubernetesConfigMapPublisher(apiURL, namespace, name, token string, client *http.Client) (*KubernetesConfigMapPublisher, error) {
	if _, err := url.ParseRequestURI(apiURL); err != nil || !strings.HasPrefix(apiURL, "https://") && !strings.HasPrefix(apiURL, "http://") {
		return nil, fmt.Errorf("invalid Kubernetes API URL")
	}
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("ConfigMap namespace and name are required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &KubernetesConfigMapPublisher{apiURL: strings.TrimRight(apiURL, "/"), namespace: namespace, name: name, token: token, client: client}, nil
}

// NewKubernetesConfigMapPublisherFromTokenFile creates a publisher that reads
// its bearer token before every API request. Projected service-account tokens
// rotate in place, so caching their contents eventually produces 401s.
func NewKubernetesConfigMapPublisherFromTokenFile(apiURL, namespace, name, tokenFile string, client *http.Client) (*KubernetesConfigMapPublisher, error) {
	if strings.TrimSpace(tokenFile) == "" {
		return nil, fmt.Errorf("Kubernetes token file is required")
	}
	publisher, err := NewKubernetesConfigMapPublisher(apiURL, namespace, name, "", client)
	if err != nil {
		return nil, err
	}
	publisher.tokenFile = tokenFile
	return publisher, nil
}

func (p *KubernetesConfigMapPublisher) Publish(ctx context.Context, files map[string][]byte) error {
	data := make(map[string]string, len(files))
	totalBytes := 0
	for name, contents := range files {
		data[name] = string(contents)
		totalBytes += len(name) + len(contents)
	}
	// Kubernetes limits a ConfigMap object to 1 MiB. Leave headroom for
	// metadata and JSON encoding rather than relying on a remote 413 response.
	if totalBytes > 900*1024 {
		return fmt.Errorf("Router endpoint files exceed the 900 KiB ConfigMap safety limit")
	}
	body, err := json.Marshal([]map[string]interface{}{{"op": "add", "path": "/data", "value": data}})
	if err != nil {
		return fmt.Errorf("marshal Router ConfigMap patch: %w", err)
	}
	endpoint := fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps/%s", p.apiURL, url.PathEscape(p.namespace), url.PathEscape(p.name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json-patch+json")
	token := p.token
	if p.tokenFile != "" {
		contents, readErr := os.ReadFile(p.tokenFile)
		if readErr != nil {
			return fmt.Errorf("read Kubernetes bearer token: %w", readErr)
		}
		token = strings.TrimSpace(string(contents))
		if token == "" {
			return fmt.Errorf("read Kubernetes bearer token: token file is empty")
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("publish Router ConfigMap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("publish Router ConfigMap: Kubernetes API returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
