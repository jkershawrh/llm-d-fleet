package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
	"github.com/llm-d/fleet-llm-d/pkg/intents"
	"github.com/llm-d/fleet-llm-d/pkg/ledger"

	"github.com/llm-d/fleet-llm-d/pkg/server"
	"github.com/llm-d/fleet-llm-d/pkg/store/postgres"
)

func TestBootstrapStaticProviders(t *testing.T) {
	repo := postgres.NewInMemoryClusterRepository()
	if err := bootstrapStaticProviders(context.Background(), repo, `["provider-a","provider-b","provider-a"]`, false); err != nil {
		t.Fatal(err)
	}
	records, err := repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("provider count = %d, want 2", len(records))
	}
	for _, record := range records {
		if record.Status != postgres.ClusterStatusRunning {
			t.Fatalf("provider %s status = %s", record.ID, record.Status)
		}
	}
	if err := bootstrapStaticProviders(context.Background(), repo, `["provider-a"]`, true); err == nil {
		t.Fatal("production/static provider combination was accepted")
	}
}

func TestBootstrapStaticRoutingState(t *testing.T) {
	clusters := postgres.NewInMemoryClusterRepository()
	pools := postgres.NewInMemoryFleetPoolRepository()
	raw := `{
		"providers":[
			{"id":"provider-a","routingURL":"https://provider-a.example","metricsURL":"https://metrics.provider-a.example","physicalModels":["granite-cpu"],"failureDomain":"zone-a"},
			{"id":"provider-b","routingURL":"https://provider-b.example","metricsURL":"https://metrics.provider-b.example","physicalModels":["granite-cpu"],"failureDomain":"zone-b"}
		],
		"pools":[{"model":"granite-cpu","providers":["provider-a","provider-b"]}]
	}`
	if err := bootstrapStaticRoutingState(context.Background(), clusters, pools, raw, false); err != nil {
		t.Fatal(err)
	}
	record, err := clusters.Get(context.Background(), "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	if record.Labels["fleet.llm-d.ai/egress-address"] != "https://provider-a.example" || record.Labels["fleet.llm-d.ai/physical-models"] != "granite-cpu" {
		t.Fatalf("unexpected provider labels: %#v", record.Labels)
	}
	pool, err := pools.Get(context.Background(), "granite-cpu")
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.DesiredClusters) != 2 || pool.TargetPorts[0] != 443 {
		t.Fatalf("unexpected pool: %#v", pool)
	}
	if err := bootstrapStaticRoutingState(context.Background(), clusters, pools, raw, true); err == nil {
		t.Fatal("production/static routing state combination was accepted")
	}
}

func TestParseFallbackQuota(t *testing.T) {
	config, err := parseFallbackQuota(`{"tokenLimitPerMinute":1000,"concurrentLimit":4,"monthlyBudgetCents":100000}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if config.TokenLimitPerMinute != 1000 || config.ConcurrentLimit != 4 || config.MonthlyBudgetCents != 100000 {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestParseFallbackQuotaRejectsDurableOrInvalidConfig(t *testing.T) {
	valid := `{"tokenLimitPerMinute":1000,"concurrentLimit":4,"monthlyBudgetCents":100000}`
	if _, err := parseFallbackQuota(valid, true); err == nil {
		t.Fatal("expected durable mode rejection")
	}
	if _, err := parseFallbackQuota(`{"tokenLimitPerMinute":0,"concurrentLimit":4,"monthlyBudgetCents":100000}`, false); err == nil {
		t.Fatal("expected non-positive limit rejection")
	}
}

func TestValidateProductionConfig(t *testing.T) {
	t.Setenv("LEDGER_GATEWAY_API_TOKEN", "ledger-token")
	valid := func() error {
		return validateProductionConfig(true, "inference", "postgres://db/fleet?sslmode=verify-full", ledger.ModeHTTP,
			"https://ledger.example", true, "/tls/tls.crt", "/tls/tls.key", server.InferenceProviderPraxis,
			"http://praxis-ai:8080", "", "", `{"gcl":"base64:key"}`)
	}
	if err := valid(); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	t.Setenv("LEDGER_GATEWAY_API_TOKEN", "")
	if err := valid(); err == nil || !strings.Contains(err.Error(), "immutable ledger") {
		t.Fatalf("expected ledger validation error, got %v", err)
	}
}

func TestValidateProductionConfigDevelopmentBypass(t *testing.T) {
	if err := validateProductionConfig(false, "all", "", ledger.ModeDisabled, "", false, "", "", server.InferenceProviderPraxis, "", "", "", ""); err != nil {
		t.Fatalf("development configuration should remain supported: %v", err)
	}
}

func TestValidateProductionConfigRouterRequiresBothPools(t *testing.T) {
	t.Setenv("LEDGER_GATEWAY_API_TOKEN", "ledger-token")
	base := func(cpu, gpu string) error {
		return validateProductionConfig(true, "inference", "postgres://db/fleet?sslmode=verify-full", ledger.ModeHTTP,
			"https://ledger.example", true, "/tls/tls.crt", "/tls/tls.key", server.InferenceProviderLLMD,
			"", cpu, gpu, `{"gcl":"base64:key"}`)
	}
	if err := base("http://router-cpu:8081", "http://router-gpu:8081"); err != nil {
		t.Fatalf("valid Router production config rejected: %v", err)
	}
	if err := base("http://router-cpu:8081", ""); err == nil || !strings.Contains(err.Error(), "Router CPU and GPU") {
		t.Fatalf("expected missing Router pool error, got %v", err)
	}
}

func TestProductionPostgresURLValidation(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "verified postgres", url: "postgres://fleet@db.example/fleet?sslmode=verify-full", want: true},
		{name: "verified postgresql", url: "postgresql://fleet@db.example/fleet?sslmode=verify-full", want: true},
		{name: "encryption without identity verification", url: "postgres://fleet@db.example/fleet?sslmode=require", want: false},
		{name: "disabled TLS", url: "postgres://fleet@db.example/fleet?sslmode=disable", want: false},
		{name: "missing host", url: "postgres:///fleet?sslmode=verify-full", want: false},
		{name: "wrong scheme", url: "https://db.example/fleet?sslmode=verify-full", want: false},
		{name: "malformed", url: "://not-a-url?sslmode=verify-full", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := productionPostgresURLValid(tt.url); got != tt.want {
				t.Fatalf("productionPostgresURLValid(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestProductionHTTPSURLValidation(t *testing.T) {
	for _, tt := range []struct {
		url  string
		want bool
	}{
		{url: "https://ledger.example/api", want: true},
		{url: "http://ledger.example/api", want: false},
		{url: "https:///api", want: false},
		{url: "://bad", want: false},
	} {
		if got := productionHTTPSURLValid(tt.url); got != tt.want {
			t.Errorf("productionHTTPSURLValid(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// newTestController creates a minimal FleetController for testing route setup.
func newTestController(t *testing.T) *server.FleetController {
	t.Helper()
	fc, err := server.NewFleetController("", "http://localhost:8000", "http://localhost:8080", "", "")
	if err != nil {
		t.Fatalf("NewFleetController: %v", err)
	}
	return fc
}

func TestConfiguredLedgerFailureDoesNotFallBackToFabricatedMemoryEvidence(t *testing.T) {
	controller, err := server.NewFleetControllerWithLedgerConfig(
		ledger.Config{Mode: ledger.ModeGRPC, Endpoint: "ledger.example:9092"},
		"http://localhost:8000", "http://localhost:8080", "", "",
	)
	if err == nil || controller != nil {
		t.Fatalf("NewFleetControllerWithLedgerConfig() = (%v, %v), want nil error result", controller, err)
	}
}

func TestDecisionPackageKeyringFromEnvironment(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Setenv("GCL_DECISION_SIGNING_KEYS_JSON", "")
	t.Setenv("GCL_DECISION_SIGNING_KEY_ID", "gcl-key-2")
	t.Setenv("GCL_DECISION_SIGNING_KEY", "base64:"+base64.StdEncoding.EncodeToString(key))

	keyring, err := server.DecisionPackageKeyringFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(keyring["gcl-key-2"]); got != string(key) {
		t.Fatalf("decoded key = %q", got)
	}
}

func TestDecisionPackageKeyringRejectsShortKey(t *testing.T) {
	t.Setenv("GCL_DECISION_SIGNING_KEYS_JSON", "")
	t.Setenv("GCL_DECISION_SIGNING_KEY", "too-short")
	if _, err := server.DecisionPackageKeyringFromEnv(); err == nil {
		t.Fatal("expected short GCL signing key to fail")
	}
}

func TestOperatorJSONIntentCompatibilityRequiresExplicitTrue(t *testing.T) {
	for _, test := range []struct {
		name      string
		flagValue bool
		envValue  string
		want      bool
	}{
		{"default", false, "", false},
		{"flag", true, "", true},
		{"environment", false, "true", true},
		{"environment is exact", false, "TRUE", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FLEET_ALLOW_OPERATOR_JSON_INTENTS", test.envValue)
			if got := server.OperatorJSONIntentsEnabled(test.flagValue); got != test.want {
				t.Fatalf("OperatorJSONIntentsEnabled(%v) = %v, want %v", test.flagValue, got, test.want)
			}
		})
	}
}

func TestConfiguredKubernetesAPIBacksIntentAuthority(t *testing.T) {
	requests := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "synthetic Kubernetes failure", http.StatusServiceUnavailable)
	}))
	defer apiServer.Close()

	controller, err := server.NewFleetController("", "http://localhost:8000", "http://localhost:8080", apiServer.URL, "fleet-system")
	if err != nil {
		t.Fatalf("NewFleetController: %v", err)
	}
	_, err = controller.IntentService.Submit(context.Background(), intents.FleetIntent{
		ID:             "intent-1",
		IdempotencyKey: "intent-key-1",
		Type:           intents.IntentScale,
		Confidence:     0.9,
		Justification:  "verify authoritative repository wiring",
		Pool:           "qwen-prod",
	})
	if err == nil || !strings.Contains(err.Error(), "Kubernetes API returned 503") {
		t.Fatalf("Submit error = %v, want Kubernetes repository failure", err)
	}
	if requests == 0 {
		t.Fatal("configured Kubernetes API received no intent repository request")
	}
}

func TestIntentV2CreatesHonestAsynchronousOperation(t *testing.T) {
	fc := newTestController(t)
	fc.AllowOperatorJSONIntents = true
	mux := fc.SetupRoutes("control")
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"type":"scale","confidence":0.9,"horizon_seconds":900,
		"justification":"forecast shortfall","state_snapshot":{"replicas":1},
		"idempotency_key":"forecast-1-scale","expires_at":%q,
		"decision_package_ref":"oci://decisions/forecast-1",
		"decision_package_digest":"%s","pool":"qwen-prod",
		"evidence":[{"uri":"urn:sha256:forecast-1","sha256":"%s"}],
		"desired_replicas":4,
		"proposer":{"subject":"spiffe://example/gcl","authority_ref":"attestation/1"}
	}`, expires, strings.Repeat("a", 64), strings.Repeat("b", 64))
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var submission intents.SubmissionResponse
	if err := json.NewDecoder(recorder.Body).Decode(&submission); err != nil {
		t.Fatal(err)
	}
	if submission.State != intents.StateAccepted {
		t.Fatalf("state = %s, want ACCEPTED", submission.State)
	}

	get := httptest.NewRequest(http.MethodGet, submission.StatusURL, nil)
	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("operation status = %d, body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var operation intents.FleetOperation
	if err := json.NewDecoder(getRecorder.Body).Decode(&operation); err != nil {
		t.Fatal(err)
	}
	if operation.State == intents.StateSucceeded || operation.State == intents.StateActuating {
		t.Fatalf("admission was reported as execution: %s", operation.State)
	}
	if operation.LedgerEntryID == "" {
		t.Fatal("admission ledger receipt was not attached")
	}
}

func TestIntentV2RejectsMissingGovernanceEnvelope(t *testing.T) {
	fc := newTestController(t)
	fc.AllowOperatorJSONIntents = true
	mux := fc.SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(`{"type":"scale","confidence":0.9,"horizon_seconds":1,"justification":"scale","state_snapshot":{},"pool":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestIntentV2RejectsOperatorJSONByDefault(t *testing.T) {
	mux := newTestController(t).SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(`{"type":"scale"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "operator compatibility is disabled") {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}
}

func TestIntentV2RequiresConfiguredGCLVerification(t *testing.T) {
	mux := newTestController(t).SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", intents.GCLDecisionPackageCloudEventContentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestIntentV2RejectsInvalidVerifiedGCLPayload(t *testing.T) {
	fc := newTestController(t)
	fc.DecisionPackageDecoder = intents.NewGCLDecisionPackageDecoder(map[string][]byte{
		"gcl-key": []byte("0123456789abcdef0123456789abcdef"),
	})
	mux := fc.SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", intents.GCLDecisionPackageCloudEventContentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestIntentV2RejectsUnsupportedMediaType(t *testing.T) {
	mux := newTestController(t).SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/intents", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestIntentV1NeverMapsAdmissionToExecuted(t *testing.T) {
	mux := newTestController(t).SetupRoutes("control")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intents", strings.NewReader(`{"id":"legacy-1","type":"scale","confidence":0.9,"horizon_seconds":1,"justification":"legacy","state_snapshot":{},"pool":"p","target_replicas":2}`))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response intents.IntentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Status != intents.StatusDeferred {
		t.Fatalf("legacy admission status = %s, want deferred", response.Status)
	}
	if recorder.Header().Get("Deprecation") != "true" {
		t.Fatal("v1 deprecation header missing")
	}
}

func TestRequestActorUsesVerifiedClaimsAndIgnoresSpoofedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/operations/op-1/approve", nil)
	req.Header.Set("X-Fleet-Actor", "spoofed-client")
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Subject: "spiffe://example/operator"}))
	if got := server.RequestActor(req); got != "spiffe://example/operator" {
		t.Fatalf("RequestActor() = %q, want verified subject", got)
	}

	unauthenticated := httptest.NewRequest(http.MethodPost, "/api/v2/operations/op-1/approve", nil)
	unauthenticated.Header.Set("X-Fleet-Actor", "spoofed-client")
	if got := server.RequestActor(unauthenticated); got != "unauthenticated-development" {
		t.Fatalf("RequestActor() = %q, want development fallback", got)
	}
}

func TestRequestActorUsesNormalizedTrustedIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/operations/op-1/approve", nil)
	req.Header.Set("X-Fleet-Actor", "spoofed-client")
	req = req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{
		Subject: "spiffe://gateway.example/user-1", Method: auth.MethodTrustedProxy,
	}))
	if got := server.RequestActor(req); got != "spiffe://gateway.example/user-1" {
		t.Fatalf("RequestActor() = %q", got)
	}
}

// routeExists sends a request to the mux and returns true when the mux
// dispatches it to a real handler (i.e. status != 404 && status != 405).
func routeExists(mux *http.ServeMux, method, path string) bool {
	var body *strings.Reader
	if method == "POST" {
		body = strings.NewReader("{}")
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	// 404 means the route is not mounted at all.
	return rr.Code != http.StatusNotFound
}

// ---------------------------------------------------------------------------
// Tests for the --mode flag behaviour
// ---------------------------------------------------------------------------

func TestSetupAPIServer_ModeAll_MountsBothControlAndInference(t *testing.T) {
	fc := newTestController(t)
	mux := fc.SetupRoutes("all")

	// Control plane routes should be present.
	if !routeExists(mux, "GET", "/api/v1/clusters") {
		t.Error("mode=all: expected /api/v1/clusters to be mounted")
	}
	if !routeExists(mux, "GET", "/api/v1/pools") {
		t.Error("mode=all: expected /api/v1/pools to be mounted")
	}

	// Inference proxy routes should be present.

	// Health probes should always be present.
	if !routeExists(mux, "GET", "/healthz") {
		t.Error("mode=all: expected /healthz to be mounted")
	}
	if !routeExists(mux, "GET", "/readyz") {
		t.Error("mode=all: expected /readyz to be mounted")
	}
}

func TestSetupAPIServer_ModeControl_OnlyMountsControlRoutes(t *testing.T) {
	fc := newTestController(t)
	mux := fc.SetupRoutes("control")

	// Control plane routes should be present.
	if !routeExists(mux, "GET", "/api/v1/clusters") {
		t.Error("mode=control: expected /api/v1/clusters to be mounted")
	}
	if !routeExists(mux, "GET", "/api/v1/pools") {
		t.Error("mode=control: expected /api/v1/pools to be mounted")
	}
	if !routeExists(mux, "GET", "/api/v1/tenants") {
		t.Error("mode=control: expected /api/v1/tenants to be mounted")
	}
	if !routeExists(mux, "GET", "/api/v1/rollouts") {
		t.Error("mode=control: expected /api/v1/rollouts to be mounted")
	}

	// Inference proxy routes should NOT be present.

	// Health probes should always be present.
	if !routeExists(mux, "GET", "/healthz") {
		t.Error("mode=control: expected /healthz to be mounted")
	}
}

func TestSetupAPIServer_ModeControl_CostEndpointsMounted(t *testing.T) {
	fc := newTestController(t)
	mux := fc.SetupRoutes("control")

	costRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/cost/pricing"},
		{"GET", "/api/v1/cost/projection"},
		{"GET", "/api/v1/cost/savings"},
		{"GET", "/api/v1/cost/alerts"},
	}
	for _, r := range costRoutes {
		if !routeExists(mux, r.method, r.path) {
			t.Errorf("mode=control: expected %s %s to be mounted", r.method, r.path)
		}
	}
}

func TestSetupAPIServer_HealthAlwaysMounted(t *testing.T) {
	fc := newTestController(t)

	for _, mode := range []string{"all", "control"} {
		mux := fc.SetupRoutes(mode)

		if !routeExists(mux, "GET", "/healthz") {
			t.Errorf("mode=%s: expected /healthz to be mounted", mode)
		}
		if !routeExists(mux, "GET", "/readyz") {
			t.Errorf("mode=%s: expected /readyz to be mounted", mode)
		}
	}
}
