package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// contextKey is the type used for storing values in request context.
type contextKey string

// ClaimsContextKey is the context key for storing validated claims.
const ClaimsContextKey contextKey = "auth-claims"

// GetClaims retrieves validated claims from the request context.
// Returns nil if no claims are present.
func GetClaims(r *http.Request) *Claims {
	v := r.Context().Value(ClaimsContextKey)
	if v == nil {
		return nil
	}
	claims, ok := v.(*Claims)
	if !ok {
		return nil
	}
	return claims
}

// WithClaims stores validated claims in a context.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, ClaimsContextKey, claims)
}

// AuthMiddleware wraps an http.Handler and requires valid bearer tokens.
// Exempt paths (e.g. health/readiness/metrics) bypass authentication.
// When auth is disabled (cfg.Enabled == false), all requests pass through.
func AuthMiddleware(cfg Config, exempt []string, next http.Handler) http.Handler {
	exemptSet := make(map[string]bool, len(exempt))
	for _, p := range exempt {
		exemptSet[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, pass through.
		if !cfg.Enabled {
			stripInternalRequestHeaders(r.Header)
			next.ServeHTTP(w, r)
			return
		}

		// Check if the path is exempt.
		if exemptSet[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		if cfg.Provider == ProviderTrustedProxy {
			identity, err := validateTrustedProxyAssertion(cfg, r, time.Now())
			stripInternalRequestHeaders(r.Header)
			if err != nil {
				writeAuthError(w, err.Error())
				slog.Info("fleet.security.auth.failed", "remote", r.RemoteAddr,
					"reason", "trusted_identity_rejected", "path", r.URL.Path)
				return
			}
			ctx := WithIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		stripInternalRequestHeaders(r.Header)
		// Extract bearer token from Authorization header.
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeAuthError(w, "missing Authorization header")
			slog.Info("fleet.security.auth.failed",
				"remote", r.RemoteAddr, "reason", "missing_header", "path", r.URL.Path)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeAuthError(w, "invalid Authorization header format, expected 'Bearer <token>'")
			slog.Info("fleet.security.auth.failed",
				"remote", r.RemoteAddr, "reason", "invalid_header_format", "path", r.URL.Path)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Validate the token.
		claims, err := ValidateToken(cfg.Secret, token)
		if err != nil {
			writeAuthError(w, err.Error())
			slog.Warn("auth failed", "remote", r.RemoteAddr, "reason", err.Error(), "path", r.URL.Path)
			return
		}

		// Store claims in context and call next handler.
		ctx := WithClaims(r.Context(), claims)
		ctx = WithIdentity(ctx, identityFromClaims(claims))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

var internalRequestHeaders = []string{
	IdentityAssertionHeader,
	"X-Fleet-Actor",
	"X-Fleet-Tenant",
	"X-Fleet-Target-Cluster",
	"X-Fleet-Routing-Reason",
	"X-Fleet-Data-Plane",
	"X-Fleet-Router-Upstream",
	"X-Gateway-Destination-Endpoint",
}

func stripInternalRequestHeaders(header http.Header) {
	for _, name := range internalRequestHeaders {
		header.Del(name)
	}
}

// AuthorizationMiddleware enforces role-based access for authenticated
// requests. When no claims are present, it passes through so disabled-auth
// development mode remains usable.
func AuthorizationMiddleware(exempt []string, next http.Handler) http.Handler {
	exemptSet := make(map[string]bool, len(exempt))
	for _, p := range exempt {
		exemptSet[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exemptSet[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		identity := GetIdentity(r)
		if identity == nil {
			identity = identityFromClaims(GetClaims(r))
		}
		if identity == nil {
			next.ServeHTTP(w, r)
			return
		}
		role := primaryRole(identity.Roles)

		inferenceRequest := r.Method == http.MethodPost &&
			(r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/completions")
		if !CheckPermission(role, r.Method) && (role != RoleTenant || !inferenceRequest) {
			writeAuthorizationError(w, "role is not allowed to perform this action")
			slog.Info("fleet.security.rbac.denied",
				"subject", identity.Subject, "role", role,
				"method", r.Method, "path", r.URL.Path, "reason", "method")
			return
		}

		if role == RoleTenant && !inferenceRequest && !tenantRequestAllowed(identity.Tenant, r.Method, r.URL.Path) {
			writeAuthorizationError(w, "tenant is not allowed to access this resource")
			slog.Info("fleet.security.rbac.denied",
				"subject", identity.Subject, "role", role,
				"method", r.Method, "path", r.URL.Path, "reason", "tenant_scope")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func primaryRole(roles []string) string {
	for _, role := range []string{RoleAdmin, RoleOperator, RoleViewer, RoleTenant} {
		for _, candidate := range roles {
			if candidate == role {
				return role
			}
		}
	}
	return ""
}

func tenantRequestAllowed(subject, method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	const prefix = "/api/v1/tenants/"
	const suffix = "/usage"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return tenantID != "" && tenantID == subject
}

// writeAuthError writes a 401 JSON error response.
func writeAuthError(w http.ResponseWriter, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "unauthorized",
		"detail": detail,
	})
}

// writeAuthorizationError writes a 403 JSON error response.
func writeAuthorizationError(w http.ResponseWriter, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "forbidden",
		"detail": detail,
	})
}
