package routing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestKubernetesConfigMapMirrorPublishesUpdatesAndReloadsToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	version, contents := "1", "first"
	requests := 0
	sawInitialToken := false
	sawRotatedToken := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		gotToken := r.Header.Get("Authorization")
		if version == "1" && gotToken == "Bearer first" {
			sawInitialToken = true
		}
		if version == "2" && gotToken == "Bearer second" {
			sawRotatedToken = true
		}
		fmt.Fprintf(w, `{"metadata":{"resourceVersion":%q},"data":{"endpoints.json":%q}}`, version, contents)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	directory := t.TempDir()
	go func() {
		done <- RunKubernetesConfigMapMirror(ctx, KubernetesConfigMapMirrorOptions{
			APIURL: server.URL, Namespace: "fleet", Name: "router-endpoints", Directory: directory,
			TokenFile: tokenFile, Interval: 10 * time.Millisecond, HTTPClient: server.Client(),
		})
	}()

	waitForFileContents(t, filepath.Join(directory, "endpoints.json"), "first")
	if err := os.WriteFile(tokenFile, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	version, contents = "2", "second"
	mu.Unlock()
	waitForFileContents(t, filepath.Join(directory, "endpoints.json"), "second")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		observed := sawRotatedToken
		mu.Unlock()
		if observed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests < 2 {
		t.Fatalf("requests = %d, want at least 2", requests)
	}
	if !sawInitialToken {
		t.Fatal("initial bearer token was not observed")
	}
	if !sawRotatedToken {
		t.Fatal("rotated bearer token was not observed")
	}
}

func TestKubernetesConfigMapMirrorRetainsLastValidFileOnReadFailure(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			fmt.Fprint(w, `{"metadata":{"resourceVersion":"1"},"data":{"endpoints.json":"valid"}}`)
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	errSeen := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunKubernetesConfigMapMirror(ctx, KubernetesConfigMapMirrorOptions{
			APIURL: server.URL, Namespace: "fleet", Name: "router-endpoints", Directory: directory,
			TokenFile: tokenFile, Interval: time.Millisecond, HTTPClient: server.Client(),
			OnError: func(error) {
				select {
				case errSeen <- struct{}{}:
				default:
				}
			},
		})
	}()
	select {
	case <-errSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("expected API failure callback")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, readErr := os.ReadFile(filepath.Join(directory, "endpoints.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "valid" {
		t.Fatalf("retained file = %q", data)
	}
}

func waitForFileContents(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && string(data) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %s did not become %q", path, want)
}
