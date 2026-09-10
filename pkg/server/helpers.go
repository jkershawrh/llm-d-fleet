package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
	"github.com/llm-d/fleet-llm-d/pkg/tlsutil"
)

const MaxRequestBodyBytes int64 = 1 << 20

func readServiceAccountToken() string {
	data, err := os.ReadFile(tlsutil.ServiceAccountTokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("error encoding JSON response", "error", err)
	}
}

// writeError writes an error JSON response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// OperatorJSONIntentsEnabled returns true if operator JSON intents are enabled
// via the flag or the FLEET_ALLOW_OPERATOR_JSON_INTENTS environment variable.
func OperatorJSONIntentsEnabled(flagValue bool) bool {
	return flagValue || strings.TrimSpace(os.Getenv("FLEET_ALLOW_OPERATOR_JSON_INTENTS")) == "true"
}

// RequestActor extracts the authenticated actor identity from the request.
// The fallback is reachable only when authentication is disabled for a
// standalone development profile. Never derive audit identity from a
// client-controlled header.
func RequestActor(r *http.Request) string {
	if identity := auth.GetIdentity(r); identity != nil && strings.TrimSpace(identity.Subject) != "" {
		return identity.Subject
	}
	if claims := auth.GetClaims(r); claims != nil && strings.TrimSpace(claims.Subject) != "" {
		return claims.Subject
	}
	return "unauthenticated-development"
}

// defaultExemptPaths merges required exempt paths with configured ones.
func defaultExemptPaths(configured []string) []string {
	required := []string{"/healthz", "/readyz", "/metrics"}
	seen := make(map[string]bool)
	for _, p := range required {
		seen[p] = true
	}
	merged := append([]string{}, required...)
	for _, p := range configured {
		if !seen[p] {
			merged = append(merged, p)
			seen[p] = true
		}
	}
	return merged
}

// SplitCSV splits a comma-separated string into a slice of trimmed non-empty strings.
func SplitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

// DecisionPackageKeyringFromEnv reads GCL decision signing keys from
// environment variables.
func DecisionPackageKeyringFromEnv() (map[string][]byte, error) {
	if encoded := strings.TrimSpace(os.Getenv("GCL_DECISION_SIGNING_KEYS_JSON")); encoded != "" {
		var configured map[string]string
		if err := json.Unmarshal([]byte(encoded), &configured); err != nil {
			return nil, fmt.Errorf("parse GCL_DECISION_SIGNING_KEYS_JSON: %w", err)
		}
		keyring := make(map[string][]byte, len(configured))
		for keyID, material := range configured {
			if strings.TrimSpace(keyID) == "" {
				return nil, fmt.Errorf("GCL decision signing key ID cannot be empty")
			}
			key, err := decodeDecisionSigningKey(material)
			if err != nil {
				return nil, fmt.Errorf("decode GCL decision signing key %q: %w", keyID, err)
			}
			keyring[keyID] = key
		}
		if len(keyring) == 0 {
			return nil, fmt.Errorf("GCL_DECISION_SIGNING_KEYS_JSON cannot be empty")
		}
		return keyring, nil
	}

	material := os.Getenv("GCL_DECISION_SIGNING_KEY")
	if material == "" {
		return nil, nil
	}
	keyID := strings.TrimSpace(os.Getenv("GCL_DECISION_SIGNING_KEY_ID"))
	if keyID == "" {
		keyID = "gcl-decision-v1"
	}
	key, err := decodeDecisionSigningKey(material)
	if err != nil {
		return nil, fmt.Errorf("decode GCL decision signing key %q: %w", keyID, err)
	}
	return map[string][]byte{keyID: key}, nil
}

func decodeDecisionSigningKey(material string) ([]byte, error) {
	var key []byte
	if strings.HasPrefix(material, "base64:") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(material, "base64:"))
		if err != nil {
			return nil, fmt.Errorf("invalid base64: %w", err)
		}
		key = decoded
	} else {
		key = []byte(material)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("key must contain at least 32 bytes")
	}
	return key, nil
}
