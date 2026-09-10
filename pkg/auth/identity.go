package auth

import (
	"context"
	"net/http"
	"slices"
)

// Method identifies how an upstream principal was authenticated. Values are
// intentionally product-neutral: integrations may use any gateway that can
// satisfy the corresponding verification contract.
type Method string

const (
	MethodHMAC         Method = "hmac"
	MethodTrustedProxy Method = "trusted-proxy"
)

// Identity is the normalized, verified principal presented to fleet policy.
// Subscription and Entitlements are opaque references; llm-d-fleet does not
// own customer-facing API keys, OIDC login, or subscription lifecycle.
type Identity struct {
	Subject      string   `json:"subject"`
	Tenant       string   `json:"tenant,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Groups       []string `json:"groups,omitempty"`
	Subscription string   `json:"subscription,omitempty"`
	Entitlements []string `json:"entitlements,omitempty"`
	Issuer       string   `json:"issuer,omitempty"`
	Audience     string   `json:"audience,omitempty"`
	RequestID    string   `json:"request_id,omitempty"`
	Method       Method   `json:"method"`
}

type identityContextKey struct{}

// WithIdentity stores a verified identity in a context.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// GetIdentity retrieves the verified normalized identity, if one exists.
func GetIdentity(r *http.Request) *Identity {
	identity, _ := r.Context().Value(identityContextKey{}).(*Identity)
	return identity
}

// HasRole reports whether the verified identity contains role.
func (i *Identity) HasRole(role string) bool {
	return i != nil && slices.Contains(i.Roles, role)
}

func identityFromClaims(claims *Claims) *Identity {
	if claims == nil {
		return nil
	}
	tenant := claims.Tenant
	if tenant == "" && claims.Role == RoleTenant {
		tenant = claims.Subject
	}
	return &Identity{
		Subject:   claims.Subject,
		Tenant:    tenant,
		Roles:     []string{claims.Role},
		Issuer:    claims.Issuer,
		Audience:  claims.Audience,
		RequestID: claims.RequestID,
		Method:    MethodHMAC,
	}
}
