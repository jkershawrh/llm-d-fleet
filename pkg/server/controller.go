package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	v1alpha1 "github.com/llm-d/fleet-llm-d/pkg/apis/fleet/v1alpha1"
	"github.com/llm-d/fleet-llm-d/pkg/classifier"

	"github.com/llm-d/fleet-llm-d/pkg/autoscaling/actuator"
	"github.com/llm-d/fleet-llm-d/pkg/autoscaling/collector"
	"github.com/llm-d/fleet-llm-d/pkg/autoscaling/optimizer"
	"github.com/llm-d/fleet-llm-d/pkg/cluster/client"
	"github.com/llm-d/fleet-llm-d/pkg/controller"
	"github.com/llm-d/fleet-llm-d/pkg/cost"
	"github.com/llm-d/fleet-llm-d/pkg/intents"
	"github.com/llm-d/fleet-llm-d/pkg/kvcache/transfer"
	"github.com/llm-d/fleet-llm-d/pkg/ledger"
	"github.com/llm-d/fleet-llm-d/pkg/lifecycle/rollout"
	"github.com/llm-d/fleet-llm-d/pkg/modelplane"
	"github.com/llm-d/fleet-llm-d/pkg/observability/metrics"
	"github.com/llm-d/fleet-llm-d/pkg/placement/scorer"
	"github.com/llm-d/fleet-llm-d/pkg/placement/solver"
	"github.com/llm-d/fleet-llm-d/pkg/routing"
	"github.com/llm-d/fleet-llm-d/pkg/routing/balancer"
	"github.com/llm-d/fleet-llm-d/pkg/routing/policy"
	gridsignals "github.com/llm-d/fleet-llm-d/pkg/routing/signals"
	"github.com/llm-d/fleet-llm-d/pkg/serving"
	"github.com/llm-d/fleet-llm-d/pkg/store/events"
	"github.com/llm-d/fleet-llm-d/pkg/store/postgres"
	"github.com/llm-d/fleet-llm-d/pkg/tenant/metering"
	"github.com/llm-d/fleet-llm-d/pkg/tenant/quota"
	"github.com/llm-d/fleet-llm-d/pkg/tlsutil"
)

// FleetController is the top-level controller that coordinates all fleet
// management capabilities including placement, routing, autoscaling,
// tenant management, lifecycle, observability, KV cache, and cluster operations.
type FleetController struct {
	// Capability components
	Solver               solver.ConstraintSolver
	Scorer               *scorer.CompositeScorer
	Routing              *RoutingController
	LoadBalancer         balancer.LoadBalancer
	MetricsCollector     collector.MetricsCollector
	Optimizer            optimizer.FleetOptimizer
	QuotaEnforcer        quota.QuotaEnforcer
	UsageTracker         metering.UsageTracker
	RolloutController    rollout.RolloutController
	MetricsFederator     metrics.MetricsFederator
	TransferOrchestrator transfer.TransferOrchestrator
	ClusterClient        client.MultiClusterClient
	EventPublisher       events.EventPublisher

	// Reconciler watches fleet CRDs and reconciles desired state
	Reconciler *controller.Reconciler

	// CRDWatcher polls the K8s API for CRD changes (optional, only when kube-api is configured)
	CRDWatcher *controller.CRDWatcher

	// LeaderElector coordinates active/passive control-plane ownership in Kubernetes.
	LeaderElector *controller.LeaderElector

	// Ledger integration
	FleetRecorder      *ledger.FleetRecorder
	LedgerGatewayURL   string
	LedgerGatewayToken string

	// IntentService owns honest asynchronous intent/operation semantics.
	IntentService *intents.Service

	// DecisionPackageDecoder verifies producer-owned GCL CloudEvents before
	// they are projected into FleetIntent admission.
	DecisionPackageDecoder *intents.GCLDecisionPackageDecoder

	// AllowOperatorJSONIntents enables the unsigned, self-asserted JSON v2
	// compatibility input. It is development/operator tooling only and is
	// deliberately false unless explicitly enabled.
	AllowOperatorJSONIntents bool

	// Cost and pricing
	PricingTable *cost.PricingTable

	// Repositories for CRUD operations
	ClusterRepo postgres.ClusterRepository
	PoolRepo    postgres.FleetPoolRepository
	TenantRepo  postgres.TenantRepository
	RolloutRepo postgres.RolloutRepository

	// ModelPlane integration
	ModelPlaneWatcher *modelplane.ModelPlaneWatcher
	ModelPlaneBridge  *modelplane.ComplianceBridge

	// Autoscaling actuation
	Actuator *actuator.ModelPlaneActuator

	// Praxis overlay renders dynamic routing config from placement decisions.
	PraxisOverlay *routing.PraxisOverlay

	// GridCRDTranslator renders fleet cluster/pool state as Praxis Grid CRDs.
	GridCRDTranslator *routing.GridCRDTranslator

	// RoutingProvider is the single authoritative adapter that receives the
	// fleet-qualified provider set. GridCRDTranslator remains as a deprecated
	// compatibility handle for callers that inspect the Praxis adapter.
	RoutingProvider     routing.RoutingProvider
	RoutingProviderName routing.ProviderName

	// SWIMSyncAdapter reads GridSite health status back into FleetCluster records.
	SWIMSyncAdapter *client.SWIMSyncAdapter

	// Routing state — tracks which clusters were healthy to avoid redundant Praxis updates.
	lastRoutingFingerprint string
	KubeAPI                string

	// Cost configuration
	CostConfig CostConfig

	// Auth secret for token refresh
	AuthSecret string

	// Inference gateway configuration. PraxisURL is intentionally internal;
	// callers must enter through the authenticated fleet gateway.
	PraxisURL              string
	PraxisToken            string
	InferenceProviderName  InferenceProviderName
	LLMDCPUURL             string
	LLMDGPUURL             string
	LLMDToken              string
	RouterUpstreamClusters map[string]string
	ModelProviderClusters  map[string][]string
	InferenceClient        *http.Client
	CPUPhysicalModel       string
	GPUPhysicalModel       string
	InferenceSlots         chan struct{}
	ProviderHealth         *ProviderHealthCache
	GridSignalPoller       *gridsignals.Poller
	providerRouteCounter   atomic.Uint64

	// Server state
	ready atomic.Bool
}

// NewFleetController creates a new FleetController with all components
// initialized using their default constructors. The backendVLLM and backendOVMS
// parameters specify the base URLs for the default inference backends. The
// kubeAPI and namespace parameters are optional; when kubeAPI is non-empty a
// CRDWatcher polls FleetInferencePool resources and FleetIntent/FleetOperation
// CRDs become the authoritative intent repository.
func NewFleetController(ledgerEndpoint, backendVLLM, backendOVMS, kubeAPI, namespace string) (*FleetController, error) {
	return NewFleetControllerWithLedgerConfig(ledger.Config{Mode: ledger.ModeMemory, Endpoint: ledgerEndpoint}, backendVLLM, backendOVMS, kubeAPI, namespace)
}

// NewFleetControllerWithLedgerConfig creates a FleetController with an
// explicit ledger backend configuration.
func NewFleetControllerWithLedgerConfig(ledgerCfg ledger.Config, backendVLLM, backendOVMS, kubeAPI, namespace string) (*FleetController, error) {
	ledgerClient, err := ledger.NewLedgerClientWithConfig(ledgerCfg)
	if err != nil {
		return nil, fmt.Errorf("initialize immutable-ledger client in %q mode: %w", ledgerCfg.Mode, err)
	}

	clusterRepo := postgres.NewInMemoryClusterRepository()
	tenantRepo := postgres.NewInMemoryTenantRepository()
	rolloutRepo := postgres.NewInMemoryRolloutRepository()
	poolRepo := postgres.NewInMemoryFleetPoolRepository()
	clusterClient := client.NewRepositoryClusterClient(clusterRepo)
	fleetRecorder := ledger.NewFleetRecorder(ledgerClient, "fleet-controller", "fleet-llm-d")
	constraintSolver := solver.NewConstraintSolver()
	metricsCollector := collector.NewMetricsCollector()

	// Create reconciler wired to the cluster client and constraint solver.
	reconciler := controller.NewReconciler(constraintSolver, clusterClient.ListClusters)
	if namespace != "" {
		reconciler.SetNamespace(namespace)
	}

	// Build Praxis overlay from environment config (before onChange which captures it).
	var praxisOverlay *routing.PraxisOverlay
	if praxisEndpoints := os.Getenv("PRAXIS_CLUSTER_ENDPOINTS"); praxisEndpoints != "" {
		var endpoints []routing.PraxisClusterEndpoint
		for _, entry := range strings.Split(praxisEndpoints, ",") {
			parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(parts) == 2 {
				endpoint := parts[1]
				tlsEnabled := strings.HasPrefix(endpoint, "https://")
				endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
				endpoints = append(endpoints, routing.PraxisClusterEndpoint{
					ClusterID: parts[0],
					Endpoint:  endpoint,
					TLS:       tlsEnabled,
				})
			}
		}
		if len(endpoints) > 0 {
			praxisOverlay = routing.NewPraxisOverlay(endpoints)
			slog.Info("Praxis overlay enabled", "endpoints", len(endpoints))
		}
	}

	// Create the event publisher before assembling the controller.
	eventPublisher := events.NewLedgerAwarePublisher(events.NewEventPublisher(), fleetRecorder)

	// Create CRDWatcher and autoscaling actuator if Kubernetes API is configured.
	var crdWatcher *controller.CRDWatcher
	var autoscalingActuator *actuator.ModelPlaneActuator
	var gridTranslator *routing.GridCRDTranslator
	providerName, err := routing.ParseProviderName(os.Getenv("FLEET_ROUTING_PROVIDER"))
	if err != nil {
		return nil, err
	}
	var routingProvider routing.RoutingProvider
	var swimSync *client.SWIMSyncAdapter
	var intentRepository intents.Repository = intents.NewMemoryRepository()
	if kubeAPI != "" {
		if namespace == "" {
			namespace = "default"
		}
		token := readServiceAccountToken()
		crdWatcher = controller.NewCRDWatcher(kubeAPI, namespace, token, reconciler)
		intentRepository = intents.NewKubernetesRepository(kubeAPI, namespace, token, nil)
		autoscalingActuator = actuator.NewModelPlaneActuator(kubeAPI, token)

		if gridNetwork := os.Getenv("GRID_NETWORK"); gridNetwork != "" && providerName == routing.ProviderPraxis {
			gridTranslator = routing.NewGridCRDTranslator(kubeAPI, namespace, token, gridNetwork)
			routingProvider = gridTranslator
			swimSync = client.NewSWIMSyncAdapter(kubeAPI, token, clusterRepo)
			slog.Info("Grid CRD translator enabled", "network", gridNetwork)
		}
		if providerName == routing.ProviderLLMD {
			directory := os.Getenv("LLMD_ROUTER_ENDPOINTS_DIR")
			if directory == "" {
				directory = "/var/lib/fleet-llm-d/router-endpoints"
			}
			var publisher routing.LLMDFilePublisher
			if configMap := os.Getenv("LLMD_ROUTER_ENDPOINTS_CONFIGMAP"); configMap != "" {
				tlsConfig, tlsErr := tlsutil.NewTLSConfig(tlsutil.KubernetesTLSOptions())
				if tlsErr != nil {
					return nil, fmt.Errorf("initialize Kubernetes trust for llm-d Router adapter: %w", tlsErr)
				}
				publisher, err = routing.NewKubernetesConfigMapPublisherFromTokenFile(kubeAPI, namespace, configMap, tlsutil.ServiceAccountTokenPath, &http.Client{
					Timeout:   10 * time.Second,
					Transport: &http.Transport{TLSClientConfig: tlsConfig},
				})
				if err != nil {
					return nil, fmt.Errorf("initialize llm-d Router ConfigMap publisher: %w", err)
				}
				slog.Info("llm-d Router ConfigMap publishing enabled", "configmap", configMap)
			}
			routingProvider, err = routing.NewLLMDProvider(routing.LLMDProviderOptions{
				Directory: directory, Publisher: publisher, Namespace: namespace, RequireTLS: os.Getenv("LLMD_ROUTER_ALLOW_INSECURE") != "true",
			})
			if err != nil {
				return nil, fmt.Errorf("initialize llm-d Router adapter: %w", err)
			}
			slog.Info("llm-d Router adapter enabled", "directory", directory)
		}
	}

	// Classification carries prompt text, so the channel is TLS by default.
	// SEMANTIC_CLASSIFIER_INSECURE opts into plaintext for local development.
	classifierEndpoint := os.Getenv("SEMANTIC_CLASSIFIER_URL")
	classifierInsecure := os.Getenv("SEMANTIC_CLASSIFIER_INSECURE") == "true"

	var classifierClient classifier.ClassifierClient
	if classifierInsecure {
		classifierClient, err = classifier.NewInsecureClassifierClient(classifierEndpoint)
	} else {
		classifierClient, err = classifier.NewClassifierClient(classifierEndpoint, os.Getenv("SEMANTIC_CLASSIFIER_CA"))
	}
	if err != nil {
		return nil, fmt.Errorf("initialize semantic classifier client: %w", err)
	}
	if classifierEndpoint != "" {
		slog.Info("semantic classifier enabled", "endpoint", classifierEndpoint, "tls", !classifierInsecure)
		if classifierInsecure {
			slog.Warn("semantic classifier connection is PLAINTEXT: prompt text crosses the network unencrypted. " +
				"Unset SEMANTIC_CLASSIFIER_INSECURE outside local development.")
		}
	}

	fc := &FleetController{
		Solver: constraintSolver,
		Scorer: scorer.NewCompositeScorer([]scorer.WeightedScorer{
			{Scorer: scorer.NewCostScorer(), Weight: 0.3},
			{Scorer: scorer.NewCapacityScorer(), Weight: 0.3},
			{Scorer: scorer.NewLocalityScorer(), Weight: 0.2},
			{Scorer: scorer.NewKVCacheAffinityScorer(), Weight: 0.2},
		}),
		Routing: &RoutingController{
			Evaluator:        policy.NewRoutingPolicyEvaluator(),
			SessionTable:     routing.NewSessionAffinityTable(30 * time.Minute),
			ClassifierClient: classifierClient,
			ClassifierCache:  classifier.NewClassificationCache(30 * time.Minute),
		},
		LoadBalancer:         balancer.NewWeightedBalancer(),
		MetricsCollector:     metricsCollector,
		Optimizer:            optimizer.NewFleetOptimizer(),
		QuotaEnforcer:        quota.NewQuotaEnforcer(tenantRepo),
		UsageTracker:         metering.NewUsageTracker(tenantRepo),
		RolloutController:    rollout.NewRolloutController(rolloutRepo),
		MetricsFederator:     metrics.NewMetricsFederator(metricsCollector),
		TransferOrchestrator: transfer.NewTransferOrchestrator(),
		ClusterClient:        clusterClient,
		EventPublisher:       eventPublisher,
		PricingTable:         cost.DefaultPricingTable(),
		CostConfig:           DefaultCostConfig(),
		Reconciler:           reconciler,
		CRDWatcher:           crdWatcher,
		Actuator:             autoscalingActuator,
		FleetRecorder:        fleetRecorder,
		LedgerGatewayURL: func() string {
			if ledgerCfg.Mode == ledger.ModeHTTP {
				return strings.TrimRight(ledgerCfg.Endpoint, "/")
			}
			return ""
		}(),
		LedgerGatewayToken:  ledgerCfg.APIToken,
		IntentService:       intents.NewService(intentRepository, intents.DefaultPolicyConfig(), ledgerClient),
		PraxisOverlay:       praxisOverlay,
		GridCRDTranslator:   gridTranslator,
		RoutingProvider:     routingProvider,
		RoutingProviderName: providerName,
		SWIMSyncAdapter:     swimSync,
		ClusterRepo:         clusterRepo,
		PoolRepo:            poolRepo,
		TenantRepo:          tenantRepo,
		RolloutRepo:         rolloutRepo,
		KubeAPI:             kubeAPI,
	}
	// The callback deliberately dereferences repositories through fc. This is
	// important because production persistence is wired after construction.
	fc.configureReconcilerOnChange(kubeAPI, namespace)
	fc.configureServingActuator(namespace)
	if err := fc.configureGridSignals(); err != nil {
		return nil, err
	}
	return fc, nil
}

func (fc *FleetController) configureGridSignals() error {
	raw := strings.TrimSpace(os.Getenv("GRID_SIGNALS_PEERS_JSON"))
	if raw == "" {
		return nil
	}
	var endpoints map[string]string
	if err := json.Unmarshal([]byte(raw), &endpoints); err != nil {
		return fmt.Errorf("parse GRID_SIGNALS_PEERS_JSON: %w", err)
	}
	certPath, keyPath := os.Getenv("GRID_SIGNALS_CLIENT_CERT"), os.Getenv("GRID_SIGNALS_CLIENT_KEY")
	if certPath == "" || keyPath == "" {
		return fmt.Errorf("grid signals require client certificate and key")
	}
	tlsConfig, err := tlsutil.NewTLSConfig(tlsutil.TLSOptions{CAPath: os.Getenv("GRID_SIGNALS_CA")})
	if err != nil {
		return fmt.Errorf("grid signals TLS trust: %w", err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("grid signals client identity: %w", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	peers := make([]gridsignals.Peer, 0, len(endpoints))
	for site, endpoint := range endpoints {
		peers = append(peers, gridsignals.Peer{Site: site, Endpoint: endpoint})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Site < peers[j].Site })
	fc.GridSignalPoller = &gridsignals.Poller{
		Client: gridsignals.NewClient(tlsConfig, 5*time.Second, 30*time.Second),
		Peers:  peers,
	}
	return nil
}

func (fc *FleetController) configureServingActuator(namespace string) {
	renderer := serving.KServeRenderer{Namespace: namespace}
	fc.Reconciler.SetServingActuator(func(ctx context.Context, pool v1alpha1.FleetInferencePoolSpec, desired []string) error {
		if pool.Serving.EffectiveTarget() != v1alpha1.ServingTargetKServeLLMInferenceService {
			return nil
		}
		applier, ok := fc.ClusterClient.(client.ResourceApplier)
		if !ok {
			return fmt.Errorf("KServe serving target requires a cluster client with resource apply capability")
		}
		resource, err := renderer.Render(pool.Model.Name, pool)
		if err != nil {
			return err
		}
		for _, clusterID := range desired {
			if err := applier.ApplyResource(ctx, clusterID, resource); err != nil {
				return fmt.Errorf("apply KServe LLMInferenceService to %s: %w", clusterID, err)
			}
		}
		return nil
	})
	fc.Reconciler.SetActualClusterObserver(func(ctx context.Context, pool v1alpha1.FleetInferencePoolSpec, desired []string) ([]string, error) {
		if pool.Serving.EffectiveTarget() != v1alpha1.ServingTargetKServeLLMInferenceService {
			return nil, nil
		}
		reader, ok := fc.ClusterClient.(client.ResourceReader)
		if !ok {
			return nil, fmt.Errorf("KServe serving target requires a cluster client with resource status capability")
		}
		return observeKServeClusters(ctx, reader, renderer, pool.Model.Name, desired)
	})
}

func observeKServeClusters(ctx context.Context, reader client.ResourceReader, renderer serving.KServeRenderer, model string, desired []string) ([]string, error) {
	actual := make([]string, 0, len(desired))
	for _, clusterID := range desired {
		raw, err := reader.GetResource(ctx, clusterID, renderer.ResourcePath(model))
		if err != nil {
			return actual, fmt.Errorf("read KServe LLMInferenceService from %s: %w", clusterID, err)
		}
		resource, err := serving.ParseKServeResource(raw)
		if err != nil {
			return actual, fmt.Errorf("read KServe LLMInferenceService from %s: %w", clusterID, err)
		}
		if resource.ObservedReady() {
			actual = append(actual, clusterID)
		}
	}
	return actual, nil
}

// configureReconcilerOnChange wires placement side effects through the
// controller's current dependencies. Never capture constructor-local
// repositories here: OverrideWithPostgres replaces them during startup.
func (fc *FleetController) configureReconcilerOnChange(kubeAPI, namespace string) {
	fc.Reconciler.SetOnChange(func(pool *controller.FleetPoolState) {
		ctx := context.Background()
		for _, clusterID := range pool.DesiredClusters {
			if _, err := fc.FleetRecorder.RecordPlacement(
				ctx, pool.Model, clusterID, 1, "", "reconciler placement",
			); err != nil {
				slog.Warn("failed to record placement", "model", pool.Model, "cluster", clusterID, "error", err)
			}
		}

		poolRecord := postgres.FleetPoolRecord{
			ID:              pool.Name,
			Name:            pool.Name,
			ModelName:       pool.Model,
			ModelSource:     pool.Source,
			DesiredClusters: append([]string(nil), pool.DesiredClusters...),
			TargetPorts:     append([]int(nil), pool.TargetPorts...),
			Status:          string(pool.Phase),
			UpdatedAt:       pool.LastReconciled,
		}
		if fc.PraxisOverlay != nil {
			allPools := fc.Reconciler.ListPools()
			var placements []routing.PoolPlacement
			for _, p := range allPools {
				if len(p.DesiredClusters) > 0 {
					placements = append(placements, routing.PoolPlacement{
						ModelName: p.Model,
						Clusters:  p.DesiredClusters,
					})
				}
			}
			if cfg, err := fc.PraxisOverlay.RenderConfig(placements); err == nil {
				if writeErr := writePraxisConfigMap(kubeAPI, namespace, cfg); writeErr != nil {
					slog.Warn("failed to update Praxis config", "error", writeErr)
				} else {
					slog.Info("Praxis config updated", "placements", len(placements))
					_ = fc.EventPublisher.Publish(ctx, events.FleetEvent{
						Type: events.EventRoutingUpdated, Source: "urn:fleet-llm-d:controller",
						Subject: pool.Model, Timestamp: time.Now().UTC(),
						Payload: map[string]interface{}{"placements": len(placements)},
					})
				}
			}
		}

		if _, err := fc.PoolRepo.Get(ctx, pool.Name); err != nil {
			poolRecord.CreatedAt = pool.LastReconciled
			if createErr := fc.PoolRepo.Create(ctx, poolRecord); createErr != nil {
				slog.Warn("failed to sync pool to repo", "pool", pool.Name, "error", createErr)
			}
		} else if updateErr := fc.PoolRepo.Update(ctx, poolRecord); updateErr != nil {
			slog.Warn("failed to update pool in repo", "pool", pool.Name, "error", updateErr)
		}
	})
}

// BuildClusterHealth assembles routing-ready ClusterHealth entries by combining
// cluster records from the repository with live metrics from the collector.
func (fc *FleetController) BuildClusterHealth(ctx context.Context) []policy.ClusterHealth {
	clusters, err := fc.ClusterRepo.List(ctx)
	if err != nil {
		return nil
	}
	allMetrics, _ := fc.MetricsCollector.CollectAll(ctx)
	metricsMap := make(map[string]collector.PoolMetrics)
	for _, cm := range allMetrics {
		for _, poolMetrics := range cm.Pools {
			aggregate, exists := metricsMap[cm.ClusterID]
			if !exists {
				metricsMap[cm.ClusterID] = poolMetrics
				continue
			}
			// Routing without a model-specific metric stream uses conservative
			// cluster-wide values rather than whichever pool happened to be first.
			aggregate.TTFT_P99_Ms = max(aggregate.TTFT_P99_Ms, poolMetrics.TTFT_P99_Ms)
			aggregate.GPUUtilization = max(aggregate.GPUUtilization, poolMetrics.GPUUtilization)
			aggregate.PoolSaturation = max(aggregate.PoolSaturation, poolMetrics.PoolSaturation)
			aggregate.ReadyEndpoints += poolMetrics.ReadyEndpoints
			if aggregate.KVCacheHitRate == 0 || (poolMetrics.KVCacheHitRate > 0 && poolMetrics.KVCacheHitRate < aggregate.KVCacheHitRate) {
				aggregate.KVCacheHitRate = poolMetrics.KVCacheHitRate
			}
			metricsMap[cm.ClusterID] = aggregate
		}
	}

	var result []policy.ClusterHealth
	for _, c := range clusters {
		capacity := float64(c.GPUAvailable) / float64(max(c.GPUTotal, 1))
		if c.GPUTotal == 0 {
			capacity = 1.0
		}
		ch := policy.ClusterHealth{
			ClusterID:         c.ID,
			Healthy:           c.Status == "Running" || c.Status == "Healthy",
			AvailableSlots:    c.GPUAvailable,
			CapacityRemaining: capacity,
			Region:            c.Region,
			Labels:            c.Labels,
		}
		if pm, ok := metricsMap[c.ID]; ok {
			ch.KVCacheHitRate = pm.KVCacheHitRate
			ch.LatencyMs = pm.TTFT_P99_Ms
			ch.CurrentLoad = pm.GPUUtilization
			ch.PoolSaturation = pm.PoolSaturation

			// EPP signals override legacy equivalents when available.
			if pm.PoolSaturation > 0 {
				ch.CurrentLoad = pm.PoolSaturation
			}
			if pm.ReadyEndpoints > 0 {
				ch.AvailableSlots = pm.ReadyEndpoints
			}
		}
		result = append(result, ch)
	}
	return result
}

// writePraxisConfigMap updates the praxis-ai-config ConfigMap with the
// rendered Praxis config. Uses the in-cluster K8s API.
func writePraxisConfigMap(kubeAPI, namespace, configYAML string) error {
	if kubeAPI == "" {
		return nil
	}
	token := readServiceAccountToken()

	payload := fmt.Sprintf(`{"data":{"praxis-ai-config.yaml":%q}}`, configYAML)
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/configmaps/praxis-ai-config",
		strings.TrimRight(kubeAPI, "/"), namespace)

	req, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/merge-patch+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	tlsConfig, err := tlsutil.NewTLSConfig(tlsutil.KubernetesTLSOptions())
	if err != nil {
		return fmt.Errorf("load Kubernetes CA: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("patch configmap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("patch configmap: %d", resp.StatusCode)
	}
	return nil
}
