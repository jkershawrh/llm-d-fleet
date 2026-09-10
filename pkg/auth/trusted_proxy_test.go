package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func trustedProxyFixture(t *testing.T) (Config, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		Provider: ProviderTrustedProxy, Enabled: true,
		TrustedProxyKeys: map[string]ed25519.PublicKey{"gateway-1": publicKey},
		TrustedIssuer:    "https://gateway.example", TrustedAudience: "llm-d-fleet",
		AssertionMaxAge: 5 * time.Minute, RequireVerifiedMTLS: true,
	}, privateKey
}

func signAssertion(t *testing.T, key ed25519.PrivateKey, assertion trustedIdentityAssertion) string {
	t.Helper()
	payload, err := json.Marshal(assertion)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, payload))
}

func validAssertion(now time.Time) trustedIdentityAssertion {
	return trustedIdentityAssertion{
		KeyID: "gateway-1", Subject: "user-1", Tenant: "tenant-a",
		Roles: []string{RoleTenant}, Issuer: "https://gateway.example",
		Audience: "llm-d-fleet", RequestID: "request-1",
		IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}
}

func verifiedTLSState() *tls.ConnectionState {
	return &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
}

func TestTrustedProxyMiddlewareAcceptsVerifiedAssertion(t *testing.T) {
	cfg, privateKey := trustedProxyFixture(t)
	now := time.Now()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := GetIdentity(r)
		if identity == nil || identity.Subject != "user-1" || identity.Tenant != "tenant-a" {
			t.Fatalf("identity = %#v", identity)
		}
		if got := r.Header.Get(IdentityAssertionHeader); got != "" {
			t.Fatalf("assertion leaked downstream: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := AuthMiddleware(cfg, nil, inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req.TLS = verifiedTLSState()
	req.Header.Set(IdentityAssertionHeader, signAssertion(t, privateKey, validAssertion(now)))
	req.Header.Set("X-Fleet-Target-Cluster", "attacker-selected")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := req.Header.Get("X-Fleet-Target-Cluster"); got != "" {
		t.Fatalf("internal routing header survived: %q", got)
	}
}

func TestTrustedProxyMiddlewareFailsClosed(t *testing.T) {
	cfg, privateKey := trustedProxyFixture(t)
	tests := []struct {
		name   string
		mutate func(*http.Request, *trustedIdentityAssertion)
	}{
		{"missing mTLS", func(r *http.Request, _ *trustedIdentityAssertion) { r.TLS = nil }},
		{"wrong audience", func(_ *http.Request, a *trustedIdentityAssertion) { a.Audience = "other" }},
		{"expired", func(_ *http.Request, a *trustedIdentityAssertion) { a.ExpiresAt = time.Now().Add(-time.Minute).Unix() }},
		{"spoofed signature", func(_ *http.Request, a *trustedIdentityAssertion) { a.Subject = "attacker" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			assertion := validAssertion(now)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
			req.TLS = verifiedTLSState()
			if tc.name == "spoofed signature" {
				req.Header.Set(IdentityAssertionHeader, signAssertion(t, privateKey, assertion))
				tc.mutate(req, &assertion)
				// The payload changes while the original signature remains.
				payload, _ := json.Marshal(assertion)
				parts := req.Header.Get(IdentityAssertionHeader)
				sig := parts[len(parts)-base64.RawURLEncoding.EncodedLen(ed25519.SignatureSize):]
				req.Header.Set(IdentityAssertionHeader, base64.RawURLEncoding.EncodeToString(payload)+"."+sig)
			} else {
				tc.mutate(req, &assertion)
				req.Header.Set(IdentityAssertionHeader, signAssertion(t, privateKey, assertion))
			}
			rr := httptest.NewRecorder()
			AuthMiddleware(cfg, nil, dummyHandler()).ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rr.Code)
			}
		})
	}
}

func TestHMACMiddlewareStripsInternalHeaders(t *testing.T) {
	claims := Claims{Subject: "admin", Role: RoleAdmin, IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	token, err := GenerateToken("test-secret", claims)
	if err != nil {
		t.Fatal(err)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gateway-Destination-Endpoint") != "" || r.Header.Get(IdentityAssertionHeader) != "" {
			t.Fatal("internal headers were not stripped")
		}
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Gateway-Destination-Endpoint", "attacker")
	req.Header.Set(IdentityAssertionHeader, "attacker")
	rr := httptest.NewRecorder()
	AuthMiddleware(Config{Secret: "test-secret", Enabled: true}, nil, inner).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}
