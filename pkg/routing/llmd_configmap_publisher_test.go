package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestKubernetesConfigMapPublisherReplacesData(t *testing.T) {
	var patch []struct {
		Operation string            `json:"op"`
		Path      string            `json:"path"`
		Value     map[string]string `json:"value"`
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/namespaces/fleet/configmaps/router-endpoints" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json-patch+json" {
			t.Fatalf("content type = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	publisher, err := NewKubernetesConfigMapPublisher(server.URL, "fleet", "router-endpoints", "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"index.json": []byte("index\n")}); err != nil {
		t.Fatal(err)
	}
	if len(patch) != 1 || patch[0].Operation != "add" || patch[0].Path != "/data" {
		t.Fatalf("patch = %#v", patch)
	}
	if got := patch[0].Value["index.json"]; got != "index\n" {
		t.Fatalf("published index = %q", got)
	}
}

func TestKubernetesConfigMapPublisherRejectsOversizedData(t *testing.T) {
	publisher, err := NewKubernetesConfigMapPublisher("https://kubernetes.example", "fleet", "router-endpoints", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"endpoints.json": make([]byte, 901*1024)}); err == nil {
		t.Fatal("oversized ConfigMap data was accepted")
	}
}

func TestKubernetesConfigMapPublisherRetainsLastValidDataOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	defer server.Close()
	publisher, err := NewKubernetesConfigMapPublisher(server.URL, "fleet", "router-endpoints", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"index.json": []byte("new")}); err == nil {
		t.Fatal("expected Kubernetes API error")
	}
}

func TestKubernetesConfigMapPublisherReloadsRotatedToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	publisher, err := NewKubernetesConfigMapPublisherFromTokenFile(server.URL, "fleet", "router-endpoints", tokenFile, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"index.json": []byte("first")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"index.json": []byte("second")}); err != nil {
		t.Fatal(err)
	}

	want := []string{"Bearer first", "Bearer second"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("authorization headers = %q, want %q", got, want)
	}
}

func TestKubernetesConfigMapPublisherRejectsUnreadableRotatingToken(t *testing.T) {
	publisher, err := NewKubernetesConfigMapPublisherFromTokenFile("https://kubernetes.example", "fleet", "router-endpoints", filepath.Join(t.TempDir(), "missing"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), map[string][]byte{"index.json": []byte("new")}); err == nil {
		t.Fatal("expected token read error")
	}
}
