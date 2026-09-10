package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testSecret is long enough to satisfy MinSecretLength.
const testSecret = "0123456789abcdef0123456789abcdef"

func TestConfigFromEnv_NoVarsSet(t *testing.T) {
	// Ensure the relevant env vars are unset.
	t.Setenv("FLEET_AUTH_SECRET", "")
	t.Setenv("FLEET_AUTH_TTL", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Enabled {
		t.Error("expected Enabled to be false when FLEET_AUTH_SECRET is empty")
	}
	if cfg.Secret != "" {
		t.Errorf("expected empty Secret, got %q", cfg.Secret)
	}
	if cfg.TokenTTL != 24*time.Hour {
		t.Errorf("expected default TokenTTL of 24h, got %v", cfg.TokenTTL)
	}
}

func TestConfigFromEnv_TrustedProxy(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(map[string]string{
		"gateway-1": "base64:" + base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLEET_IDENTITY_PROVIDER", string(ProviderTrustedProxy))
	t.Setenv("FLEET_AUTH_SECRET", "")
	t.Setenv("FLEET_AUTH_SECRET_FILE", "")
	t.Setenv("FLEET_TRUSTED_IDENTITY_KEYS_JSON", string(encoded))
	t.Setenv("FLEET_TRUSTED_IDENTITY_ISSUER", "https://gateway.example")
	t.Setenv("FLEET_TRUSTED_IDENTITY_AUDIENCE", "llm-d-fleet")
	t.Setenv("FLEET_TRUSTED_IDENTITY_MAX_AGE", "2m")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if cfg.Provider != ProviderTrustedProxy || !cfg.Enabled || !cfg.RequireVerifiedMTLS {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.AssertionMaxAge != 2*time.Minute || len(cfg.TrustedProxyKeys) != 1 {
		t.Fatalf("trusted proxy config = %#v", cfg)
	}
}

func TestConfigFromEnv_TrustedProxyRejectsIncompleteTrust(t *testing.T) {
	t.Setenv("FLEET_IDENTITY_PROVIDER", string(ProviderTrustedProxy))
	t.Setenv("FLEET_AUTH_SECRET", "")
	t.Setenv("FLEET_AUTH_SECRET_FILE", "")
	t.Setenv("FLEET_TRUSTED_IDENTITY_KEYS_JSON", `{}`)
	t.Setenv("FLEET_TRUSTED_IDENTITY_ISSUER", "")
	t.Setenv("FLEET_TRUSTED_IDENTITY_AUDIENCE", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("incomplete trusted proxy configuration must fail closed")
	}
}

func TestConfigFromEnv_RejectsUnknownProvider(t *testing.T) {
	t.Setenv("FLEET_IDENTITY_PROVIDER", "proprietary-gateway")
	t.Setenv("FLEET_AUTH_SECRET", "")
	t.Setenv("FLEET_AUTH_SECRET_FILE", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("unknown identity provider must be rejected")
	}
}

func TestConfigFromEnvWithProvider_FlagOverridesEnvironment(t *testing.T) {
	t.Setenv("FLEET_IDENTITY_PROVIDER", string(ProviderDisabled))
	t.Setenv("FLEET_AUTH_SECRET", testSecret)
	t.Setenv("FLEET_AUTH_SECRET_FILE", "")
	cfg, err := ConfigFromEnvWithProvider(string(ProviderHMAC))
	if err != nil {
		t.Fatalf("ConfigFromEnvWithProvider() error = %v", err)
	}
	if cfg.Provider != ProviderHMAC || !cfg.Enabled {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestConfigFromEnv_SecretSet(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET", testSecret)
	t.Setenv("FLEET_AUTH_TTL", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true when FLEET_AUTH_SECRET is set")
	}
	if cfg.Secret != testSecret {
		t.Errorf("expected Secret %q, got %q", testSecret, cfg.Secret)
	}
	if cfg.TokenTTL != 24*time.Hour {
		t.Errorf("expected default TokenTTL of 24h, got %v", cfg.TokenTTL)
	}
}

func TestConfigFromEnv_TTLSet(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET", testSecret)
	t.Setenv("FLEET_AUTH_TTL", "1h30m")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 90 * time.Minute
	if cfg.TokenTTL != expected {
		t.Errorf("expected TokenTTL %v, got %v", expected, cfg.TokenTTL)
	}
}

// TestConfigFromEnv_TTLInvalidIsRejected: an unparseable TTL is an operator
// error. Silently substituting the 24h default hides a misconfiguration that
// was meant to shorten token lifetime.
func TestConfigFromEnv_TTLInvalidIsRejected(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET", testSecret)
	t.Setenv("FLEET_AUTH_TTL", "not-a-duration")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected an error for an unparseable FLEET_AUTH_TTL")
	}
}

func TestConfigFromEnv_EnabledIsFalseForEmptySecret(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Enabled {
		t.Error("expected Enabled=false for empty secret")
	}
}

// TestConfigFromEnv_ShortSecretIsRejected guards against placeholder secrets
// being accepted as real ones.
func TestConfigFromEnv_ShortSecretIsRejected(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET", "changeme")
	t.Setenv("FLEET_AUTH_TTL", "")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("expected an error for a secret shorter than MinSecretLength")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error should state the minimum length, got: %v", err)
	}
}

func TestConfigFromEnv_ReadsSecretFile(t *testing.T) {
	// Write secret to a temp file (simulates K8s Secret volume mount).
	path := filepath.Join(t.TempDir(), "fleet-secret")
	if err := os.WriteFile(path, []byte(testSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FLEET_AUTH_SECRET_FILE", path)
	t.Setenv("FLEET_AUTH_SECRET", "") // env var should be overridden by file
	t.Setenv("FLEET_AUTH_TTL", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Secret != testSecret {
		t.Errorf("expected secret from file, got %q", cfg.Secret)
	}
	if !cfg.Enabled {
		t.Error("should be enabled when secret is loaded from file")
	}
}

// TestConfigFromEnv_UnreadableSecretFileFailsClosed is the regression guard
// for a fail-open path: an unreadable FLEET_AUTH_SECRET_FILE used to be
// ignored, falling back to the env var or to no secret at all, which set
// Enabled=false and served every request unauthenticated. Setting the
// variable is an unambiguous statement that auth is wanted, so a bad path
// must stop startup rather than quietly disable it.
func TestConfigFromEnv_UnreadableSecretFileFailsClosed(t *testing.T) {
	t.Setenv("FLEET_AUTH_SECRET_FILE", "/nonexistent/path")
	t.Setenv("FLEET_AUTH_SECRET", testSecret)
	t.Setenv("FLEET_AUTH_TTL", "")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("an unreadable secret file must be an error, not a silent fallback")
	}
	if !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("error should name the cause, got: %v", err)
	}
}

func TestConfigFromEnv_EmptySecretFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-secret")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FLEET_AUTH_SECRET_FILE", path)
	t.Setenv("FLEET_AUTH_SECRET", "")
	t.Setenv("FLEET_AUTH_TTL", "")

	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("an empty secret file must be an error, not a silent fallback")
	}
}
