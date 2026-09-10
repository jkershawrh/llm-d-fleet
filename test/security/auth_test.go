//go:build security

package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
)

func TestTrustedGatewayAssertionCannotBeSpoofedOrForwardInternalHeaders(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := auth.Config{
		Provider: auth.ProviderTrustedProxy, Enabled: true,
		TrustedProxyKeys: map[string]ed25519.PublicKey{"gateway": publicKey},
		TrustedIssuer:    "https://gateway.example", TrustedAudience: "llm-d-fleet",
		AssertionMaxAge: 5 * time.Minute, RequireVerifiedMTLS: true,
	}
	payload, err := json.Marshal(map[string]interface{}{
		"kid": "gateway", "sub": "user-1", "tenant": "tenant-a",
		"roles": []string{auth.RoleTenant}, "iss": "https://gateway.example",
		"aud": "llm-d-fleet", "iat": time.Now().Add(-time.Minute).Unix(),
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertion := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := auth.GetIdentity(r)
		if identity == nil || identity.Tenant != "tenant-a" {
			t.Fatalf("identity = %#v", identity)
		}
		if r.Header.Get(auth.IdentityAssertionHeader) != "" || r.Header.Get("X-Fleet-Target-Cluster") != "" {
			t.Fatal("trusted or routing authority leaked downstream")
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.AuthMiddleware(cfg, nil, inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
	req.Header.Set(auth.IdentityAssertionHeader, assertion)
	req.Header.Set("X-Fleet-Target-Cluster", "attacker")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}

	// A changed payload with the original signature must fail.
	var changed map[string]interface{}
	if err := json.Unmarshal(payload, &changed); err != nil {
		t.Fatal(err)
	}
	changed["tenant"] = "tenant-b"
	changedPayload, _ := json.Marshal(changed)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
	req.Header.Set(auth.IdentityAssertionHeader, base64.RawURLEncoding.EncodeToString(changedPayload)+"."+base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload)))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("forged status = %d", rr.Code)
	}
}

// newTestServer creates an httptest.Server with auth middleware wrapping
// a simple mux that mirrors the fleet-controller API shape.
func newTestServer(secret string) *httptest.Server {
	cfg := auth.Config{
		Secret:   secret,
		TokenTTL: 24 * time.Hour,
		Enabled:  secret != "",
	}
	exempt := []string{"/healthz", "/readyz", "/metrics"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/clusters", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	})
	mux.HandleFunc("DELETE /api/v1/clusters/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Check RBAC.
		claims := auth.GetClaims(r)
		if claims != nil && !auth.CheckPermission(claims.Role, r.Method) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "forbidden",
				"detail": fmt.Sprintf("role %q cannot perform %s", claims.Role, r.Method),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	handler := auth.AuthMiddleware(cfg, exempt, mux)
	return httptest.NewServer(handler)
}

func generateTestToken(secret, subject, role string, ttl time.Duration) string {
	claims := auth.Claims{
		Subject:   subject,
		Role:      role,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}
	token, err := auth.GenerateToken(secret, claims)
	if err != nil {
		panic(fmt.Sprintf("failed to generate test token: %v", err))
	}
	return token
}

func TestUnauthenticatedRequestReturns401(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/clusters")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthenticatedRequestReturns200(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "admin-user", auth.RoleAdmin, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestExpiredTokenReturns401(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	// Generate a token that expired 1 hour ago.
	token := generateTestToken(secret, "old-user", auth.RoleAdmin, -1*time.Hour)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestViewerCannotDelete(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "viewer-user", auth.RoleViewer, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clusters/test-cluster", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestHealthProbeBypassesAuth(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	// No Authorization header, but health probe should pass.
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOperatorCannotDeleteClusters(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "operator-user", auth.RoleOperator, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clusters/test-cluster", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for operator DELETE, got %d", resp.StatusCode)
	}
}

func TestAdminCanPerformAllOperations(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "admin-user", auth.RoleAdmin, 1*time.Hour)

	// Admin can GET
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin GET should return 200, got %d", resp.StatusCode)
	}

	// Admin can DELETE
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clusters/test-cluster", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin DELETE should return 200, got %d", resp.StatusCode)
	}
}

// newTenantTestServer creates an httptest.Server with both AuthMiddleware and
// AuthorizationMiddleware wired up, including a tenant usage endpoint for
// testing tenant-scoped access control.
func newTenantTestServer(secret string) *httptest.Server {
	cfg := auth.Config{
		Secret:   secret,
		TokenTTL: 24 * time.Hour,
		Enabled:  true,
	}
	exempt := []string{"/healthz", "/readyz", "/metrics"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clusters", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	})
	mux.HandleFunc("GET /api/v1/tenants/{id}/usage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("DELETE /api/v1/clusters/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	var handler http.Handler = auth.AuthorizationMiddleware(exempt, mux)
	handler = auth.AuthMiddleware(cfg, exempt, handler)
	return httptest.NewServer(handler)
}

func TestTenantCanAccessOwnUsage(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTenantTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "tenant-alpha", auth.RoleTenant, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tenants/tenant-alpha/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("tenant should access own usage, got %d", resp.StatusCode)
	}
}

func TestTenantCannotAccessOtherTenantUsage(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTenantTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "tenant-alpha", auth.RoleTenant, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/tenants/tenant-beta/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("tenant should NOT access other tenant's usage, got %d", resp.StatusCode)
	}
}

func TestTenantCannotListClusters(t *testing.T) {
	secret := "integration-test-secret"
	ts := newTenantTestServer(secret)
	defer ts.Close()

	token := generateTestToken(secret, "tenant-alpha", auth.RoleTenant, 1*time.Hour)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Tenants should be forbidden from non-usage endpoints
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("tenant should NOT access clusters, got %d", resp.StatusCode)
	}
}
