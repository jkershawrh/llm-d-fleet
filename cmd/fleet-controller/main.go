package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
	fleetcontroller "github.com/llm-d/fleet-llm-d/pkg/controller"
	"github.com/llm-d/fleet-llm-d/pkg/intents"
	"github.com/llm-d/fleet-llm-d/pkg/ledger"
	"github.com/llm-d/fleet-llm-d/pkg/routing"
	"github.com/llm-d/fleet-llm-d/pkg/server"
	"github.com/llm-d/fleet-llm-d/pkg/store/events"
	"github.com/llm-d/fleet-llm-d/pkg/store/postgres"
	"github.com/llm-d/fleet-llm-d/pkg/tenant/quota"
	"github.com/llm-d/fleet-llm-d/pkg/tlsutil"
)

func main() {
	port := flag.Int("port", 8080, "API server port")
	metricsPort := flag.Int("metrics-port", 9091, "Metrics server port")
	grpcPort := flag.Int("grpc-port", 0, "gRPC (JSON-RPC) server port; 0 disables")
	mode := flag.String("mode", "all", "Server mode: all, control, inference, publisher, or endpoint-mirror")
	production := flag.Bool("production", false, "Require production-safe authentication, persistence, ledger, TLS, and signing configuration")
	ledgerMode := flag.String("ledger-mode", string(ledger.ModeDisabled), "Ledger backend mode: disabled (default), memory (development only -- evidence is fabricated and lost on restart), http (standalone are-immutable-ledger REST compatibility gateway). gRPC is canonical upstream but not yet generated in this binary")
	ledgerEndpoint := flag.String("ledger-endpoint", "http://localhost:18099", "standalone immutable-ledger REST gateway endpoint (HTTP compatibility mode only)")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	tlsCert := flag.String("tls-cert", "", "Path to TLS certificate file")
	tlsKey := flag.String("tls-key", "", "Path to TLS private key file")
	_ = flag.String("backend-vllm", "", "Deprecated: inference routing moved to Praxis")
	_ = flag.String("backend-ovms", "", "Deprecated: inference routing moved to Praxis")
	kubeAPI := flag.String("kube-api", "", "Kubernetes API server URL (enables CRD watching and authoritative intent persistence when set)")
	namespace := flag.String("namespace", "default", "Kubernetes namespace to watch for FleetInferencePool CRDs")
	pgURL := flag.String("pg-url", "", "PostgreSQL connection string (e.g. postgres://user:pass@host:5432/fleet?sslmode=disable). When set, uses PostgreSQL instead of in-memory stores")
	eventEndpoint := flag.String("event-endpoint", "", "HTTP endpoint for publishing fleet events (e.g. http://kafka-bridge:8080/topics/fleet-events). When set, events are also POSTed to this URL")
	modelplaneAPI := flag.String("modelplane-api", "", "ModelPlane API server URL (enables ModelPlane integration when set)")
	modelplaneNamespace := flag.String("modelplane-namespace", "default", "ModelPlane namespace to watch for resources")
	rateLimit := flag.Float64("rate-limit", 100, "Rate limit in requests per second per IP (0 to disable)")
	rateBurst := flag.Int("rate-burst", 200, "Rate limit burst size (max requests before throttling)")
	rateLimitExempt := flag.String("rate-limit-exempt", "/healthz,/readyz,/metrics", "Comma-separated exact paths exempt from rate limiting and auth")
	trustProxyHeaders := flag.Bool("trust-proxy-headers", false, "Honour X-Forwarded-For when identifying clients for rate limiting. Enable ONLY when every request arrives through a proxy that overwrites the header; otherwise clients can forge their own rate-limit identity")
	identityProvider := flag.String("identity-provider", "", "Request identity provider: disabled, hmac, or trusted-proxy (env FLEET_IDENTITY_PROVIDER; default preserves existing HMAC behavior)")
	praxisURL := flag.String("praxis-url", "", "Internal Praxis inference endpoint")
	inferenceProvider := flag.String("inference-provider", "", "Inference data plane: praxis (default) or llm-d-router")
	llmdCPUURL := flag.String("llm-d-router-cpu-url", "", "Internal llm-d Router CPU proxy endpoint")
	llmdGPUURL := flag.String("llm-d-router-gpu-url", "", "Internal llm-d Router GPU proxy endpoint")
	routingProvider := flag.String("routing-provider", "", "Authoritative routing adapter: praxis (default), llm-d-router, or disabled")
	inferenceMaxInflight := flag.Int("inference-max-inflight", 100, "Maximum concurrent inference requests per gateway replica")
	_ = flag.String("backends", "", "Deprecated: inference routing moved to Praxis")
	_ = flag.Int("max-inflight", 0, "Deprecated: load shedding moved to Praxis")
	allowOperatorJSONIntents := flag.Bool("allow-operator-json-intents", false, "Enable unsigned application/json v2 intent input for development/operator compatibility only")
	flag.Parse()
	if *praxisURL == "" {
		*praxisURL = os.Getenv("PRAXIS_URL")
	}
	if *inferenceProvider == "" {
		*inferenceProvider = os.Getenv("FLEET_INFERENCE_PROVIDER")
	}
	parsedInferenceProvider, err := server.ParseInferenceProviderName(*inferenceProvider)
	if err != nil {
		slog.Error("invalid inference provider", "error", err)
		os.Exit(1)
	}
	if *llmdCPUURL == "" {
		*llmdCPUURL = os.Getenv("FLEET_LLMD_ROUTER_CPU_URL")
	}
	if *llmdGPUURL == "" {
		*llmdGPUURL = os.Getenv("FLEET_LLMD_ROUTER_GPU_URL")
	}
	if *routingProvider == "" {
		*routingProvider = os.Getenv("FLEET_ROUTING_PROVIDER")
	}
	parsedRoutingProvider, err := routing.ParseProviderName(*routingProvider)
	if err != nil {
		slog.Error("invalid routing provider", "error", err)
		os.Exit(1)
	}
	if err := os.Setenv("FLEET_ROUTING_PROVIDER", string(parsedRoutingProvider)); err != nil {
		slog.Error("configure routing provider", "error", err)
		os.Exit(1)
	}

	if *pgURL == "" {
		if env := os.Getenv("PG_URL"); env != "" {
			*pgURL = env
		}
	}

	// Configure structured JSON logging.
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	slog.Info("fleet-controller starting", "mode", *mode, "log_level", *logLevel, "ledger_mode", *ledgerMode, "ledger", *ledgerEndpoint, "grpc_port", *grpcPort)
	if *mode == "endpoint-mirror" {
		if *kubeAPI == "" {
			slog.Error("endpoint mirror requires --kube-api")
			os.Exit(1)
		}
		tlsConfig, err := tlsutil.NewTLSConfig(tlsutil.KubernetesTLSOptions())
		if err != nil {
			slog.Error("endpoint mirror Kubernetes trust failed", "error", err)
			os.Exit(1)
		}
		configMap := strings.TrimSpace(os.Getenv("LLMD_ROUTER_ENDPOINTS_CONFIGMAP"))
		directory := strings.TrimSpace(os.Getenv("LLMD_ROUTER_ENDPOINTS_DIR"))
		if directory == "" {
			directory = "/var/run/fleet-router"
		}
		err = routing.RunKubernetesConfigMapMirror(context.Background(), routing.KubernetesConfigMapMirrorOptions{
			APIURL: *kubeAPI, Namespace: *namespace, Name: configMap, Directory: directory,
			TokenFile: tlsutil.ServiceAccountTokenPath, Interval: 2 * time.Second,
			HTTPClient: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}},
			OnPublished: func(resourceVersion string) {
				slog.Info("Router endpoint files mirrored", "resource_version", resourceVersion)
			},
			OnError: func(err error) {
				slog.Warn("Router endpoint mirror sync failed", "error", err)
			},
		})
		if err != nil {
			slog.Error("endpoint mirror exited", "error", err)
			os.Exit(1)
		}
		return
	}

	if ledger.Mode(*ledgerMode) == ledger.ModeMemory {
		slog.Warn("ledger mode is 'memory': receipts are fabricated in-process and lost on restart. " +
			"This is NOT tamper-evident evidence. Use --ledger-mode=http against the standalone " +
			"are-immutable-ledger for anything that must be auditable.")
	}

	authCfg, err := auth.ConfigFromEnvWithProvider(*identityProvider)
	if err != nil {
		slog.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	if !authCfg.Enabled {
		slog.Warn("authentication is DISABLED: every API request will be served unauthenticated. " +
			"Set FLEET_AUTH_SECRET or FLEET_AUTH_SECRET_FILE before exposing this controller.")
	}
	if *grpcPort > 0 && authCfg.Provider != auth.ProviderHMAC {
		slog.Error("JSON-RPC identity configuration rejected", "identity_provider", authCfg.Provider,
			"error", "JSON-RPC currently requires the hmac identity provider")
		os.Exit(1)
	}
	if err := validateProductionConfig(*production, *mode, *pgURL, ledger.Mode(*ledgerMode), *ledgerEndpoint, authCfg.Enabled, *tlsCert, *tlsKey, parsedInferenceProvider, *praxisURL, *llmdCPUURL, *llmdGPUURL, os.Getenv("GCL_DECISION_SIGNING_KEYS_JSON")); err != nil {
		slog.Error("production configuration rejected", "error", err)
		os.Exit(1)
	}
	slog.Info("configuration loaded", "auth_enabled", authCfg.Enabled, "identity_provider", authCfg.Provider, "tls_enabled", *tlsCert != "" && *tlsKey != "", "kube_api", *kubeAPI, "namespace", *namespace, "postgres", *pgURL != "", "event_endpoint", *eventEndpoint)

	fc, err := server.NewFleetControllerWithLedgerConfig(ledger.Config{
		Mode:     ledger.Mode(*ledgerMode),
		Endpoint: *ledgerEndpoint,
		APIToken: os.Getenv("LEDGER_GATEWAY_API_TOKEN"),
	}, "", "", *kubeAPI, *namespace)
	if err != nil {
		slog.Error("invalid immutable-ledger configuration", "error", err)
		os.Exit(1)
	}
	if *mode == "publisher" {
		// Publisher mode needs Kubernetes only for leader election and the
		// ConfigMap adapter. It must not claim CRD-watcher readiness or actuate
		// serving resources.
		fc.CRDWatcher = nil
		fc.Actuator = nil
	}
	if *kubeAPI != "" && *mode != "inference" {
		identity := os.Getenv("POD_NAME")
		if identity == "" {
			identity, err = os.Hostname()
			if err != nil || identity == "" {
				slog.Error("leader election requires POD_NAME or a resolvable hostname")
				os.Exit(1)
			}
		}
		fc.ConfigureLeaderElection(fleetcontroller.NewLeaderElector(*kubeAPI, *namespace, identity))
		slog.Info("leader election enabled", "identity", identity, "namespace", *namespace)
	}

	decisionKeys, err := server.DecisionPackageKeyringFromEnv()
	if err != nil {
		slog.Error("invalid GCL DecisionPackage verification configuration", "error", err)
		os.Exit(1)
	}
	if len(decisionKeys) > 0 {
		allowLegacyHMAC := os.Getenv("FLEET_ALLOW_HMAC_DECISION_PACKAGES") == "true"
		fc.DecisionPackageDecoder = intents.NewGCLDecisionPackageDecoderWithPolicy(decisionKeys, allowLegacyHMAC)
		slog.Info("GCL DecisionPackage verification enabled",
			"trusted_keys", len(decisionKeys), "allow_legacy_hmac", allowLegacyHMAC)
		if allowLegacyHMAC {
			slog.Warn("legacy HMAC-SHA256 DecisionPackages are accepted: the signing key is shared with GCL, " +
				"so a signature no longer proves GCL authorship. Unset " +
				"FLEET_ALLOW_HMAC_DECISION_PACKAGES once GCL signs with Ed25519.")
		}
	}
	fc.AllowOperatorJSONIntents = server.OperatorJSONIntentsEnabled(*allowOperatorJSONIntents)
	if fc.AllowOperatorJSONIntents {
		slog.Warn("unsigned application/json v2 intent compatibility is enabled; do not use this ingress as GCL provenance")
	}

	fc.AuthSecret = authCfg.Secret
	fc.InferenceProviderName = parsedInferenceProvider
	fc.PraxisURL = *praxisURL
	fc.PraxisToken = os.Getenv("PRAXIS_API_TOKEN")
	fc.LLMDCPUURL = *llmdCPUURL
	fc.LLMDGPUURL = *llmdGPUURL
	fc.LLMDToken = os.Getenv("FLEET_LLMD_ROUTER_API_TOKEN")
	if raw := os.Getenv("FLEET_ROUTER_UPSTREAM_CLUSTERS_JSON"); raw != "" {
		var upstreamClusters map[string]string
		if err := json.Unmarshal([]byte(raw), &upstreamClusters); err != nil {
			slog.Error("invalid Router upstream cluster mapping", "error", err)
			os.Exit(1)
		}
		fc.RouterUpstreamClusters = make(map[string]string, len(upstreamClusters))
		for upstream, cluster := range upstreamClusters {
			fc.RouterUpstreamClusters[strings.ToLower(strings.TrimSpace(upstream))] = strings.TrimSpace(cluster)
		}
	}
	fc.CPUPhysicalModel = os.Getenv("FLEET_CPU_PHYSICAL_MODEL")
	fc.GPUPhysicalModel = os.Getenv("FLEET_GPU_PHYSICAL_MODEL")
	if raw := os.Getenv("FLEET_MODEL_PROVIDERS_JSON"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &fc.ModelProviderClusters); err != nil {
			slog.Error("invalid exact-model provider mapping", "error", err)
			os.Exit(1)
		}
		for rawModel, providers := range fc.ModelProviderClusters {
			model := strings.TrimSpace(rawModel)
			clean := make([]string, 0, len(providers))
			seen := make(map[string]bool, len(providers))
			for _, provider := range providers {
				if provider = strings.TrimSpace(provider); provider != "" && !seen[provider] {
					clean = append(clean, provider)
					seen[provider] = true
				}
			}
			if model == "" || len(clean) == 0 {
				slog.Error("exact-model provider mapping contains an empty model or provider set")
				os.Exit(1)
			}
			delete(fc.ModelProviderClusters, rawModel)
			fc.ModelProviderClusters[model] = clean
		}
	}
	if raw := os.Getenv("FLEET_STATIC_PROVIDER_IDS_JSON"); raw != "" {
		if err := bootstrapStaticProviders(context.Background(), fc.ClusterRepo, raw, *production || *pgURL != ""); err != nil {
			slog.Error("invalid static provider bootstrap configuration", "error", err)
			os.Exit(1)
		}
	}
	if raw := os.Getenv("FLEET_STATIC_ROUTING_STATE_JSON"); raw != "" {
		if err := bootstrapStaticRoutingState(context.Background(), fc.ClusterRepo, fc.PoolRepo, raw, *production || *pgURL != ""); err != nil {
			slog.Error("invalid static routing state", "error", err)
			os.Exit(1)
		}
	}
	if raw := os.Getenv("FLEET_PROVIDER_HEALTH_URLS_JSON"); raw != "" {
		var providerURLs map[string]string
		if err := json.Unmarshal([]byte(raw), &providerURLs); err != nil {
			slog.Error("invalid provider health URL configuration", "error", err)
			os.Exit(1)
		}
		providerHealth, err := server.NewProviderHealthCache(providerURLs, os.Getenv("FLEET_PROVIDER_CA_FILE"))
		if err != nil {
			slog.Error("invalid provider health probe configuration", "error", err)
			os.Exit(1)
		}
		initializeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := providerHealth.Initialize(initializeCtx); err != nil {
			cancel()
			slog.Error("provider health initialization failed", "error", err)
			os.Exit(1)
		}
		cancel()
		fc.ProviderHealth = providerHealth
		providerHealth.Start(context.Background())
		slog.Info("active provider health probes enabled", "providers", len(providerURLs))
	}
	if *inferenceMaxInflight > 0 {
		fc.InferenceSlots = make(chan struct{}, *inferenceMaxInflight)
	}
	fc.InferenceClient = &http.Client{Timeout: 180 * time.Second}

	if *pgURL != "" {
		db, err := sql.Open("postgres", *pgURL)
		if err != nil {
			slog.Error("failed to open postgres", "error", err)
			os.Exit(1)
		}
		defer db.Close()
		if err := fc.OverrideWithPostgres(db); err != nil {
			slog.Error("postgres override failed", "error", err)
			os.Exit(1)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("FLEET_FALLBACK_QUOTA_JSON")); raw != "" {
		fallback, err := parseFallbackQuota(raw, *production || *pgURL != "")
		if err != nil {
			slog.Error("fallback quota configuration rejected", "error", err)
			os.Exit(1)
		}
		fc.QuotaEnforcer = quota.NewQuotaEnforcerWithFallback(fallback, fc.TenantRepo)
		slog.Warn("certification fallback quota enabled",
			"token_limit_per_minute", fallback.TokenLimitPerMinute,
			"concurrent_limit", fallback.ConcurrentLimit,
			"monthly_budget_cents", fallback.MonthlyBudgetCents)
	}

	fc.InitGauges(context.Background())

	if *eventEndpoint != "" {
		fc.EventPublisher = events.NewLedgerAwarePublisher(events.NewHTTPEventPublisher(*eventEndpoint), fc.FleetRecorder)
		slog.Info("event publishing enabled", "endpoint", *eventEndpoint)
	}

	if *modelplaneAPI != "" {
		fc.WireModelPlane(*modelplaneAPI, *modelplaneNamespace)
	}

	var rl *auth.RateLimiter
	if *rateLimit > 0 {
		rl = auth.NewRateLimiter(*rateLimit, *rateBurst)
		rl.TrustProxyHeaders(*trustProxyHeaders)
		defer rl.Stop()
		slog.Info("rate limiting enabled",
			"rate", *rateLimit, "burst", *rateBurst,
			"trust_proxy_headers", *trustProxyHeaders)
	}

	if err := fc.Run(context.Background(), *port, *metricsPort, *grpcPort, authCfg, *tlsCert, *tlsKey, *mode, rl, server.SplitCSV(*rateLimitExempt)); err != nil {
		slog.Error("fleet-controller exited", "error", err)
		os.Exit(1)
	}
}

func parseFallbackQuota(raw string, durable bool) (quota.FallbackConfig, error) {
	if durable {
		return quota.FallbackConfig{}, fmt.Errorf("FLEET_FALLBACK_QUOTA_JSON is certification-only and cannot be used with production mode or PostgreSQL")
	}
	var config quota.FallbackConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return quota.FallbackConfig{}, fmt.Errorf("parse fallback quota: %w", err)
	}
	if config.TokenLimitPerMinute <= 0 || config.ConcurrentLimit <= 0 || config.MonthlyBudgetCents <= 0 {
		return quota.FallbackConfig{}, fmt.Errorf("fallback quota limits must all be positive")
	}
	return config, nil
}

func bootstrapStaticProviders(ctx context.Context, repo postgres.ClusterRepository, raw string, durable bool) error {
	if durable {
		return fmt.Errorf("FLEET_STATIC_PROVIDER_IDS_JSON is certification-only and cannot be used with production mode or PostgreSQL")
	}
	var providerIDs []string
	if err := json.Unmarshal([]byte(raw), &providerIDs); err != nil {
		return fmt.Errorf("parse provider IDs: %w", err)
	}
	if len(providerIDs) == 0 {
		return fmt.Errorf("provider ID list is empty")
	}
	now := time.Now().UTC()
	seen := make(map[string]bool, len(providerIDs))
	for _, providerID := range providerIDs {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			return fmt.Errorf("provider ID is empty")
		}
		if seen[providerID] {
			continue
		}
		seen[providerID] = true
		if _, err := repo.Get(ctx, providerID); err == nil {
			continue
		}
		if err := repo.Create(ctx, postgres.ClusterRecord{
			ID: providerID, Name: providerID, Status: postgres.ClusterStatusRunning,
			RegisteredAt: now, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("create provider %s: %w", providerID, err)
		}
	}
	slog.Warn("static certification provider inventory enabled", "providers", len(seen))
	return nil
}

type staticRoutingState struct {
	Providers []struct {
		ID             string   `json:"id"`
		RoutingURL     string   `json:"routingURL"`
		MetricsURL     string   `json:"metricsURL"`
		PhysicalModels []string `json:"physicalModels"`
		FailureDomain  string   `json:"failureDomain"`
	} `json:"providers"`
	Pools []struct {
		Model     string   `json:"model"`
		Providers []string `json:"providers"`
	} `json:"pools"`
}

func bootstrapStaticRoutingState(ctx context.Context, clusters postgres.ClusterRepository, pools postgres.FleetPoolRepository, raw string, durable bool) error {
	if durable {
		return fmt.Errorf("FLEET_STATIC_ROUTING_STATE_JSON is certification-only and cannot be used with production mode or PostgreSQL")
	}
	var state staticRoutingState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return fmt.Errorf("parse static routing state: %w", err)
	}
	if len(state.Providers) == 0 || len(state.Pools) == 0 {
		return fmt.Errorf("static routing state requires providers and pools")
	}
	now := time.Now().UTC()
	providerIDs := make(map[string]bool, len(state.Providers))
	for _, provider := range state.Providers {
		provider.ID = strings.TrimSpace(provider.ID)
		routingURL, err := url.Parse(strings.TrimSpace(provider.RoutingURL))
		if provider.ID == "" || err != nil || routingURL.Scheme != "https" || routingURL.Hostname() == "" {
			return fmt.Errorf("provider %q requires a valid HTTPS routingURL", provider.ID)
		}
		if providerIDs[provider.ID] {
			return fmt.Errorf("duplicate provider %q", provider.ID)
		}
		providerIDs[provider.ID] = true
		labels := map[string]string{
			"fleet.llm-d.ai/egress-address":   provider.RoutingURL,
			"fleet.llm-d.ai/metrics-endpoint": provider.MetricsURL,
			"fleet.llm-d.ai/tls-server-name":  routingURL.Hostname(),
			"fleet.llm-d.ai/physical-models":  strings.Join(provider.PhysicalModels, ","),
			"fleet.llm-d.ai/authorized":       "true",
			"topology.kubernetes.io/zone":     provider.FailureDomain,
		}
		record := postgres.ClusterRecord{ID: provider.ID, Name: provider.ID, Labels: labels, Status: postgres.ClusterStatusRunning, RegisteredAt: now, UpdatedAt: now}
		if existing, err := clusters.Get(ctx, provider.ID); err == nil {
			record.RegisteredAt = existing.RegisteredAt
			if err := clusters.Update(ctx, record); err != nil {
				return fmt.Errorf("update static provider %s: %w", provider.ID, err)
			}
		} else if err := clusters.Create(ctx, record); err != nil {
			return fmt.Errorf("create static provider %s: %w", provider.ID, err)
		}
	}
	for _, pool := range state.Pools {
		pool.Model = strings.TrimSpace(pool.Model)
		if pool.Model == "" || len(pool.Providers) == 0 {
			return fmt.Errorf("static pool requires a model and providers")
		}
		for _, providerID := range pool.Providers {
			if !providerIDs[providerID] {
				return fmt.Errorf("pool %q references unknown provider %q", pool.Model, providerID)
			}
		}
		if err := pools.Create(ctx, postgres.FleetPoolRecord{ID: pool.Model, Name: pool.Model, ModelName: pool.Model, DesiredClusters: pool.Providers, TargetPorts: []int{443}, Status: "Active", CreatedAt: now, UpdatedAt: now}); err != nil {
			return fmt.Errorf("create static pool %s: %w", pool.Model, err)
		}
	}
	slog.Warn("static certification routing state enabled", "providers", len(state.Providers), "pools", len(state.Pools))
	return nil
}

func validateProductionConfig(production bool, mode, pgURL string, ledgerMode ledger.Mode, ledgerEndpoint string, authEnabled bool, tlsCert, tlsKey string, inferenceProvider server.InferenceProviderName, praxisURL, llmdCPUURL, llmdGPUURL, decisionKeys string) error {
	if !production {
		return nil
	}
	var missing []string
	if !authEnabled {
		missing = append(missing, "authentication")
	}
	if !productionPostgresURLValid(pgURL) {
		missing = append(missing, "TLS PostgreSQL")
	}
	if ledgerMode != ledger.ModeHTTP || !productionHTTPSURLValid(ledgerEndpoint) || os.Getenv("LEDGER_GATEWAY_API_TOKEN") == "" {
		missing = append(missing, "authenticated HTTPS immutable ledger")
	}
	if tlsCert == "" || tlsKey == "" {
		missing = append(missing, "API TLS certificate and key")
	}
	if decisionKeys == "" {
		missing = append(missing, "GCL Ed25519 verification keys")
	}
	if mode == "all" || mode == "inference" {
		switch inferenceProvider {
		case server.InferenceProviderPraxis:
			if praxisURL == "" {
				missing = append(missing, "Praxis URL")
			}
		case server.InferenceProviderLLMD:
			if llmdCPUURL == "" || llmdGPUURL == "" {
				missing = append(missing, "llm-d Router CPU and GPU URLs")
			}
		default:
			missing = append(missing, "supported inference provider")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing or unsafe production requirements: %s", strings.Join(missing, ", "))
	}
	if mode != "all" && mode != "control" && mode != "inference" && mode != "publisher" {
		return fmt.Errorf("invalid server mode %q", mode)
	}
	return nil
}

func productionPostgresURLValid(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" {
		return false
	}
	return parsed.Query().Get("sslmode") == "verify-full"
}

func productionHTTPSURLValid(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}
