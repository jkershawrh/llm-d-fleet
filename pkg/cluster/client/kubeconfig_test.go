package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterCluster_WithAPIServer(t *testing.T) {
	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "cluster-1",
		Name:   "us-east-prod",
		Region: "us-east-1",
		Labels: map[string]string{"env": "production"},
	})
	if err != nil {
		t.Fatalf("RegisterCluster: unexpected error: %v", err)
	}

	clusters, err := c.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: unexpected error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if clusters[0].ID != "cluster-1" {
		t.Errorf("expected cluster ID %q, got %q", "cluster-1", clusters[0].ID)
	}
	if clusters[0].Name != "us-east-prod" {
		t.Errorf("expected cluster Name %q, got %q", "us-east-prod", clusters[0].Name)
	}
	if clusters[0].Region != "us-east-1" {
		t.Errorf("expected cluster Region %q, got %q", "us-east-1", clusters[0].Region)
	}
	if clusters[0].Labels["env"] != "production" {
		t.Errorf("expected label env=production, got %q", clusters[0].Labels["env"])
	}
}

func TestRegisterCluster_EmptyID(t *testing.T) {
	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		Name: "no-id-cluster",
	})
	if err == nil {
		t.Fatal("expected error for empty cluster ID, got nil")
	}
}

func TestHealthCheck_Healthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "test-1",
		Name:   "health-test",
		Region: "us-west-2",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	// Set the APIServer directly via the internal map.
	c.mu.Lock()
	c.clusters["test-1"].APIServer = ts.URL
	c.mu.Unlock()

	err = c.HealthCheck(ctx, "test-1")
	if err != nil {
		t.Fatalf("HealthCheck: unexpected error: %v", err)
	}

	c.mu.RLock()
	healthy := c.clusters["test-1"].Healthy
	lastCheck := c.clusters["test-1"].LastCheck
	c.mu.RUnlock()

	if !healthy {
		t.Error("expected cluster to be Healthy after successful health check")
	}
	if lastCheck.IsZero() {
		t.Error("expected LastCheck to be updated")
	}
}

func TestHealthCheck_Unreachable(t *testing.T) {
	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "test-2",
		Name:   "unreachable-test",
		Region: "eu-west-1",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	// Point to an unreachable address.
	c.mu.Lock()
	c.clusters["test-2"].APIServer = "http://127.0.0.1:1"
	c.mu.Unlock()

	err = c.HealthCheck(ctx, "test-2")
	if err == nil {
		t.Fatal("expected error for unreachable health check, got nil")
	}
}

func TestApplyResource(t *testing.T) {
	var receivedBody string
	var receivedContentType string
	var receivedAuth string
	var receivedMethod string
	var receivedQuery string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedQuery = r.URL.RawQuery
		receivedContentType = r.Header.Get("Content-Type")
		receivedAuth = r.Header.Get("Authorization")
		// Read the request body for verification.
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		resp, _ := json.Marshal(map[string]string{"status": "ok"})
		w.WriteHeader(http.StatusOK)
		w.Write(resp)
	}))
	defer ts.Close()

	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "apply-test",
		Name:   "apply-cluster",
		Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	c.mu.Lock()
	c.clusters["apply-test"].APIServer = ts.URL
	c.clusters["apply-test"].Token = "test-token-123"
	c.mu.Unlock()

	resource := []byte(`{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {
			"name": "test-config",
			"namespace": "default"
		},
		"data": {
			"key": "value"
		}
	}`)

	err = c.ApplyResource(ctx, "apply-test", resource)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected error: %v", err)
	}

	if receivedMethod != http.MethodPatch {
		t.Errorf("expected PATCH method, got %q", receivedMethod)
	}
	if receivedContentType != "application/apply-patch+yaml" {
		t.Errorf("expected server-side apply Content-Type, got %q", receivedContentType)
	}
	if receivedQuery != "fieldManager=llm-d-fleet&force=false" {
		t.Errorf("unexpected apply query %q", receivedQuery)
	}
	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("expected Authorization 'Bearer test-token-123', got %q", receivedAuth)
	}
	if !strings.Contains(receivedBody, "test-config") {
		t.Errorf("expected request body to contain 'test-config', got %q", receivedBody)
	}
}

func TestGetResource_ErrorsOnNon2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer ts.Close()

	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "test-cluster",
		Name:   "test",
		Region: "us",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	c.mu.Lock()
	c.clusters["test-cluster"].APIServer = ts.URL
	c.clusters["test-cluster"].Token = "test-token"
	c.mu.Unlock()

	_, err = c.GetResource(ctx, "test-cluster", "/api/v1/pods")
	if err == nil {
		t.Fatal("expected error on 404 response")
	}
}

func TestDeregisterCluster(t *testing.T) {
	c := NewKubeconfigClusterClient()
	ctx := context.Background()

	err := c.RegisterCluster(ctx, ClusterRegistration{
		ID:     "dereg-1",
		Name:   "to-be-removed",
		Region: "ap-southeast-1",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	// Verify it exists.
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster before deregister, got %d", len(clusters))
	}

	// Deregister.
	err = c.DeregisterCluster(ctx, "dereg-1")
	if err != nil {
		t.Fatalf("DeregisterCluster: unexpected error: %v", err)
	}

	// Verify it is gone.
	clusters, err = c.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters after deregister: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters after deregister, got %d", len(clusters))
	}

	// Deregistering again should return an error.
	err = c.DeregisterCluster(ctx, "dereg-1")
	if err == nil {
		t.Fatal("expected error when deregistering non-existent cluster, got nil")
	}
}
