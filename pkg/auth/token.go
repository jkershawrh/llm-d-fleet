package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims represents the payload of an authentication token.
type Claims struct {
	Subject   string    `json:"sub"`  // user or service account
	Role      string    `json:"role"` // admin, operator, viewer
	Tenant    string    `json:"tenant,omitempty"`
	Issuer    string    `json:"iss,omitempty"`
	Audience  string    `json:"aud,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

// GenerateToken creates an HMAC-SHA256 signed token from the given claims.
// Token format: base64(json_claims).base64(hmac_sha256(json_claims, secret))
func GenerateToken(secret string, claims Claims) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret must not be empty")
	}
	if claims.Subject == "" {
		return "", fmt.Errorf("subject must not be empty")
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(claimsJSON)
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return claimsB64 + "." + sigB64, nil
}

// RefreshToken validates an existing token and issues a new one with an
// extended expiry. The subject and role are preserved. Returns an error
// if the original token is invalid or expired.
func RefreshToken(secret, token string, newTTL time.Duration) (string, error) {
	claims, err := ValidateToken(secret, token)
	if err != nil {
		return "", fmt.Errorf("cannot refresh: %w", err)
	}

	newClaims := Claims{
		Subject:   claims.Subject,
		Role:      claims.Role,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(newTTL),
	}
	return GenerateToken(secret, newClaims)
}

// ValidateToken verifies the token signature and checks expiration.
// Returns the decoded claims on success.
func ValidateToken(secret, token string) (*Claims, error) {
	if secret == "" {
		return nil, fmt.Errorf("secret must not be empty")
	}

	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format: expected two parts separated by '.'")
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token: failed to decode claims: %w", err)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token: failed to decode signature: %w", err)
	}

	// Verify HMAC signature.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(claimsJSON)
	expectedSig := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expectedSig) {
		return nil, fmt.Errorf("invalid token: signature verification failed")
	}

	// Decode claims.
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid token: failed to unmarshal claims: %w", err)
	}

	// Check expiration.
	if time.Now().After(claims.ExpiresAt) {
		return nil, fmt.Errorf("token expired at %s", claims.ExpiresAt.Format(time.RFC3339))
	}

	return &claims, nil
}
