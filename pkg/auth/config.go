package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds authentication configuration for the fleet controller.
type Config struct {
	Provider            ProviderName
	Secret              string // HMAC-SHA256 signing secret
	TokenTTL            time.Duration
	Enabled             bool
	TrustedProxyKeys    map[string]ed25519.PublicKey
	TrustedIssuer       string
	TrustedAudience     string
	AssertionMaxAge     time.Duration
	RequireVerifiedMTLS bool
}

type ProviderName string

const (
	ProviderDisabled     ProviderName = "disabled"
	ProviderHMAC         ProviderName = "hmac"
	ProviderTrustedProxy ProviderName = "trusted-proxy"
)

const IdentityAssertionHeader = "X-Fleet-Identity-Assertion"

// MinSecretLength is the shortest HMAC signing secret accepted. Tokens are
// signed with HMAC-SHA256, so a secret shorter than the 32-byte output adds
// no strength and usually indicates a placeholder was left in place.
const MinSecretLength = 32

// ConfigFromEnv creates a Config from environment variables.
// Auth is enabled when FLEET_AUTH_SECRET is set.
// FLEET_AUTH_TTL optionally overrides the default 24h token lifetime
// (parsed via time.ParseDuration, e.g. "1h", "30m").
//
// It returns an error rather than silently disabling authentication when the
// operator's intent to enable it is clear but the configuration is unusable —
// an unreadable secret file, or a secret too short to be meaningful. Failing
// to start is the correct outcome: a controller that serves traffic with
// auth_enabled=false after a mis-mounted Secret is the worst case.
func ConfigFromEnv() (Config, error) {
	return configFromEnv(ProviderName(strings.TrimSpace(os.Getenv("FLEET_IDENTITY_PROVIDER"))))
}

// ConfigFromEnvWithProvider loads authentication configuration while using
// provider as the command-line override. An empty override uses
// FLEET_IDENTITY_PROVIDER and then the backward-compatible HMAC/disabled
// inference rules.
func ConfigFromEnvWithProvider(provider string) (Config, error) {
	selected := ProviderName(strings.TrimSpace(provider))
	if selected == "" {
		selected = ProviderName(strings.TrimSpace(os.Getenv("FLEET_IDENTITY_PROVIDER")))
	}
	return configFromEnv(selected)
}

func configFromEnv(provider ProviderName) (Config, error) {
	secret := os.Getenv("FLEET_AUTH_SECRET")

	// FLEET_AUTH_SECRET_FILE takes precedence (for K8s Secret volume mounts).
	if secretFile := os.Getenv("FLEET_AUTH_SECRET_FILE"); secretFile != "" {
		// #nosec G304 G703 -- path comes from FLEET_AUTH_SECRET_FILE, which only
		// the operator sets; it is deployment configuration, not request input.
		data, err := os.ReadFile(secretFile)
		if err != nil {
			return Config{}, fmt.Errorf("FLEET_AUTH_SECRET_FILE %q is set but unreadable: %w", secretFile, err)
		}
		secret = strings.TrimSpace(string(data))
		if secret == "" {
			return Config{}, fmt.Errorf("FLEET_AUTH_SECRET_FILE %q is empty", secretFile)
		}
	}

	if secret != "" && len(secret) < MinSecretLength {
		return Config{}, fmt.Errorf("auth secret is %d bytes; at least %d are required", len(secret), MinSecretLength)
	}

	ttl := 24 * time.Hour
	if raw := os.Getenv("FLEET_AUTH_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("FLEET_AUTH_TTL %q is not a valid duration: %w", raw, err)
		}
		ttl = parsed
	}

	if provider == "" {
		if secret != "" {
			provider = ProviderHMAC
		} else {
			provider = ProviderDisabled
		}
	}
	cfg := Config{
		Provider:            provider,
		Secret:              secret,
		TokenTTL:            ttl,
		Enabled:             provider != ProviderDisabled,
		AssertionMaxAge:     5 * time.Minute,
		RequireVerifiedMTLS: true,
	}
	switch provider {
	case ProviderDisabled:
		if secret != "" {
			return Config{}, fmt.Errorf("FLEET_IDENTITY_PROVIDER=disabled conflicts with configured HMAC secret")
		}
	case ProviderHMAC:
		if secret == "" {
			return Config{}, fmt.Errorf("FLEET_IDENTITY_PROVIDER=hmac requires FLEET_AUTH_SECRET or FLEET_AUTH_SECRET_FILE")
		}
	case ProviderTrustedProxy:
		if secret != "" {
			return Config{}, fmt.Errorf("trusted-proxy mode does not accept FLEET_AUTH_SECRET; configure public verification keys")
		}
		cfg.TrustedIssuer = strings.TrimSpace(os.Getenv("FLEET_TRUSTED_IDENTITY_ISSUER"))
		cfg.TrustedAudience = strings.TrimSpace(os.Getenv("FLEET_TRUSTED_IDENTITY_AUDIENCE"))
		keys, err := parseTrustedProxyKeys(os.Getenv("FLEET_TRUSTED_IDENTITY_KEYS_JSON"))
		if err != nil {
			return Config{}, err
		}
		cfg.TrustedProxyKeys = keys
		if cfg.TrustedIssuer == "" || cfg.TrustedAudience == "" || len(keys) == 0 {
			return Config{}, fmt.Errorf("trusted-proxy mode requires issuer, audience, and at least one Ed25519 public key")
		}
		if raw := strings.TrimSpace(os.Getenv("FLEET_TRUSTED_IDENTITY_MAX_AGE")); raw != "" {
			cfg.AssertionMaxAge, err = time.ParseDuration(raw)
			if err != nil || cfg.AssertionMaxAge <= 0 {
				return Config{}, fmt.Errorf("FLEET_TRUSTED_IDENTITY_MAX_AGE %q must be a positive duration", raw)
			}
		}
		if raw := strings.TrimSpace(os.Getenv("FLEET_TRUSTED_PROXY_REQUIRE_MTLS")); raw != "" {
			switch raw {
			case "true":
				cfg.RequireVerifiedMTLS = true
			case "false":
				cfg.RequireVerifiedMTLS = false
			default:
				return Config{}, fmt.Errorf("FLEET_TRUSTED_PROXY_REQUIRE_MTLS must be true or false")
			}
		}
	default:
		return Config{}, fmt.Errorf("unsupported FLEET_IDENTITY_PROVIDER %q", provider)
	}
	return cfg, nil
}

func parseTrustedProxyKeys(raw string) (map[string]ed25519.PublicKey, error) {
	encoded := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, fmt.Errorf("FLEET_TRUSTED_IDENTITY_KEYS_JSON must be a key-id to base64 public-key object: %w", err)
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	for id, value := range encoded {
		value = strings.TrimPrefix(strings.TrimSpace(value), "base64:")
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("trusted identity key %q must be a base64-encoded %d-byte Ed25519 public key", id, ed25519.PublicKeySize)
		}
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("trusted identity key ID must not be empty")
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}
