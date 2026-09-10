package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type trustedIdentityAssertion struct {
	KeyID        string   `json:"kid"`
	Subject      string   `json:"sub"`
	Tenant       string   `json:"tenant,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Groups       []string `json:"groups,omitempty"`
	Subscription string   `json:"subscription,omitempty"`
	Entitlements []string `json:"entitlements,omitempty"`
	Issuer       string   `json:"iss"`
	Audience     string   `json:"aud"`
	RequestID    string   `json:"request_id,omitempty"`
	IssuedAt     int64    `json:"iat"`
	ExpiresAt    int64    `json:"exp"`
}

func validateTrustedProxyAssertion(cfg Config, r *http.Request, now time.Time) (*Identity, error) {
	if cfg.RequireVerifiedMTLS && (r.TLS == nil || len(r.TLS.VerifiedChains) == 0) {
		return nil, fmt.Errorf("trusted proxy requires a verified mTLS client certificate")
	}
	compact := strings.TrimSpace(r.Header.Get(IdentityAssertionHeader))
	if compact == "" {
		return nil, fmt.Errorf("missing trusted identity assertion")
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid trusted identity assertion format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid trusted identity assertion payload")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid trusted identity assertion signature")
	}
	var assertion trustedIdentityAssertion
	if err := json.Unmarshal(payload, &assertion); err != nil {
		return nil, fmt.Errorf("invalid trusted identity assertion JSON")
	}
	key, ok := cfg.TrustedProxyKeys[assertion.KeyID]
	if !ok || !ed25519.Verify(key, payload, signature) {
		return nil, fmt.Errorf("trusted identity assertion signature verification failed")
	}
	if assertion.Subject == "" || assertion.Issuer != cfg.TrustedIssuer || assertion.Audience != cfg.TrustedAudience {
		return nil, fmt.Errorf("trusted identity assertion subject, issuer, or audience is invalid")
	}
	issuedAt, expiresAt := time.Unix(assertion.IssuedAt, 0), time.Unix(assertion.ExpiresAt, 0)
	if issuedAt.After(now.Add(30*time.Second)) || now.After(expiresAt) || !expiresAt.After(issuedAt) {
		return nil, fmt.Errorf("trusted identity assertion time window is invalid")
	}
	if now.Sub(issuedAt) > cfg.AssertionMaxAge || expiresAt.Sub(issuedAt) > cfg.AssertionMaxAge {
		return nil, fmt.Errorf("trusted identity assertion exceeds maximum age")
	}
	if len(assertion.Roles) == 0 {
		return nil, fmt.Errorf("trusted identity assertion has no roles")
	}
	return &Identity{
		Subject: assertion.Subject, Tenant: assertion.Tenant, Roles: assertion.Roles,
		Groups: assertion.Groups, Subscription: assertion.Subscription,
		Entitlements: assertion.Entitlements, Issuer: assertion.Issuer,
		Audience: assertion.Audience, RequestID: assertion.RequestID,
		Method: MethodTrustedProxy,
	}, nil
}
