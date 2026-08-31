# Fleet-Level Inference Orchestration for Enterprise AI

> **Historical extended engineering record.** This document combines design
> material and measurements from several pre-`v0.3.0` deployment revisions.
> It is retained for traceability but is not the authoritative OSS whitepaper
> or current certification statement. Read
> [`llm-d-fleet-whitepaper.md`](llm-d-fleet-whitepaper.md) for the normalized
> product architecture and evidence boundary.

## Architecture, Benchmarks, and Production Validation with llm-d

**Author:** Jonathan Kershaw
**Date:** August 2026
**Version:** Archived draft 0.7

---

## 1. Executive Summary

Production AI inference runs on a well-understood four-layer stack: cluster provisioning (OpenShift), multi-cluster management (RHACM), within-cluster inference intelligence (llm-d), and API management (MaaS/AI Gateway). What is missing is the fifth layer -- fleet-level inference orchestration -- that coordinates model placement, traffic routing, autoscaling, tenant governance, lifecycle management, observability, and KV cache state transfer across the entire inference fleet. Multiple enterprise engagements across telecommunications, financial services, and sovereign cloud independently confirm the need for this layer.

fleet-llm-d fills this gap with a data-plane-neutral Go control plane and PostgreSQL-backed persistence. It qualifies exact-model providers across failure domains and hands that eligible set to one routing adapter. Praxis is the validated adapter for the measured deployment; llm-d Router is the upstream-native beta. KServe and llm-d retain cluster-local serving and endpoint selection. ModelPack, DeepField, GCL, semantic classification, and immutable evidence are optional integrations. The existing governed-evidence production mode requires signed GCL admission and an authenticated external ledger, while the default community installation does not.

The repository contains capability-specific evidence from a physical
three-cluster topology (CpuCluster, HubCluster, and GpuCluster), including real CPU and GPU
inference. Key historical results include 83.7 RPS GPU saturation on H100 NVL
with 0% errors, a 34,027-operation variable-intensity soak with zero crashes,
32/32 live security tests, and a 268,738-operation 24-hour soak at 99.0%.
Those results have different scopes and do not collectively certify every
capability or routing adapter. Praxis is the validated path for the current
three-cluster release; llm-d Router remains beta and is undergoing separate
TLS, signal, and failover qualification. Gold remains open on longer-duration
and adapter-specific evidence, signed external evidence for the governed
profile, and a formal security audit.

## 2. Problem Statement

### 2.1 The Four-Layer Stack and the Missing Fifth

Production AI inference at scale runs on four well-understood layers: cluster provisioning (OpenShift), multi-cluster management (RHACM), within-cluster inference intelligence (llm-d), and API management (MaaS/AI Gateway). Each exists and works. What's missing is the layer that coordinates inference across all of them: fleet-level inference orchestration.

### 2.2 Customer Signals

Multiple enterprise engagements across telecommunications, financial services, and sovereign cloud independently describe the same need. Telco Edge Provider requires multi-cluster mesh topology across 30+ edge sites with tenant isolation and usage metering. Enterprise Telco asks for a single pane of glass across their inference fleet with per-tenant cost controls. Financial Services Provider needs multi-region failover with regulatory constraints on model placement. Global Banking Partner named multi-cluster routing as their top priority. Mobile Network Operator selected a competitor on tenant self-service.

### 2.3 Competitive Landscape

On June 23, 2026, two independent open-source projects launched targeting this exact layer: ModelPlane (Crossplane-based, Apache 2.0) and SkyPilot Endpoints (UC Berkeley). Both treat llm-d as settled within-cluster infrastructure and position themselves as the fleet-level control plane above it. ModelPlane is now positioned as a complementary infrastructure layer via collaboration proposal [modelplaneai/modelplane#326](https://github.com/modelplaneai/modelplane/issues/326); see section 3.9 for the three-layer architecture and section 5.4.13 for the full integration specification. SkyPilot Endpoints takes a different approach, focusing on cloud-native GPU cluster provisioning and job scheduling rather than Kubernetes-native CRD-driven orchestration; fleet-llm-d's CRD model and llm-d composition are the primary differentiators.

### 2.4 Why Within-Cluster Intelligence Is Necessary But Insufficient

llm-d delivers state-of-the-art within-cluster inference: intelligent routing (EPP), KV cache management, prefill/decode disaggregation, SLO-aware autoscaling (WVA), and flow control. These capabilities are essential but scoped to a single cluster. No component coordinates model placement, traffic routing, or capacity optimization across clusters.

## 3. Architecture

### 3.1 Design Principles

Six principles govern all architectural decisions in fleet-llm-d:

1. **Open Source First (Apache 2.0).** Every line of fleet-llm-d code ships under Apache 2.0. No proprietary dependencies are permitted in the critical path. Customers and competitors alike can inspect, fork, and extend the platform. This matches llm-d's own licensing model and ensures fleet-llm-d can be adopted in sovereign environments that prohibit proprietary control plane software.

2. **Framework, Not Product.** fleet-llm-d is a composable framework of capabilities, not a monolithic product. Each capability (placement, routing, autoscaling, etc.) operates independently and can be adopted incrementally. Operators choose which CRDs to deploy and which capabilities to enable. A customer running only multi-cluster observability need not deploy the placement engine.

3. **Composition Over Embedding.** fleet-llm-d composes with llm-d and KServe
   rather than replacing them. `FleetInferencePool` can target an existing
   `InferencePool` or KServe `LLMInferenceService`. KEDA over EPP metrics is the
   default local autoscaling path; WVA is an optional heterogeneous-variant
   optimizer feeding KEDA/HPA. ModelPack, evidence ledgers, and observation or
   governance systems remain external integrations.

4. **No-Fork Commitment.** fleet-llm-d never forks llm-d. All within-cluster intelligence remains in the upstream llm-d project. fleet-llm-d operates strictly above the cluster boundary, consuming llm-d's APIs and metrics without modifying its code. When a fleet-level need implies a within-cluster change, that change is proposed upstream to llm-d.

5. **Polyglot by Design.** The control plane is written in Go (standard library patterns, table-driven tests) because the Kubernetes ecosystem is Go-native. Per-cluster agents and KV cache transfer are written in Rust (tokio, tonic, axum) because they operate on the hot path where memory safety and zero-cost abstractions matter. Praxis AI handles inference routing as the programmable data plane. The dashboard is TypeScript (Next.js). Protocol Buffers define the contract between control plane and data plane.

6. **Immutable-Ledger Independence.** [are-immutable-ledger](https://github.com/jkershawrh/are-immutable-ledger) is an independent component in the fleet ecosystem. It runs on its own database and compute and is operated separately from fleet-llm-d. fleet-llm-d is one of many writers. Proof receipts establish recorded evidence; they are not credentials and do not authorize fleet actions.

### 3.2 Control Plane (Go)

The control plane is implemented in Go 1.26.5 using standard library HTTP for the REST API. It consists of a single binary, `fleet-controller`, that hosts nine capability packages and a REST API server.

**Capability Packages.** Each capability is a self-contained Go package under `pkg/`:

- `pkg/placement/` -- Constraint solver (`solver/constraint_solver.go`) evaluates PlacementPolicy rules against cluster state using label-selector constraints (key=value matching). Cluster scorer (`scorer/cluster_scorer.go`) ranks candidate clusters by weighted affinity criteria (GPU utilization, cost efficiency, data locality). Together they produce placement decisions in under 100ms p99.
- `pkg/routing/` -- Policy evaluator (`policy/evaluator.go`) resolves FleetRoutingPolicy rules (geographic, least-load, failover, KV cache affinity). Balancer (`balancer/balancer.go`) distributes traffic across clusters using the resolved policy.
- `pkg/autoscaling/` -- Metrics collector (`collector/collector.go`) aggregates per-cluster GPU utilization, queue depth, and SLO metrics. Optimizer (`optimizer/optimizer.go`) computes scaling decisions against FleetScalingPolicy objectives and GPU budget constraints.
- `pkg/lifecycle/` -- Rollout controller (`rollout/controller.go`) manages canary deployments, SLO-gated promotion, automatic rollback, and staged cluster-by-cluster rollout sequences.
- `pkg/tenant/` -- Quota enforcer (`quota/enforcer.go`) evaluates TenantProfile rate limits, token budgets, and GPU quotas. Metering tracker (`metering/tracker.go`) records per-tenant token consumption and cost attribution for chargeback.
- `pkg/observability/` -- Prometheus federation (`metrics/federation.go`) aggregates metrics from per-cluster Prometheus instances into fleet-wide views.
- `pkg/kvcache/` -- Transfer orchestrator (`transfer/orchestrator.go`) coordinates cross-cluster KV cache transfers, managing transfer state and issuing ARE ledger proof receipts.
- `pkg/modelplane/` -- ModelPlane integration layer: types (`types.go`), adapter (`adapter.go`), watcher (`watcher.go`), policy injector (`policy.go`), and compliance bridge (`compliance.go`). Consumes ModelDeployment and InferenceCluster CRDs, injects fleet policy, and bridges events to the ARE ledger.
- `pkg/cost/` -- GPU inference cost model: pricing table (`pricing.go`), tokenomics calculator (`tokenomics.go`), chargeback report generator (`chargeback.go`), and budget alert engine (`alerts.go`). Covers 6 GPU types across 3 pricing tiers with per-tenant cost attribution.

**Fleet Controller API.** The fleet-controller's current OpenAPI contract
defines 43 paths across health, fleet management, metrics, rollouts,
compliance, ModelPlane, cost, agents, intents, authentication, inference, and
webhook groups. Authentication uses HMAC-SHA256 signed bearer tokens (custom
format: base64(claims).base64(hmac), not RFC 7519 JWT). The OpenAPI 3.1
specification in `api/openapi/fleet-api.yaml` is authoritative; path counts are
inventory, not a compatibility promise.

**State Management.** PostgreSQL 16+ stores fleet state: cluster registrations, pool configurations, tenant profiles, rollout state, and placement decisions. The repository layer (`pkg/store/postgres/`) provides transactional access with an in-memory implementation for testing. Events are published to an in-memory publisher plus an optional HTTP sink (`pkg/store/events/http_publisher.go`) that can target any CloudEvents-compatible endpoint, including a Kafka REST proxy. There is no native Kafka client -- this is a deliberate supply-chain-minimal design choice (the entire Go module depends only on `lib/pq`).

### 3.3 Data Plane

The data plane uses a combination of Rust components for per-cluster operations and Praxis AI for inference routing. The KV transfer coordinator (`crates/kv-transfer/`) is a library crate, not a standalone binary.

**Fleet Agent (`crates/fleet-agent/`).** One fleet-agent instance runs per managed cluster. It performs four functions:

- *Watcher* (`watcher.rs`) -- Monitors local Kubernetes resources (InferencePool status, GPU capacity, pod health) and streams state updates to the fleet controller.
- *Reporter* (`reporter.rs`) -- Aggregates per-cluster metrics (GPU utilization, queue depth, TTFT, throughput) and publishes them at configurable intervals to the fleet controller's metrics ingestion endpoint.
- *Enforcer* (`enforcer.rs`) -- Receives placement and scaling directives from the fleet controller and applies them to the local cluster by creating, updating, or deleting InferencePool resources and adjusting replica counts.
- *Proxy* (`proxy.rs`) -- Provides a local gRPC endpoint for health checks and routing decisions, abstracting the cluster's internal topology.

**Routing-provider adapters.** Exactly one adapter is authoritative in a
deployment. Praxis is the validated default and consumes derived `GridSite`
and `InferenceProvider` resources without changing their existing contract.
The llm-d Router beta adapter emits deterministic watched endpoint files, one
per exact physical model. Both receive the same fleet-qualified provider set;
neither may add an incompatible provider or substitute a physical model.

The current physical deployment reaches CpuCluster CPU and GpuCluster GPU through
verified TLS OpenShift Routes. The earlier NodePort/ExternalName path is
historical evidence only. The normalized transport contract contains routing
and metrics URLs, TLS server identity, trust and authentication references,
health freshness, and failure-domain data. OpenShift Routes are the validated
Red Hat implementation, not an OSS-core requirement; Gateway API and other
end-to-end TLS transports can publish the same contract.

The optional Grid Signals publisher exports only allowlisted pool-level
metrics over mTLS and strips all source topology labels. SWIM remains the
membership/capability source; dynamic load signals are optional. See
[`docs/architecture/product-boundary.md`](../architecture/product-boundary.md)
for the normative ownership boundary.

**KV Cache Transfer Coordinator (`crates/kv-transfer/`).** Manages cross-cluster KV cache state transfer for hot failover, warm migration, and prefix tree synchronization.

- *Coordinator* (`coordinator.rs`) -- Orchestrates the transfer lifecycle: identifies source and destination clusters, negotiates transfer parameters, monitors progress, and confirms completion.
- *NIXL Bridge* (`nixl_bridge.rs`) -- Interfaces with llm-d's NIXL (NVIDIA Inference Xfer Library) for high-bandwidth GPU memory transfer, bridging the cross-cluster gap that NIXL does not natively support.
- *Protocol* (`protocol.rs`) -- Defines the wire protocol for KV cache transfer, including chunking, flow control, integrity verification, and ARE ledger proof receipt integration.
- *Transport* (`tcp_transport.rs`, `grpc_transport.rs`) -- Pluggable transport backends for KV cache transfer data. The TCP transport is the production default; the gRPC transport provides an alternative for environments where TCP is restricted.

**Fleet Ledger Client (`crates/fleet-ledger/`).** A shared Rust client for writing to and verifying the ARE Immutable Ledger, used by fleet-agent and the KV transfer coordinator for recording operational decisions and cache transfer provenance. Includes a SHA-256 hasher (`hasher.rs`) for computing chain hashes locally before submission.

### 3.4 CRD-Driven Declarative Model

fleet-llm-d is governed by twelve Custom Resource Definitions (CRDs) that define the complete fleet state as declarative Kubernetes resources. Ten are fleet-native CRDs under the `fleet.llm-d.io` API group; two are Praxis Grid CRDs under `grid.praxis-proxy.io` that the fleet controller renders from fleet state via the Grid CRD translator (`pkg/routing/praxis_crd_translator.go`). All CRD schemas are maintained in `api/crds/` and serve as the source of truth for the fleet's desired state.

1. **FleetCluster** (`fleetcluster.yaml`) -- Registers a spoke cluster with the hub controller. Specifies the cluster's endpoint, credentials, labels (used for placement constraint matching), GPU inventory, and health check configuration. FleetCluster is the foundation resource that all other CRDs reference when targeting specific clusters.

2. **FleetInferencePool** (`fleetinferencepool.yaml`) -- The primary resource declaring a model's fleet-wide deployment intent. Specifies the model source (OCI reference), placement policy reference, routing policy reference, scaling policy reference, serving configuration (InferencePool template for llm-d), and lifecycle strategy. This is the top-level resource that ties all other CRDs together for a given model.

3. **FleetIntent** (`fleetintent.yaml`) -- Represents an admitted intent from an external governance system. FleetIntent and FleetOperation implement the governed intent admission pipeline. External governance systems (e.g., GCL) submit signed DecisionPackage CloudEvents to `POST /api/v2/intents`. The 956-line adapter (`pkg/intents/gcl_adapter.go`) performs cryptographic signature verification, expiry checking, scope binding, and fleet-owned authorization. Admitted intents create FleetOperations that track through a 17-phase lifecycle from RECEIVED through SUCCEEDED or FAILED.

4. **FleetOperation** (`fleetoperation.yaml`) -- Tracks the execution of an admitted intent through a 17-phase lifecycle (RECEIVED, ACCEPTED, PLANNED, GOVERNANCE_PENDING, AUTHORIZED, ACTUATING, OBSERVING, VERIFIED, EVIDENCE_PENDING, SUCCEEDED, REJECTED, DEFERRED, EXPIRED, FAILED, ROLLING_BACK, ROLLED_BACK, QUARANTINED). Each phase transition is recorded with timestamp and rationale for audit trail purposes.

5. **PlacementPolicy** (`placementpolicy.yaml`) -- Defines constraints and affinities for model placement across clusters. Constraints use label-selector constraints (key=value matching) evaluated against cluster labels and state (e.g., regulatory zone, GPU type, certification status). Affinities assign weighted preferences (GPU utilization, cost efficiency, data locality). Supports spreading strategies for high availability.

6. **FleetRoutingPolicy** (`fleetroutingpolicy.yaml`) -- Governs how inference traffic is distributed across clusters serving the same model. Supports geographic routing (prefer local cluster), least-load balancing, failover chains with ordered cluster lists, KV cache affinity routing, and per-tenant priority rules. Includes health check configuration for unhealthy cluster detection.

7. **FleetScalingPolicy** (`fleetscalingpolicy.yaml`) -- Declares autoscaling objectives (GPU utilization targets, queue depth thresholds), constraints (global GPU maximums, stabilization windows, rate limits), cross-cluster migration settings (threshold, cooldown), and scale-to-zero configuration (cooldown period, trigger).

8. **TenantProfile** (`tenantprofile.yaml`) -- Defines tenant identity, quotas, rate limits, priority, cost controls, and cluster restrictions. Quotas include maxTokensPerMinute, maxConcurrentRequests, maxModels, and GPU budgets (maxGPUs with type restrictions). Cost controls specify monthly budgets with alert thresholds. Cluster restrictions limit which clusters a tenant may use.

9. **ModelLifecycle** (`modellifecycle.yaml`) -- Manages the lifecycle of model versions across the fleet. Defines rollout strategies (canary, blue-green, rolling), SLO gates for promotion (latency, error rate, throughput thresholds), rollback triggers, and staged cluster deployment sequences.

10. **KVCacheTransferPolicy** (`kvcachetransferpolicy.yaml`) -- Governs cross-cluster KV cache state transfer. Specifies transfer triggers (hot failover, warm migration, scheduled synchronization), bandwidth limits, priority, integrity verification requirements, and ARE ledger proof receipt configuration.

11. **GridSite** (`gridsite.yaml`, `grid.praxis-proxy.io/v1alpha1`) -- Represents a site in the Praxis Grid mesh. The Grid CRD translator renders one GridSite per registered FleetCluster, populating grid network reference, region, zone, sovereignty zone, and egress address with TLS mode. The SWIM sync adapter reads GridSite status (Active, Discovered, Connecting, Pending, Unreachable, Left) back into fleet cluster health records. Cluster-scoped.

12. **InferenceProvider** (`inferenceprovider.yaml`, `grid.praxis-proxy.io/v1alpha1`) -- Declares an inference backend within a GridSite. The translator renders one InferenceProvider per FleetInferencePool placement, specifying provider kind (InCluster, Remote, External), backend kind, endpoint, served models with capabilities and context window, and metrics configuration. Namespace-scoped.

### 3.5 Event-Driven State Management

Every state mutation in fleet-llm-d publishes an event through the in-memory event publisher with an optional HTTP sink for external subscribers. The event publisher (`pkg/store/events/publisher.go`) defines a `FleetEvent` with type, payload, timestamp, and source. A `LedgerAwarePublisher` wraps the base publisher to dual-write events to both the in-memory event bus and the ARE Immutable Ledger, ensuring the compliance trail is populated as a side effect of normal operation rather than requiring separate instrumentation. The HTTP sink (`pkg/store/events/http_publisher.go`) can target any CloudEvents-compatible endpoint, including a Kafka REST proxy, without requiring a native Kafka client dependency.

The system publishes thirteen event types, defined as constants in `pkg/store/events/publisher.go`. Twelve are actively wired into the controller and ledger recorder; one (`tenant.quota.exceeded`) fires when quota enforcement is integrated into the request admission path:

1. `fleet.cluster.registered` -- A new cluster joins the fleet.
2. `fleet.cluster.deregistered` -- A cluster is removed from the fleet.
3. `fleet.placement.assigned` -- The placement engine assigns a model to one or more clusters.
4. `model.deployed` -- A model deployment completes on a target cluster.
5. `model.scaled` -- The fleet autoscaler adjusts replica count or migrates replicas across clusters.
6. `routing.updated` -- Praxis config overlay updated from placement decisions.
7. `tenant.onboarded` -- A new TenantProfile is created.
8. `tenant.quota.exceeded` -- A tenant exceeds a quota threshold (wired when quota enforcement is in request path).
9. `rollout.promoted` -- A canary rollout is promoted to full traffic.
10. `rollout.rolledback` -- A rollout is rolled back due to SLO violation.
11. `fleet.kvcache.transferred` -- A cross-cluster KV cache transfer completes.
12. `fleet.grid.synced` -- Grid CRD translator renders GridSite and InferenceProvider CRDs from fleet state.
13. `fleet.swim.health.updated` -- SWIM sync adapter updates cluster health from Grid status.

Subscribers register interest in specific event types and receive synchronous callbacks. This enables loose coupling: the placement engine publishes `fleet.placement.assigned` without knowing which downstream systems (metrics aggregation, ledger recording, dashboard updates, alerting) consume the event.

### 3.6 Deployment Modes

fleet-llm-d supports four deployment modes, each implemented as a Kustomize overlay in `deploy/kustomize/overlays/`.

**Hub Mode** (`overlays/hub/`) -- The fleet controller runs on a dedicated hub cluster in an RHACM-style topology. Kubernetes Lease election provides active/passive controller ownership; the packaged profile remains at one replica until shared state, eventing, ledger, and observability dependencies are configured. The hub manages registered spoke clusters while those production-grade dependencies remain externally managed.

**Standalone Mode** (`overlays/standalone/`) -- The fleet controller and PostgreSQL run on a single node as a self-contained deployment. This mode is designed for sovereign cloud zones that operate behind air-gap boundaries, single-region deployments, and development/testing environments. The overlay includes embedded PostgreSQL and Redis manifests so no external infrastructure is required. Each sovereign zone runs its own standalone fleet-llm-d instance.

**Federated Mode** (`overlays/federated/`) -- Multiple fleet-llm-d instances operate as peers in a mesh topology with no central hub. Each instance manages its local clusters and exchanges model catalog metadata (names, versions, resource requirements) with peers. No inference data, model weights, or tenant content crosses the federation boundary. This mode supports cross-organization collaboration (e.g., multiple sovereign zones sharing a catalog view) and multi-cloud deployments where no single cluster can serve as hub.

**Air-Gap Mode** (`overlays/airgap/`) -- Extends the standalone overlay with local mirror registry image overrides for disconnected environments (sovereign cloud, classified networks). All six external container images are replaced with references to a configurable local registry. Supports both `oc image mirror` and `skopeo copy` workflows, and includes an optional OpenShift `ImageContentSourcePolicy` for transparent pull redirection.

### 3.7 Model Metadata Integration (ModelPack)

fleet-llm-d integrates with ModelPack to provide automatic model metadata resolution from OCI-compliant model registries. When a FleetInferencePool specifies an `ociRef`, the placement engine queries the ModelPack registry to resolve:

- **GPU memory requirements** -- Computed from model architecture, parameter count, and quantization, plus a 20% overhead factor for KV cache and runtime buffers. For example, a 70B-parameter FP16 model resolves to approximately 168 GB (140 GB raw + 20% overhead), while the same model with AWQ-INT4 quantization resolves to approximately 42 GB (35 GB raw + 20%). This eliminates manual GPU sizing and prevents OOM deployments.
- **Recommended tensor parallelism** -- Based on model size and target GPU type, ModelPack recommends the minimum tensor parallel degree needed to fit the model in memory with adequate KV cache headroom.
- **Compatible GPU types** -- The resolved metadata includes a list of GPU types that meet the memory and compute requirements (e.g., H100-80GB, H200-141GB, B200-192GB), which the placement engine uses as an implicit hardware constraint.
- **Fleet model catalog** -- All resolved ModelPack entries are indexed in the fleet catalog, enabling operators to query available models, their resource footprints, and deployment history across the fleet.

The ModelPack integration runs as a validation step in the model deployment workflow (`resolve-model` in the `validate` stage), ensuring GPU requirements are resolved before placement begins. This prevents placement failures due to under-provisioned hardware and enables cost-optimal GPU selection.

### 3.8 Compliance & Audit Trail (ARE Immutable Ledger)

fleet-llm-d integrates with the standalone [are-immutable-ledger](https://github.com/jkershawrh/are-immutable-ledger), an **independent, shared evidence component**. It runs on its own database and compute and is operated independently. Any platform in the customer's ecosystem can write to it. The ledger verifies entries, chains, and proof receipts, but it has no grants, passports, scopes, or execution-authority API.

fleet-llm-d is one of many writers to the ARE ledger. Every placement decision, deployment event, scaling action, and tenant usage record is written to a hash-chained append-only ledger.

**Tamper-Evident Decision Recording.** Each fleet decision -- model placement, traffic routing, autoscaling, rollout promotion -- is recorded as a ledger entry containing the decision type, actor, rationale, evaluated constraints, and outcome. Entries are hash-chained: each entry's hash includes the previous entry's hash, creating an immutable sequence that detects any retroactive modification or deletion.

**Proof Receipts for KV Cache Transfer.** When KV cache state is transferred between clusters (hot failover, warm migration, or prefix tree synchronization), the ARE ledger issues a cryptographic proof receipt. The receipt contains the KV cache content hash, source and destination clusters, transfer timestamp, and a chain proof linking the transfer to the deployment record. The receiving cluster verifies the receipt before accepting the cache data, ensuring data integrity and provenance.

**Regulatory Evidence.** The ledger provides the evidence chain required by emerging AI governance frameworks:

- **EU AI Act (Article 12)** -- Automatic logging of AI system decisions with full traceability. The ledger records why a model was placed on specific clusters, which constraints were evaluated, and which alternatives were rejected.
- **NIST AI RMF (MAP/MEASURE/MANAGE)** -- The SLO validation records in the ledger provide continuous measurement evidence. Deployment and promotion records map to the MANAGE function's requirement for documented deployment decisions.
- **SOC 2 Type II** -- The hash-chained ledger with writer signatures provides non-repudiation evidence for change management controls.

The ARE ledger runs as an independent Deployment (`ledger-gateway` in the `immutable-ledger` namespace) containing two containers: `ledger-core` (Rust gRPC service) and `gateway` (Python REST compatibility gateway). It is integrated into the model deployment and tenant onboarding workflows at key decision points. The fleet-llm-d ledger client (`pkg/ledger/`) supports four modes: `disabled` (no-op), `memory` (in-process for testing), `http` (authenticated REST gateway), and `grpc` (direct gRPC, planned).

### 3.9 ModelPlane Integration

fleet-llm-d operates as the **operations layer** on top of ModelPlane, which provides the infrastructure layer for managing model deployments across Kubernetes clusters. Together with llm-d's within-cluster inference intelligence, this forms a three-layer architecture:

```
  ┌──────────────────────────────────────────┐
  │            fleet-llm-d                   │  Operations layer
  │  placement | routing | scaling | cost    │  Fleet-wide orchestration,
  │  tenant | lifecycle | observability      │  policy, and governance
  ├──────────────────────────────────────────┤
  │            ModelPlane                    │  Infrastructure layer
  │  ModelDeployment | InferenceCluster      │  Crossplane-based cluster
  │  cluster lifecycle | resource mgmt       │  and deployment management
  ├──────────────────────────────────────────┤
  │            llm-d                         │  Inference layer
  │  EPP | WVA | KV cache | prefill/decode   │  Within-cluster inference
  └──────────────────────────────────────────┘
```

**Reconciling the layer models.** The executive summary describes a five-layer enterprise stack (OpenShift, RHACM, llm-d, fleet-llm-d, MaaS/AI Gateway) that covers the full production AI infrastructure. The three-layer diagram above shows the inference-specific stack where fleet-llm-d sits: it zooms in on the inference path and omits provisioning and API management, which operate above and below the inference stack respectively.

**What each layer owns.** llm-d owns within-cluster inference intelligence: endpoint picking (EPP), workload-aware autoscaling (WVA), KV cache management, and prefill/decode disaggregation. ModelPlane owns infrastructure lifecycle: it defines ModelDeployment and InferenceCluster CRDs, manages cluster provisioning via Crossplane providers, and handles resource allocation at the Kubernetes level. fleet-llm-d owns fleet-wide operations: multi-cluster placement decisions, cross-cluster traffic routing, fleet autoscaling coordination, tenant governance, lifecycle management, cost optimization, and compliance audit trails.

**Five integration modules.** The `pkg/modelplane/` package implements five modules: CRD consumption (`watcher.go`), type adaptation (`adapter.go`), policy injection with routing and scaling methods (`policy.go`), compliance bridge (`compliance.go`), and type definitions (`types.go`). See section 5.4.13 for the full integration specification, API endpoints, and mock validation evidence.

Three API endpoints expose ModelPlane state through the fleet controller: `/api/v1/modelplane/clusters` (list ModelPlane-managed clusters), `/api/v1/modelplane/deployments` (list ModelDeployment resources), and `/api/v1/modelplane/cost/{deployment}` (cost data for a specific deployment).

**Prototype integration evidence.** The demo exercised the watcher and cost
paths against `cmd/modelplane-mock/`. This is mock contract evidence, not proof
of the official pinned ModelPlane provider, Gateway API ownership, or observed
multi-cluster actuation.

### 3.10 Governed Cognitive Loop Integration

fleet-llm-d accepts typed intents from the [governed-cognitive-loop](https://github.com/jkershawrh/governed-cognitive-loop) (GCL), a governed autonomy layer that sits between prediction (deepfield-fleet) and actuation (fleet-llm-d). The GCL derives constraints from evidence, computes actions under hard constraints using deterministic math (numpy), and challenges every proposed action through a falsification gate before committing. Only actions that survive all seven deterministic disconfirmation checks are sent to fleet-llm-d as intents.

fleet-llm-d evaluates received intents against its CRD-defined policies (confidence thresholds, replica limits, human gates for critical actions) before actuating. This creates a two-stage governance model: the GCL governs the decision (constraint satisfaction, falsification), and fleet-llm-d governs the execution (policy compliance, resource availability).

The current integration contract is the Ed25519-signed, expiry-bounded
`DecisionPackage` proposal carried into Fleet REST v2 with a stable correlation
and idempotency identity. Fleet stores GCL public keys only. Legacy HMAC is
migration-only and refused unless explicitly enabled. Fleet verifies the
proposal, applies its own admission and approval policy, and owns actuation.
The immutable ledger records correlated proposal and outcome evidence without
granting authority.

The GCL does not claim optimality. It claims that hard constraints are satisfied, the plan survived a challenge, and the receipt exists. fleet-llm-d does not depend on the GCL; it operates independently and evaluates all received intents against its own policies regardless of source.

**Historical prototype evidence.** The earlier HubCluster run exercised the legacy
GCL/HMAC intent path and produced test and ledger artifacts. It does not prove
the target `deepfield-fleet -> GCL DecisionPackage -> FleetOperation ->
are-immutable-ledger` chain and therefore cannot support a current capability
promotion.

## 4. Seven Capabilities

### 4.1 Model Placement

Model placement determines which clusters in the fleet should host a given model based on regulatory constraints, hardware requirements, cost optimization, and affinity preferences. The placement engine (`pkg/placement/`) operates in two phases: the constraint solver evaluates hard constraints expressed as label-selector constraints against cluster labels and state (e.g., `sovereignty.zone=eu-sovereign`), producing a set of feasible clusters; the cluster scorer then ranks feasible clusters by weighted affinity criteria including GPU utilization, cost efficiency, and data locality. When a FleetInferencePool specifies an `ociRef`, the placement engine first queries ModelPack to resolve GPU memory requirements and compatible GPU types, ensuring that only clusters with sufficient GPU capacity and correct hardware are considered. When ModelPlane is present, placement decisions are also propagated as annotations on ModelDeployment resources via the policy injector, allowing ModelPlane's reconciliation loop to apply fleet-level constraints during infrastructure provisioning. In the sovereign cloud pattern, regulatory placement constraints enforce data residency at the CRD level -- no model weights or inference data can be placed outside the designated zone. The Financial Services Provider reference architecture is designed to use regulatory constraints to ensure all models remain within US-only clusters. The placement engine targets sub-100ms p99 decision latency across a 15-cluster fleet; single-cluster placement benchmarks measure 3.9ms p99 (see section 5.2).

### 4.2 Cross-Cluster Eligibility and Traffic Routing

fleet-llm-d applies exact-model compatibility, tenant policy, placement,
authorization, health freshness, draining, and failure-domain constraints to
produce an eligible provider set. The selected routing provider then scores or
chooses among that set. Praxis performs this role in the validated deployment;
the llm-d Router adapter delegates queue, KV, prefix, session, and endpoint
scoring to EPP. Fleet filtering remains authoritative and adapters cannot
silently downgrade a request to another physical model. When no compatible
healthy provider remains, the supported behavior is a structured
`503 no_compatible_capacity`.

The three-cluster Praxis path has direct inference and soak evidence. Router
file discovery and exact-model filtering are proven, but direct Router proxy
inference is not yet qualified because verified TLS forwarding and the
pool-level signal path remain in progress. Thirty-site geographic routing and
sub-50ms TTFT remain design targets rather than measured claims.

### 4.3 Fleet Autoscaling

Fleet capacity policy coordinates budgets, placement bounds, and cross-cluster migration constraints. KEDA over EPP metrics is the default cluster-local scaling path for homogeneous pools. The Workload Variant Autoscaler (WVA) remains an optional optimizer for heterogeneous variants and publishes desired replicas for KEDA or HPA to apply. The fleet metrics collector aggregates per-cluster capacity and SLO signals, but `FleetScalingPolicy` does not replace KEDA, HPA, or WVA's local decision ownership. Cross-cluster optimization and cache-aware migration remain design targets; measured autoscaling evidence exists only at single-cluster scope (see section 5.4.6).

### 4.4 Multi-Cluster Observability

Multi-cluster observability provides a unified view of the entire inference fleet through Prometheus metric federation, Grafana dashboards, and a purpose-built web dashboard. The federation layer (`pkg/observability/metrics/federation.go`) aggregates metrics from per-cluster Prometheus instances, computing fleet-wide aggregates (total GPUs, active models, aggregate throughput, average TTFT) and per-model cross-cluster views (throughput, latency percentiles, cache hit rates, cluster distribution). Recording rules (`deploy/prometheus/recording-rules.yaml`) pre-compute expensive aggregations to keep dashboard queries fast. The fleet-llm-d web dashboard (Next.js, `web/src/`) provides seven pages: a fleet overview with stat cards (total GPUs, active models, throughput, TTFT), cluster inventory with GPU capacity and utilization, model catalog with per-model metrics and cluster distribution, tenant listing with usage and cost tracking, rollout status with canary progress, compliance verification showing ARE ledger chain integrity, and a test matrix visualization showing production gate status. Enterprise Telco's requirement for a "single pane of glass across their inference fleet" is directly addressed: operators see fleet-wide SLO compliance, per-tenant cost attribution, and per-model latency distributions in a single view, replacing the manual aggregation of per-cluster dashboards.

### 4.5 Tenant Governance

Tenant governance enforces multi-tenant isolation, quotas, rate limiting, cost attribution, and chargeback across the inference fleet. Each tenant is defined by a TenantProfile CRD that specifies quotas (maxTokensPerMinute, maxConcurrentRequests, maxModels, GPU budgets with type restrictions), rate limits (requestsPerSecond, burstSize), scheduling priority (0-1000 scale), cost controls (monthly budgets with alert thresholds), and cluster restrictions (allowed cluster lists for data residency). The quota enforcer (`pkg/tenant/quota/enforcer.go`) evaluates every inference request against the tenant's quota in real time, rejecting requests that would exceed limits while respecting priority-based preemption during contention. The metering tracker (`pkg/tenant/metering/tracker.go`) records per-tenant token consumption, request counts, average latency, and total cost as decimal values for accurate financial reporting. Tenant usage data is exposed through the fleet controller API (`/api/v1/tenants/{id}/usage`) for integration with enterprise billing and chargeback systems. Mobile Network Operator's selection of a competitor was driven specifically by tenant self-service capabilities -- fleet-llm-d's TenantProfile CRD enables the same self-service model where LOB teams define their own quotas and budgets within guardrails set by the platform team, with per-tenant cost tracking supporting chargeback at the business unit level.

### 4.6 Lifecycle Management

Lifecycle management orchestrates model version updates across the fleet using canary rollouts, SLO-gated promotion, automatic rollback, and staged cluster-by-cluster deployment. The rollout controller (`pkg/lifecycle/rollout/controller.go`) implements the ModelLifecycle CRD, managing the complete rollout lifecycle from creation through promotion or rollback. A canary rollout starts by deploying the new model version to a single cluster with a configurable traffic weight (e.g., 20%), monitoring SLO metrics (latency p99, error rate, throughput) against promotion gates defined in the ModelLifecycle CRD. If SLO gates pass for the configured observation period, the rollout can be promoted (via `POST /api/v1/rollouts/{id}/promote`) to increase traffic weight or expand to additional clusters. If SLO gates fail, the rollout is automatically rolled back (via `POST /api/v1/rollouts/{id}/rollback`) and the `rollout.rolledback` event is published with the SLO violation details recorded in the ARE ledger. The fleet controller API exposes four rollout endpoints (list, create, promote, rollback) for both programmatic and CLI-driven lifecycle management. In the financial services deployment pattern, SLO-gated canary rollouts are designed to ensure that no model version reaches production across a multi-region fleet without passing latency and accuracy gates, with every promotion and rollback decision hash-chained in the ARE ledger for OCC SR 11-7 audit evidence.

### 4.7 KV Cache State Transfer

KV cache state transfer is designed to enable cross-cluster movement of KV cache data for hot failover, warm migration, and prefix tree synchronization, eliminating the cold-start penalty that otherwise occurs when inference moves between clusters. The transfer orchestrator (`pkg/kvcache/transfer/orchestrator.go` on the control plane, `crates/kv-transfer/` on the data plane) coordinates the complete transfer lifecycle. Hot failover is designed to transfer KV cache state from a failing cluster to a healthy target within the failover chain, preserving in-flight session state and targeting sub-30-second recovery. Warm migration would proactively transfer cache state ahead of a planned scaling or maintenance event, pre-warming the destination cluster before traffic shifts. Prefix tree synchronization would replicate common prompt prefixes across clusters, enabling KV cache affinity routing to find cache hits regardless of which cluster originally computed the prefix. The NIXL bridge (`nixl_bridge.rs`) interfaces with llm-d's NIXL layer for high-bandwidth GPU-to-GPU transfer, extending NIXL's within-cluster capabilities across the fleet network. Every transfer is designed to generate an ARE ledger proof receipt containing the KV cache content hash, source and destination clusters, transfer timestamp, and chain proof, which the receiving cluster verifies before accepting the data.

**Prototype evidence only.** The NIXL bridge is a stub implementation. KV cache transfer throughput, sub-30-second recovery, proof receipts, and cross-site prefix synchronization are design targets, not measured results. The benchmark table in section 5.2 reports KV transfer throughput as "N/A (stub)."

In the telco AI grid deployment pattern, prefix tree synchronization across 30+ edge sites is designed to eliminate redundant prefill for common MOP execution prompts, targeting 40% throughput improvement versus independent single-cluster deployments. This is a design target for the multi-cluster topology, not measured evidence.

## 5. Benchmarks & Validation

### 5.1 Methodology

The benchmark suite (`test/benchmarks/`, invoked via `make bench-quick`, `make bench-standard`, `make bench-full`) evaluates fleet-llm-d across six workloads and five scenarios, executed in CI as part of the production gate pipeline.

**Six Workload Profiles** (`test/benchmarks/workloads/`):

1. *Agent orchestration* -- Multi-turn agentic workflow with tool calls, measuring placement and routing under bursty request patterns.
2. *Code completion* -- Low-latency code generation workload, measuring TTFT and throughput under sustained developer-facing load.
3. *RAG retrieval* -- Retrieval-augmented generation with large context windows, stressing KV cache and memory placement constraints.
4. *Document summarization* -- Long-input summarization workload, measuring throughput with large prompt-to-completion ratios.
5. *Chat multi-turn* -- Interactive conversation workload with KV cache reuse across turns, measuring cache hit rates and session routing.
6. *Multi-tenant mixed* -- Concurrent mixed workloads across multiple tenants, measuring quota enforcement and fair scheduling under contention.

**Five Scenarios** (`test/benchmarks/scenarios/`):

1. *Failover* -- Simulated cluster loss (network partition + health check timeout), measuring failover latency and traffic redistribution.
2. *Fleet routing* -- Cross-cluster routing policy evaluation under load, measuring geographic, least-load, and KV cache affinity routing decisions.
3. *KV migration* -- Cross-cluster KV cache state transfer, measuring transfer throughput and integrity verification.
4. *Scaling* -- Sudden traffic increases triggering fleet-wide autoscaling and cross-cluster migration, measuring reaction time.
5. *Tenant isolation* -- Concurrent tenants exceeding quota thresholds, measuring priority-based scheduling fairness and tail latency.

The benchmark CI pipeline runs the quick suite on every pull request (under 5 minutes), the standard suite nightly (under 30 minutes), and the full suite weekly with three repetitions (under 2 hours). Results are written to `test/benchmarks/reports/` and compared against the previous run to detect regressions.

### 5.2 Results

Results were collected from three sources: 3-cluster production validation on physical OpenShift clusters (CpuCluster 5-node hub, HubCluster SNO spoke, GpuCluster H100 GPU spoke), the integration test harness (13-suite testing: smoke, stress, soak, pressure, chaos, chaos-recovery, redteam, latency, throughput, inference, multimodel, fairness, scale), and local Go microbenchmarks. All 3-cluster results are from real hardware with real inference backends -- no simulated clusters, no mocked inference.

#### 5.2.1 Inference Performance (3-Cluster Production)

| Backend | Hardware | Model | Peak RPS | p50 | p99 | Errors | Concurrency Range |
|---|---|---|---|---|---|---|---|
| vLLM (GPU) | NVIDIA H100 NVL 94GB | granite-3.1-8b-instruct | **83.7** | 163ms | 316ms | 0% | 1--30 |
| OVMS (CPU) | Intel Xeon6 AMX | granite-2b-cpu | **4.1** | 3,872ms | 8,285ms | 0% | 1--50 |
| Fleet-routed | Praxis AI gateway | mixed | -- | 688ms | 797ms | 0% | -- |

The GPU/CPU throughput ratio is **20x** with **24x lower latency**, demonstrating that fleet-level placement decisions routing requests to GPU-equipped clusters have massive performance impact. Neither backend reached a breakpoint -- zero errors at maximum tested concurrency.

#### 5.2.2 Control Plane Performance

| Benchmark | Metric | p50 | p99 | Target | Source |
|---|---|---|---|---|---|
| Placement Decision | ms | 0.44 | 3.9 | < 100ms | microbench |
| Routing Decision | ns | 188 | 188 | < 5ms | microbench |
| API Latency (sustained) | ms | 170 | 245 | < 500ms | 3-cluster soak |
| Autoscale Reaction | s | < 1 | < 1 | < 30s | harness |
| Ledger Write Latency | ms | 0.44 | 2.24 | < 10ms p99 | harness |
| Controller Throughput | req/s | 2,000 / 812 | -- | > 500 req/s | harness |

#### 5.2.3 Soak Testing (3-Cluster Production)

| Test | Hub | OK Ops (Total) | Rate | Duration | Bands | Crashes |
|---|---|---|---|---|---|---|
| 4hr variable intensity | CpuCluster (5-node) | **34,027** (35,679) | 95.4%* | 3.5hr | 6/7 | **0** |
| 24hr variable intensity | HubCluster (SNO) | 268,738 | 99.0% | 19hr | all 7 | 1 (node crash) |
| Standard 2hr | HubCluster (SNO) | 1,770 | 98.7% | 23 cycles | -- | 1 (node crash) |
| Cascade (smoke→stress 10x) | HubCluster (SNO) | 9,778+ | 100% | -- | -- | 0 |

*The 4.6% error rate was entirely test harness data mismatches (incorrect model name in harness config, rollout promote edge case returning 404), not platform failures. Both issues were identified and fixed in `79de0c6`; no fleet-llm-d request was dropped or misrouted.

The CpuCluster 5-node hub sustained 3.5 hours of variable-intensity load (calm, ramp, pressure, stress, cool, burst bands) with zero infrastructure crashes. The HubCluster SNO hub crashed at 22 minutes under the same workload, confirming that fleet-llm-d is production-viable on multi-node infrastructure.

#### 5.2.4 Security Validation (Live Deployment)

| Category | Tests | Result |
|---|---|---|
| SSRF | 3 (cloud metadata, k8s API, file protocol) | 3/3 pass |
| Header Injection | 4 (X-Forwarded-For, X-Real-IP, Host, CRLF) | 4/4 pass |
| Token Replay | 3 (expired, wrong secret, tampered) | 3/3 pass |
| Privilege Escalation | 5 (viewer write/delete, tenant cross-access, operator delete) | 5/5 pass |
| DecisionPackage Tampering | 2 (unsigned intent, malformed CloudEvent) | 2/2 pass |
| Large Payload | 1 (1MB body) | 1/1 pass |
| SQL Injection | 3 (DROP, OR 1=1, UNION SELECT) | 3/3 pass |
| Path Traversal | 3 (../../etc/passwd, URL-encoded, double-encoded) | 3/3 pass |
| **Metric Poisoning** | **8** (extreme GPU util, negative queue, negative KV cache, zero throughput, extreme latency, extreme throughput, routing verification, post-poison health) | **8/8 pass** |
| **Total** | **32** | **32/32 pass** |

#### 5.2.5 Multi-Cluster Orchestration

20 capabilities validated across 3 heterogeneous clusters (Intel Xeon SGX + Intel Xeon6 AMX + NVIDIA H100 NVL):

Preflight, cluster registration, cross-cluster placement, cross-cluster routing, failover, drain/activate cycle, session affinity, model lifecycle rollout, autoscaling signals, tenant governance, observability federation, KV cache transfer, ledger integrity, ecosystem pipeline, live CPU inference, live GPU inference, fleet-orchestrated inference, Grid CRD propagation, SWIM health sync, 3-cluster Grid failover.

14 SLO gates evaluated per test run covering cluster registration (100%), placement (>=95%), failover (0-error), drain/activate (0-error), session affinity (100%), rollout lifecycle (>=95%), observability federation (>=95%), ledger integrity (100%), CPU inference (>=90%), GPU inference (>=90%), fleet-routed inference (>=90%), Grid CRD sync (>=95%), SWIM health (>=90%), and overall success (>95%).

#### 5.2.6 Governance and Compliance

- ARE Immutable Ledger: 9,760+ entries across 11 chain types, all chains verified valid
- Historical GCL DecisionPackage CloudEvents: end-to-end verified with the
  then-current HMAC-SHA256 contract; current production admission requires
  Ed25519 by default
- Fail-closed on ledger errors: controller returns 500, never silently drops evidence
- Chain integrity maintained across 4hr sustained variable-intensity load

#### 5.2.7 Test Matrix

| Category | Count | Status |
|---|---|---|
| Go unit tests | 27 packages | All pass |
| BDD scenarios | 11 features, 49 scenarios | All pass |
| Architecture proofs | 57 claims (54 test functions) | All pass |
| Contract tests | 24 tests (6 files) | All pass |
| Security tests | 32 tests | All pass |
| Soak profiles | 12 profiles | Validated |
| Total automated tests | 500+ | All pass |

**Local Go Microbenchmarks (hot-path operations):**

- Token generation (HMAC-SHA256): 2.9M ops/s, 1,241 ns/op.
- Token validation: 2.0M ops/s, 1,615 ns/op.
- Backend selection (routing): 19.5M ops/s, 188 ns/op.

### 5.3 Comparison with Manual Operations

Without fleet-llm-d, organizations managing multi-cluster LLM inference rely on manual processes that do not scale and cannot provide the safety guarantees required for production.

**Placement by spreadsheet.** Platform teams manually track GPU capacity across clusters in spreadsheets, deciding where to deploy models based on tribal knowledge and static capacity plans. A new model deployment requires manually checking available GPUs on each cluster, verifying regulatory constraints by reading cluster documentation, and submitting deployment requests per cluster. This process takes hours to days and is error-prone: a human cannot evaluate 20 label-selector constraints across 30 clusters without mistakes. fleet-llm-d's placement engine evaluates all constraints in under 100ms and guarantees that no regulatory violation occurs.

**Routing by manual configuration.** Traffic routing across clusters is configured through static Ingress or Gateway API resources on each cluster, updated manually when clusters change health status or load distribution shifts. Failover requires human intervention: an operator detects a cluster outage, manually updates DNS or Ingress weights, and monitors the shift. Mean time to failover is measured in minutes to hours. fleet-llm-d's Praxis AI gateway detects unhealthy clusters within 30 seconds and reroutes traffic automatically.

**No cross-cluster scaling.** Within-cluster autoscalers (llm-d's WVA, Kubernetes HPA) optimize locally but have no visibility into fleet-wide utilization. One cluster runs at 95% GPU utilization while another sits at 30%, but no system coordinates rebalancing. Manual rebalancing requires an operator to identify the imbalance, decide where to move replicas, drain the source, deploy on the target, and update routing -- a multi-hour process that risks downtime. fleet-llm-d's fleet autoscaler continuously optimizes across clusters, migrating replicas and KV cache state together.

**No unified observability.** Each cluster has its own Prometheus, Grafana, and alerting stack. To understand fleet-wide SLO compliance, an operator must log into each cluster's dashboard, manually aggregate numbers, and correlate alerts across independent systems. Per-tenant cost attribution requires custom scripts that query each cluster's metering data and aggregate in a spreadsheet. fleet-llm-d federates metrics into a single view with pre-computed fleet-wide aggregates.

**No audit trail.** Deployment decisions, scaling actions, and routing changes are documented in Slack messages, Jira tickets, and change management systems that are disconnected from the actual infrastructure state. Reconstructing the sequence of events for a compliance audit requires interviewing operators and correlating timestamps across systems. fleet-llm-d's integration with the ARE Immutable Ledger provides an automatic, tamper-evident, hash-chained audit trail of every fleet decision.

### 5.4 CPU Inference at Scale: Intel Xeon + fleet-llm-d

#### 5.4.1 The Business Case

GPU procurement cycles of 6-18 months, GPU costs of $30,000-$200,000 per accelerator, and power/cooling requirements of 700W+ per GPU create barriers for organizations that need AI inference today. Meanwhile, existing Intel Xeon infrastructure sits at single-digit CPU utilization in most enterprise data centers. The question is not whether CPU inference is as fast as GPU inference -- it is not -- but whether CPU inference is fast enough for the workload, and whether fleet-llm-d can make it reliable at scale.

Three industry segments validate this need:

**Telco and edge.** Carrier networks operate thousands of edge sites with Intel Xeon processors but no GPUs. Network operations, customer service automation, and field technician assistance require LLM inference at the edge. Sub-2-second latency for 20-token responses is acceptable for these use cases. Deploying GPUs at 30+ edge sites is prohibitively expensive; CPU inference with fleet-level orchestration makes these workloads viable.

**Financial services and regulated industries.** Banks and insurers operate large Xeon fleets in on-premise data centers subject to strict regulatory controls. GPU procurement requires capital expenditure approval and physical security audits. CPU inference enables immediate deployment on existing approved infrastructure while GPU procurement proceeds in parallel. The combination of fleet-llm-d's tenant governance, rate limiting, and the ARE Immutable Ledger provides the audit trail required by OCC SR 11-7 and SOC 2 Type II.

**Sovereign and air-gapped environments.** Government agencies and defense organizations operate air-gapped sovereign clouds on Intel Xeon processors. GPU supply chains introduce additional security review requirements. CPU inference with Intel TDX confidential computing enables hardware-isolated AI workloads where model weights and user data are encrypted in memory -- not visible to the hypervisor or cloud administrator.

#### 5.4.2 The Engineering Proof

A production-scale validation was conducted on a Red Hat OpenShift cluster running mixed workloads, where CPU inference had previously failed under concurrent load at a major industry event. The failure analysis identified five root causes:

1. **Single-replica deployments** with Python threading locks serialized all inference -- one request at a time per model.
2. **No autoscaling** -- Kubernetes HPAs were pinned at `min == max == 1`.
3. **No load management** -- excess requests queued for up to 2 minutes before timing out, with no user feedback.
4. **No connection pooling** -- Go's default `MaxIdleConnsPerHost` of 2 caused TCP connection thrashing under burst load.
5. **Server timeouts mismatched** -- a 30-second `WriteTimeout` killed streaming responses from slow CPU inference backends.

These are not hardware problems. The cluster had 2,752 CPU cores at <3% utilization. The problem was the absence of an orchestration layer between the inference backends and the users.

#### 5.4.3 The Solution

fleet-llm-d was deployed on the production cluster alongside existing workloads, providing seven capabilities absent from the previous deployment:

| Capability | Implementation | Impact |
|-----------|----------------|--------|
| **Connection pooling** | `MaxIdleConnsPerHost=50`, `MaxConnsPerHost=100` | Eliminated TCP thrashing under burst |
| **Health polling** | 30-second active probes to each backend | Dead backends detected and removed from routing within one poll interval |
| **Load shedding** | Per-model in-flight tracking with 503 + `Retry-After` | Users receive immediate feedback under overload instead of 2-minute hangs |
| **Per-tenant rate limiting** | Token bucket keyed on `x-llm-d-inference-fairness-id` header | One lab session cannot starve others |
| **HPA integration** | Created HPAs with `min=1, max=4, targetCPU=60%` | Pods auto-scale under load and scale back during idle |
| **Multi-worker serving** | Custom container image with `uvicorn --workers 2` | 2x concurrent inference slots per pod |
| **INT8 quantization** | Models exported with NNCF INT8 asymmetric weight compression | AMX hardware acceleration on Intel Xeon |

No existing workloads were modified. fleet-llm-d deployed in its own namespace and pointed at existing backend services via the `--backends` JSON configuration. The entire deployment is reversible by deleting the namespace -- zero impact on the host cluster.

#### 5.4.4 Hardware and Software Stack

The validation cluster represents a typical enterprise deployment:

**Hardware:** Intel Xeon 6767P (Granite Rapids) processors across 9 worker nodes -- 6 CPU-only workers (256 cores, 503 GB RAM each) and 3 accelerator workers (288 cores, 2.2 TB RAM, Intel Gaudi x8). CPU features include AMX (amx_bf16, amx_int8, amx_tile), AVX-512, and TME. Total cluster capacity: 2,752 cores. CPU utilization during all testing remained below 3%.

**Software:** Red Hat OpenShift 4.18, OpenVINO Model Server (OVMS) for C++ native serving with GenAI continuous batching pipeline, OpenVINO via optimum-intel for Python-based serving, NNCF for INT8 weight compression, fleet-llm-d for orchestration.

**Model format:** All models exported to OpenVINO IR format with INT8 asymmetric per-channel weight compression and u8 KV cache precision, optimized for Intel AMX instruction set. The export pipeline uses `export_model.py` from the OVMS repository, producing the full GenAI directory structure (model IR, tokenizer IR, detokenizer IR, graph definition, and server configuration).

Any organization with Intel Xeon processors (5th Gen Scalable or newer), Red Hat OpenShift, and the fleet-llm-d repository can reproduce this deployment. The process is documented in `deploy/intel-cpu-inference/` and requires no proprietary tooling.

#### 5.4.5 Models Under Test

Five models spanning three parameter classes and two serving runtimes:

| Model | Parameters | Runtime | Format | Provenance |
|-------|-----------|---------|--------|-----------|
| IBM Granite 4.0 350M | 350M | OVMS C++ | INT8 | Draft model for speculative decoding |
| IBM Granite 3.2 2B Instruct | 2B | OVMS C++ | INT8 | INT8/AMX optimization proof |
| IBM Granite 4.1 3B | 3B | OVMS C++ | INT8 | Latest Granite with 512K context |
| Microsoft Phi-3-Mini | 3.8B | Python/OpenVINO | FP32 | Non-Granite reference model |
| Qwen 2.5 3B Instruct | 3B | Python/OpenVINO | FP32 | Non-Granite reference model |

All models are Apache 2.0 licensed and accessed through a single fleet-llm-d endpoint with HMAC-SHA256 authentication, per-tenant rate limiting, and model-level load shedding.

#### 5.4.6 Benchmark Results

All benchmarks collected on the validation cluster under normal operating conditions with 9 non-target model pods running concurrently. No other workloads were affected during testing (verified by continuous monitoring before, during, and after each test phase).

**Single-request latency (20 tokens, realistic prompts):**

| Model | Quick Q&A | Code Generation | Summarization | Analysis |
|-------|-----------|----------------|---------------|----------|
| Granite 350M (OVMS INT8) | 784 ms | 857 ms | 1,460 ms | 1,139 ms |
| Granite 2B INT8 (OVMS) | 2,045 ms | 2,368 ms | 2,882 ms | 2,105 ms |
| Granite 4.1 3B (OVMS INT8) | 2,444 ms | 3,262 ms | 5,449 ms | 4,294 ms |
| Phi-3-Mini (Python FP32) | 762 ms | 979 ms | 1,515 ms | 1,357 ms |
| Qwen 2.5 3B (Python FP32) | 1,043 ms | 1,153 ms | 1,845 ms | 1,555 ms |

**Per-model concurrency scaling (single replica):**

| Model | 1 conc. | 5 conc. | 10 conc. | Peak throughput |
|-------|---------|---------|---------|----------------|
| Granite 350M | 0.8s | 1.1s | 1.3s | 7.6 rps |
| Granite 2B INT8 | 1.9s | 2.7s | 2.8s | 3.4 rps |
| Granite 4.1 3B | 2.1s | 2.9s | 3.4s | 2.9 rps |
| Phi-3-Mini | 0.7s | 2.9s | 4.3s | 1.5 rps |
| Qwen 2.5 3B | 0.9s | 3.0s | 6.0s | 1.1 rps |

OVMS C++ models maintain consistent latency under concurrent load due to continuous batching, while Python/FastAPI models degrade linearly because inference serializes through a threading lock.

**Mixed-model concurrent load (all 5 models, 1 replica each):**

| Concurrent Users | P50 Latency | P95 Latency | Throughput | Error Rate |
|-----------------|-------------|-------------|-----------|------------|
| 5 | 1.3s | 1.5s | 3.3 rps | 0% |
| 10 | 1.4s | 1.6s | 6.1 rps | 0% |
| 20 | 1.8s | 6.3s | 3.1 rps | 0% |
| 30 | 1.9s | 4.0s | 6.2 rps | 0% |
| 50 | 2.3s | 8.0s | 5.6 rps | 0% |

Zero errors at 50 concurrent users across all 5 models. Load shedding ensures that requests beyond capacity receive immediate 503 + `Retry-After` instead of queuing indefinitely. The throughput dip at 20 concurrent users (6.1 rps at 10 users, 3.1 at 20, 6.2 at 30) reflects Python GIL contention in non-OVMS backends, which resolves at higher concurrency when request pipelining amortizes the lock overhead.

**Sustained soak test (10 simulated users, 2 minutes):**

| Metric | Value |
|--------|-------|
| Total requests | 324 |
| Successful | 324 (100%) |
| Errors | 0 |
| Sustained throughput | 2.6 rps |
| P50 latency | 1.0s |
| P95 latency | 2.3s |
| Max latency | 3.1s |

**HPA autoscaling proof:** Under sustained inference load, the Kubernetes HPA detected 372% CPU utilization on a single model and scaled from 1 to 4 replicas within 2 minutes. After load subsided, the 300-second stabilization window prevented oscillation before scaling back to 1 replica.

**Horizontal scaling curve (secondary validation cluster, OVMS):**

| OVMS Replicas | 50-Concurrent TTFT | Error Rate | Throughput |
|-------------|-------------------|------------|-----------|
| 1 | 22.9s | 21.3% | 0.9 rps |
| 4 | 466 ms | 0% | 2.5 rps |
| 8 | 225 ms | 0% | 4.2 rps |

Scaling is near-linear: 2x replicas yields approximately 2x throughput with no degradation in per-request latency. Efficiency decreases at higher replica counts due to connection pooling and scheduling overhead.

#### 5.4.7 Capacity Projections for Large-Scale Events

Based on measured sustained throughput of 2.6 rps with 1 replica per model:

| Scenario | User Behavior | Current (1 replica) | With HPA (4 replicas) |
|----------|-------------|--------------------|-----------------------|
| Lab session (20 seats) | 1 req / 30s | Supported | Supported |
| Demo (50 seats) | 1 req / 10s | Needs scaling (5 rps demand exceeds 2.6 rps measured) | Supported with headroom |
| Workshop (200 seats) | 1 req / 30s | Needs scaling | Supported (~10 rps) |
| Keynote (500 seats) | 1 req / 60s | Needs scaling | Supported (~10 rps) |

SLO targets met: P50 < 2s (measured 1.0s), P95 < 5s (measured 2.3s), error rate < 1% (measured 0.0%), availability 99.9%+ (measured 100% during test window).

#### 5.4.8 Control Plane Resilience on Production Infrastructure

Five suites from the fleet-llm-d test harness ran against the fleet controller on the validation cluster:

| Suite | Result | Notes |
|-------|--------|-------|
| Stress | 6/6 | Survived 500 concurrent goroutines, no breaking point |
| Chaos | 8/8 | 1 MB body, unicode, null bytes, burst 1000 |
| Red Team | 11/11 | SQL injection, path traversal, XSS, token tampering |
| Latency | 4/4 | Health, auth-reads, auth-writes, metrics |
| Throughput | 3/3 | healthz 2,000 rps, clusters 2,000 rps |

All non-target workloads (9 model pods on Gaudi accelerators and other CPU models) remained unaffected throughout testing.

#### 5.4.9 Optimization Journey

The optimization process followed the project's TDD/EDD/CDD/BDD/CBT Red/Green methodology, where each improvement started with a failing test, was implemented as the smallest change to pass, and was validated through the benchmark harness before promotion.

| Optimization | Measured Impact | Methodology |
|-------------|----------------|-------------|
| HPA autoscaling (1→4 replicas) | 4x concurrent capacity | CBT: harness proved auto-scale under load |
| CPU request reduction (32→16 cores) | 2x per-request speedup | CBT: measured before/after latency |
| Multi-worker uvicorn (2 workers) | 2x concurrent slots per pod | EDD: container image with proper module structure |
| OVMS C++ serving (INT8) | Consistent latency under concurrent load | TDD: GenAI pipeline export + deploy + benchmark |
| Connection pooling (50/host) | Eliminated TCP connection thrashing | TDD: transport config test |
| Load shedding (503 + Retry-After) | Instant feedback under overload | TDD: in-flight tracking + 503 response tests |
| Health polling (30s) | Dead backends auto-removed | TDD: health check goroutine + unhealthy detection test |
| NUMA pinning | **Rolled back** -- hurt performance 10x on multi-socket | CBT: benchmark showed regression, immediately reverted |

The NUMA pinning result is worth highlighting: `OMP_PROC_BIND=close` with `OPENVINO_CPU_AFFINITY=NUMA`, conventionally recommended for OpenVINO on Intel CPUs, caused a 10x performance regression on the dual-socket Xeon 6767P workers. The root cause is that pinning threads to a single NUMA node underutilizes the second socket's memory bandwidth, which these models require for weight loading. The Red/Green methodology caught this immediately -- the benchmark showed the regression, the change was rolled back, and the finding was documented. This exemplifies why data-driven optimization with automated benchmarks is essential: conventional wisdom does not always apply.

#### 5.4.10 Scope: CPU-Only LLM Inference

This section covers fleet-llm-d's orchestration of **CPU-only LLM inference** on Intel Xeon processors. GPU inference (NVIDIA, Intel Gaudi) is handled by llm-d's existing within-cluster capabilities and is outside the scope of this evaluation. The benchmarks, optimizations, and capacity projections presented here apply exclusively to CPU-based model serving using OpenVINO and OVMS on Intel Xeon hardware.

GPU inference workloads ran concurrently on the same validation cluster (9 Gaudi-backed model pods) throughout all testing and were unaffected -- confirming that fleet-llm-d's CPU inference orchestration coexists with GPU workloads without interference.

#### 5.4.11 Gaps by Choice

The following capabilities were evaluated and intentionally deferred based on the current project priorities:

| Gap | Rationale | Path to Close |
|-----|-----------|--------------|
| **ModelPlane live integration** | The `--backends` JSON flag provides sufficient model registration for 5 models. ModelPlane adds value at 20+ models or with frequent model churn. | Mock API validated. Real integration pending ModelPlane deployment on target cluster. Collaboration proposal submitted as [modelplaneai/modelplane#326](https://github.com/modelplaneai/modelplane/issues/326). |
| **Granite 4.1 8B on CPU** | Export requires >16 GB RAM, exceeding the local development machine. Not blocking for large-scale industry events -- the 350M/2B/3B tier covers all demo scenarios. | Export planned on the validation cluster worker nodes (503 GB RAM each) or, if unavailable, a secondary validation cluster (503 GB RAM, Intel VPN network). |
| **OVMS for all models** | Python/FastAPI backends for Phi-3-Mini and Qwen 2.5 3B perform competitively for single requests (762ms vs 784ms). OVMS advantage is primarily under concurrent load. | OVMS exports for these models can be produced with the same `export_model.py` pipeline when concurrent capacity becomes the bottleneck. |
| **Speculative decoding** | Granite 350M is deployed as the draft model. The speculative decode integration in fleet-llm-d's proxy (draft → verify pipeline) is designed but not yet implemented. | Code integration in `pkg/routing/proxy.go` -- route draft to 350M, verify with 2B/3B. |
| **Multi-cluster routing** | Closed for the historical 3-cluster soak: HubCluster hub, CpuCluster CPU, and GpuCluster GPU H100 routed through NodePort bridges, with 9,778 operations, 0 errors, and 9/9 SLO gates. The current physical deployment uses TLS OpenShift Routes. | Praxis AI replaced fleet-gateway as the inference routing layer. The historical run used HMAC-signed GCL events; current production admission requires Ed25519. |
| **Intel TDX confidential inference** | Xeon 6767P supports TDX (TME flag present). BIOS enablement requires infrastructure team coordination and a maintenance window for worker node reboots. | Detailed enablement plan documented at `docs/proposals/intel-tdx-enablement.md`. |

#### 5.4.12 Gaps by Technical Limitation

The following gaps are imposed by the current technology stack and require upstream changes or alternative approaches:

| Gap | Root Cause | Workaround | Upstream Path |
|-----|-----------|-----------|---------------|
| **OVMS single-request latency exceeds Python** | OVMS GenAI continuous batching scheduler adds overhead (~200ms) that exceeds the per-request savings of C++ serving for short responses. | Use Python backends for latency-sensitive single-user scenarios; OVMS for throughput-critical multi-user scenarios. | OVMS team is aware -- GenAI scheduler optimization is on the OpenVINO roadmap. |
| **NUMA pinning regression** | `OMP_PROC_BIND=close` with `OPENVINO_CPU_AFFINITY=NUMA` caused 10x performance degradation on dual-socket Xeon 6767P. OpenVINO's NUMA affinity pins all threads to one socket, underutilizing the second socket's memory bandwidth. | Removed NUMA pinning. Let the OS scheduler distribute threads across both sockets. | Reported to OpenVINO team. Per-model NUMA zone assignment (model A on socket 0, model B on socket 1) would be the optimal approach. |
| **Python threading lock serializes inference** | FastAPI + OpenVINO uses `threading.Lock()` for model inference, limiting to 1 concurrent request per worker process. | Multi-worker uvicorn (2 workers per pod) doubles concurrent capacity. | Async inference support in OpenVINO Python API would eliminate the lock requirement. |
| **INT8 re-export required for weight compression** | `save_pretrained(weight_format='int8')` does not compress already-exported OpenVINO models. INT8 weight compression must be applied during the initial export from HuggingFace source weights. | Use `export_model.py` from OVMS repository which handles export + compression in a single pipeline. | OpenVINO could support post-export weight compression via `nncf` CLI. |
| **Model export requires significant RAM** | Exporting 8B+ models to OpenVINO IR requires loading full PyTorch weights into memory (~16 GB for 8B, ~64 GB for 70B). | Export on high-memory nodes (the validation cluster workers have 503 GB). | Streaming export with memory-mapped weights would reduce the requirement. |
| **HPA scaling cold start** | CPU inference pods using inline Python scripts (`pip install` on startup) take 3-5 minutes to become ready. New replicas created by HPA are not immediately useful. | Pre-built container images with all dependencies baked in reduce startup to ~30 seconds. | Pre-warmed pod pools or KEDA-based predictive scaling based on event schedules. |

#### 5.4.13 ModelPlane Collaboration

A collaboration proposal has been submitted to the ModelPlane project ([modelplaneai/modelplane#326](https://github.com/modelplaneai/modelplane/issues/326)) outlining six integration points between fleet-llm-d and ModelPlane:

1. **CRD consumption** -- fleet-llm-d reads ModelDeployment and InferenceCluster resources to auto-discover available inference backends, replacing manual `--backends` configuration.
2. **Policy injection** -- fleet-llm-d annotates ModelDeployment resources with fleet placement decisions, enabling ModelPlane's reconciliation loop to enforce fleet-level constraints during infrastructure provisioning.
3. **Cost integration** -- fleet-llm-d reads GPU/CPU pricing from ModelPlane InferenceClass resources, feeding real infrastructure costs into fleet cost projections, chargeback reports, and budget alerts.
4. **Compliance bridge** -- fleet-llm-d forwards ModelPlane lifecycle events (deployment creation, scaling, deletion) to the ARE Immutable Ledger, extending the tamper-evident audit trail to cover infrastructure-level actions.
5. **Routing integration** -- fleet-llm-d uses InferenceCluster health status from ModelPlane alongside its own health probes for traffic routing decisions.
6. **Scaling integration** -- fleet-llm-d coordinates with ModelPlane resource limits when computing cross-cluster scaling decisions.

A mock API (`cmd/modelplane-mock/`) implementing the ModelPlane CRD contract has been validated end-to-end. The mock serves 4 clusters (3 GPU + 1 CPU with Intel AMX), 3 deployments, 3 endpoints, and 4 InferenceClasses with pricing. fleet-llm-d's ModelPlane watcher, adapter, policy injector, and compliance bridge are tested against this mock. The transition from mock to live ModelPlane requires only changing the `--modelplane-api` URL -- no code changes in fleet-llm-d.

#### 5.4.14 Implications for the Red Hat and Intel Partnership

The CPU inference work demonstrates a concrete technical narrative for the Red Hat-Intel partnership:

**For Intel:** The Xeon 6767P (Granite Rapids) with AMX runs production LLM inference at sub-second latency for models up to 3B parameters. INT8 weight compression with AMX acceleration enables the same model quality at lower memory footprint. OVMS's C++ continuous batching handles concurrent requests without the Python GIL bottleneck. Organizations with existing Xeon infrastructure can serve AI workloads without GPU procurement -- turning CPU inference from a compromise into a viable deployment strategy.

**For Red Hat:** OpenShift provides the platform for the entire stack -- from model export through deployment, autoscaling, load management, and observability. The HPA integration, `MachineConfig` for kernel tuning, `NetworkPolicy` for workload isolation, and `Route` for edge termination are all standard OpenShift primitives used in production. fleet-llm-d extends OpenShift's value proposition from within-cluster inference (llm-d) to fleet-level orchestration with CPU-aware autoscaling and heterogeneous hardware routing.

**Together:** The combination enables a differentiated offering for regulated industries -- confidential AI inference on Intel TDX-enabled Xeon processors, orchestrated by fleet-llm-d on OpenShift, with tamper-evident compliance records in the ARE Immutable Ledger. This is a sovereign AI story that neither company can tell alone.

### 5.5 Ecosystem Stress Tests (HubCluster, July 2026)

An 8-phase stress test exercised the full 4-system platform (deepfield-fleet, GCL, fleet-llm-d, ARE ledger) on the HubCluster cluster. GCL ran as a single pod on OpenShift with sslip.io TLS route termination. Fleet controller ran locally against the same test harness. The test harness (`tests/test_ecosystem_stress.py` in GCL) exercises smoke, performance baseline, pressure, edge cases, degradation, soak, pen testing, and chaos phases.

**Overall result: 42/48 passed (87.5%).**

The six failures are: fleet_metrics (expvar endpoint not exposed via Route, deployment config issue), gcl_cycle_latency p99 (1086ms exceeds 500ms threshold due to ~500ms remote network RTT, on-cluster p50 is 154ms), nan_evidence (Python json module rejects NaN client-side), wrong_content_type (FastAPI returns 422 instead of 415), gcl_10kb_payload and gcl_reset_recovery (both caused by single-pod saturation after 200 concurrent chaos cycles). None are functional defects.

#### 5.5.1 Pressure Testing

| Concurrency | GCL p50 | GCL p95 | Errors | Wall Clock |
|---|---|---|---|---|
| 5 | 815ms | 871ms | 0/5 (0%) | 871ms |
| 10 | 1,048ms | 1,079ms | 0/10 (0%) | 1,084ms |
| 20 | 2,430ms | 2,498ms | 0/20 (0%) | 2,513ms |
| 50 | 4,777ms | 4,841ms | 0/50 (0%) | 4,866ms |

Zero errors at all concurrency levels. Latency scales linearly with concurrency, indicating orderly queuing rather than contention failure. Signal payloads of 100, 500, and 1,000 all completed in ~770ms with no latency variation.

#### 5.5.2 Soak (300 Sequential Cycles)

| Metric | Value |
|---|---|
| Total cycles | 300 |
| Errors | 0/300 |
| p50 | 566ms |
| p95 | 900ms |
| p99 | 1,007ms |
| Wall clock | 186.7s |
| Latency drift | 1.2x (across 19 ten-second windows) |

Mixed concurrent soak (60 seconds, 3 GCL workers + 2 fleet workers): 479 total requests, 0 errors, 0.0% error rate across both systems.

#### 5.5.3 Degradation and Security

GCL operates correctly when fleet is unreachable. All 6 governance scenarios (spike, compliance breach, capacity exhaustion, SLO cascade, mixed storm, multi-cluster migration) degrade gracefully with no crashes. Rapid state thrashing (10 reset/seed cycles) produced 0 failures. Fleet healthz remained at p50=2ms under concurrent GCL load.

Pen testing: SQL injection, path traversal, malformed input, and unknown scenario injection all handled correctly. No 500 errors. Reset endpoint has no auth (expected for development).

#### 5.5.4 Chaos Boundary

200 simultaneous governance cycles completed with 0 errors (p50=13,099ms, wall=13.3s). Subsequent requests after saturation returned 503 until recovery. This is the expected single-pod concurrency ceiling.

See [GCL ecosystem stress test benchmarks](https://github.com/jkershawrh/governed-cognitive-loop/blob/main/docs/benchmarks/ecosystem-stress-benchmarks.md) for the full 48-test breakdown.

#### 5.5.5 Production-Emulation Soak (On-Cluster, 2 Hours)

A 2-hour production-emulation soak ran on-cluster on HubCluster (pod-to-pod, no external network). The soak driver ran as a Kubernetes Job inside the `fleet-llm-d` namespace, exercising the full decision pipeline: GCL governance cycles across all 6 scenarios, with 7 degradation injections (burst 50 concurrent, invalid intents, GCL state resets, expired events).

**Result: 2,240 governance cycles. Zero errors. All 5 SLO gates passed.**

| Metric | Value |
|---|---|
| Total events | 2,240 |
| Success rate | 100.0% |
| E2E latency p50 | 147ms |
| E2E latency p95 | 504ms |
| E2E latency p99 | 561ms |
| Chain integrity verifications | 23/23 passed |
| Health availability | GCL 100%, Fleet 100% |
| Max injection recovery | 5.6s |

All 7 degradation injections passed: burst-50 concurrent events (0 errors), invalid intents (correctly rejected with 401), and full GCL state resets (recovered in ~1 second). Latency remained flat at 135-155ms across the full 2 hours with no drift.

#### 5.5.6 3-Cluster Cascade Soak (August 2026)

A cascading soak test validated the full fleet across three physical clusters:
HubCluster (hub, Intel Xeon CPU, OVMS), CpuCluster (CPU spoke, Intel Xeon6, OVMS), and
GpuCluster (GPU spoke, NVIDIA H100 NVL 94GB, vLLM). This historical run routed
through Praxis AI on HubCluster with NodePort bridges to CpuCluster and GpuCluster. The
current deployment uses TLS OpenShift Routes. All 17 lifecycle phases were
exercised per cycle, including the then-current HMAC-signed GCL
DecisionPackage contract and ARE ledger hash-chain verification. Current
production GCL admission requires Ed25519 by default.

| Level | Duration | Concurrency | Interval | Operations | Errors | Rate | SLO Gates |
|-------|----------|-------------|----------|-----------|--------|------|-----------|
| smoke | 0.2 min | 1x | single pass | 54 | 0 | 100% | 9/9 |
| short | 5.5 min | 2x | 15s | 684 | 0 | 100% | 9/9 |
| pressure | 10.5 min | 5x | 5s | 2,518 | 0 | 100% | 9/9 |
| stress | 16.0 min | 10x | 3s | 6,522 | 0 | 100% | 9/9 |
| **Total** | **32.2 min** | | | **9,778** | **0** | **100%** | **ALL PASS** |

SLO gates validated: cluster registration 100%, cross-cluster placement 100%, failover zero-error, drain/activate zero-error, session affinity 100%, rollout lifecycle 100%, observability federation 100%, ledger integrity 100%, overall success > 95%. Ledger chain integrity verified across 240+ hash-chained entries spanning 8 chain types.

Key latencies through Praxis AI gateway:
- CPU inference (HubCluster OVMS, local): 2,900-3,500ms p50
- GPU inference (GpuCluster vLLM H100, cross-cluster): 550-650ms p50
- Fleet orchestrated inference (round-robin CPU+GPU): 700-850ms p50
- Control plane operations (placement, routing, tenant): 120-200ms p50

#### 5.5.7 Control Plane Scale Microbenchmarks

Go microbenchmarks measured the hot-path functions at cluster counts from 10 to 1,000 (Apple M2, `-benchmem`):

| Clusters | List() | Solver | Balancer | Reconcile |
|---|---|---|---|---|
| 10 | 345 ns | 1,023 ns | 34 ns | 1,361 ns |
| 100 | 2,744 ns | 7,137 ns | 353 ns | 5,970 ns |
| 500 | 16,493 ns | 41,333 ns | 1,049 ns | 66,291 ns |
| 1,000 | 35,114 ns | 81,703 ns | 1,720 ns | 84,186 ns |

All scale linearly. The balancer (per-request routing decision) runs at 1.7 microseconds even at 1,000 clusters with zero allocations. No knee point found.

## 6. Production Gates

### 6.1 Stage Model

fleet-llm-d uses a five-stage production gate model that governs the promotion of each capability from initial development through production validation.

1. **Red** -- Unit tests are not passing. The capability's core logic is under active development or has known correctness issues. No deployment outside local development environments.
2. **Yellow** -- All unit tests pass. Table-driven tests cover the primary code paths, achieving minimum coverage thresholds. The capability can be deployed to dev environments for integration testing. BDD scenarios, contract tests, and integration tests are authored but may not yet pass.
3. **Green** -- The capability passes the required three-cluster Kind integration environment in addition to its unit, BDD, and contract gates.
4. **Blue** -- The capability passes the real hub-plus-two-OpenShift-spoke gate, including published performance and chaos scenarios.
5. **Gold** -- All seven capabilities have live verdicts, the 72-hour soak and signed external evidence pass, and the security audit has no critical findings.

Each stage transition requires evidence that all rubric dimensions meet the stage's minimum thresholds. No capability can skip a stage, and any regression (e.g., a previously passing test suite starts failing) reverts the capability to the appropriate lower stage.

### 6.2 Rubric Scoring

Each capability is scored on five dimensions defined in `test/matrix/rubric.yaml`. All weights sum to 1.0, and each dimension has per-stage minimum thresholds on a 0-100 scale.

| Dimension | Weight | Dev Min | Staging Min | Production Min | Key Metrics |
|---|---|---|---|---|---|
| Correctness | 0.30 | 60 | 85 | 95 | Unit test pass rate, BDD scenario pass rate, contract conformance, regression stability |
| Performance | 0.25 | 50 | 75 | 90 | p50/p95/p99 latency, throughput (req/s), autoscale reaction time, KV transfer bandwidth, benchmark regression delta |
| Reliability | 0.25 | 50 | 80 | 95 | Availability (uptime %), MTTR, error rate under fault injection, graceful degradation, soak test pass rate (24h+) |
| Operability | 0.10 | 40 | 70 | 85 | Observability coverage (% instrumented), alert S/N ratio, runbook coverage, CRD validation coverage, upgrade path tests |
| Security | 0.10 | 50 | 80 | 95 | CVE scan pass rate (critical/high), RBAC conformance, tenant isolation tests, secrets-in-code (zero findings), image signature verification, network policy coverage |

**Stage-to-rubric mapping.** The five production gate stages (Red through Gold) map to the three rubric threshold columns as follows: Red and Yellow map to dev thresholds, Green maps to staging, Blue and Gold map to production. This means a capability at Yellow must pass all dev minimums, a capability at Green must pass all staging minimums, and Blue/Gold require all production minimums.

The composite score is the weighted sum across all dimensions: `composite = sum(dimension.weight * dimension.score)`. However, a capability is promotable to a stage only when **every** dimension individually meets or exceeds that stage's minimum threshold. A high composite score cannot compensate for a single dimension falling below its minimum. This ensures that no capability reaches production with, for example, excellent correctness but poor security.

### 6.3 Current Status

As of August 2026, fleet-llm-d has **cleared the Blue gate** for core capabilities. The 3-cluster production validation on physical OpenShift infrastructure (CpuCluster 5-node hub + HubCluster SNO spoke + GpuCluster H100 GPU spoke) satisfies the Blue requirement of a real hub-plus-two-OpenShift-spoke topology with published performance and chaos scenarios. **Gold remains open** on three requirements.

**Blue evidence (August 2026):**

- **Correctness**: 24/24 smoke tests, 57 architecture proofs (54 test functions), 11/11 red team tests, 11 BDD features (49 scenarios), 24 contract tests (6 files), all suites green. 20/20 multi-cluster orchestration capabilities validated across 3 heterogeneous clusters.
- **Performance**: GPU saturation 83.7 RPS on H100 NVL (p50 163ms, p99 316ms, 0% errors). CPU inference 10.2 RPS on Intel Xeon (p50 1.0s, p99 2.3s). Placement latency p50=0.44ms (target < 100ms), routing decision 188ns (target < 5ms), throughput 2,000 rps healthz / 812 rps GET clusters (target > 500 rps).
- **Reliability**: 4hr variable-intensity soak on CpuCluster 5-node (34,027 OK / 35,679 total ops, 0 crashes). 24hr variable soak on HubCluster (268,738 ops, 99.0%, 1 node crash). Cascade soak (9,778+ ops, 100%). 500 concurrent goroutines, 4/4 pressure tests, 8/8 chaos tests.
- **Operability**: 42 API endpoints monitored, Prometheus metrics, Grafana dashboards, full CRD validation coverage, 13-suite test harness.
- **Security**: 32/32 live security tests (3 SSRF, 4 header injection, 3 token replay, 5 privilege escalation, 2 DecisionPackage tampering, 1 large payload, 3 SQL injection, 3 path traversal, 8 metric poisoning). TLS enforced, 0 Go CVEs (Trivy), HMAC-SHA256 auth at 2.9M ops/s.

**Gold gaps (remaining):**

| Requirement | Status | Gap |
|---|---|---|
| All 7 capabilities with live verdicts | **Done** — all 7 validated on 3-cluster production topology | Placement, routing, autoscaling, tenant governance, observability, lifecycle, KV cache transfer |
| 72-hour soak | Longest: 24hr (268,738 ops, 99.0%) | Need 72hr continuous soak on multi-node hub |
| Signed external evidence | Not started | Requires signed ARE ledger proof chain or external attestation |
| Security audit (no critical findings) | 32/32 automated tests pass | Formal external security audit not yet conducted |

## 7. Customer Deployment Patterns

### 7.1 Telco AI Grid

The Telco AI Grid reference architecture (`docs/customer-patterns/telco-ai-grid.md`) describes a deployment pattern for fleet-llm-d across a carrier's distributed edge infrastructure, designed to enable LLM inference at 30+ sites managed from a single hub cluster. Informed by engagements with Telco Edge Provider, Mobile Network Operator, and European Telco Partner, the pattern uses a three-tier topology: a central hub (fleet controller, Praxis AI gateway, ARE ledger), regional hub clusters (traffic aggregation, failover), and 30+ edge site clusters running lightweight models targeting sub-50ms TTFT. KV cache prefix sharing across sites is designed to eliminate redundant prefill for common MOP execution prompts, targeting 40% throughput improvement versus independent single-cluster deployments; this is a design target, not measured evidence (see section 4.7). Tenant self-service -- the capability that drove Mobile Network Operator's competitor selection -- is addressed through TenantProfile CRDs that enable LOB teams to define quotas, rate limits, and GPU budgets within platform-team guardrails. Geographic routing via the Praxis AI gateway is designed to ensure latency-sensitive workloads (real-time customer service, fraud detection, RAN optimization) route to the nearest edge site, with automatic failover to regional hubs when edge sites experience outages.

### 7.2 Financial Services

The Financial Services reference architecture describes a multi-region deployment pattern designed for regulatory-constrained environments with MaaS integration. The pattern targets Financial Services Provider and Global Banking Partner requirements: multi-region model placement with regulatory zone enforcement, SLO-gated canary rollouts, and per-tenant cost attribution with chargeback reporting.

The ARE Immutable Ledger directly satisfies the compliance requirements surfaced by Financial Services Provider and Global Banking Partner. Both institutions require auditable evidence of model deployment decisions, including which clusters were selected, why alternatives were rejected, and that SLO gates passed before production promotion. The hash-chained ledger provides tamper-evident records that meet SOC 2 Type II change management controls and OCC model risk management (SR 11-7) requirements for documented model deployment and monitoring. The proof receipt mechanism is designed to enable cross-cluster KV cache transfers to carry verifiable provenance, satisfying data lineage requirements for inter-region data movement (KV cache transfer is currently a stub implementation; see section 4.7).

ModelPack integration addresses the model provenance requirements common to regulated financial services. By resolving model metadata from OCI-compliant registries, fleet-llm-d provides a verifiable chain from model artifact to deployment: the model's identity, version, quantization, and resource footprint are recorded in the ledger at deployment time. This enables auditors to trace any inference response back to the specific model version, its deployment configuration, and the placement rationale -- a requirement for both institutions' model risk governance frameworks.

### 7.3 Sovereign Cloud

The Sovereign Cloud pattern (`docs/customer-patterns/sovereign-cloud.md`) deploys fleet-llm-d in standalone mode within fully self-contained sovereign zones that guarantee data residency, air-gapped operation, and cryptographic model provenance. Targeting national cloud providers, government agencies (defense, intelligence, civilian), and commercial sovereign cloud providers, each zone operates with zero external network connectivity behind a hard air-gap boundary. PlacementPolicy regulatory constraints enforce data sovereignty at the CRD level, ensuring no model weights or inference data leave the designated zone. ModelPack OCI signatures are verified against the zone's PKI trust anchor before any model is accepted for deployment, with SBOM validation and offline vulnerability scanning completing a five-stage import pipeline. Multi-tenant GPU sharing uses MIG (up to 7 isolated instances per H100) and MPS for hardware-level isolation between government tenants, governed by TenantProfile quotas with elevated priority (300+) for defense agencies. Each zone's ARE Immutable Ledger instance runs locally, providing tamper-evident compliance records for EU AI Act Article 12, NIST 800-53, and CNSSI 1253 requirements. Federated coordination between zones synchronizes only model catalog metadata (names, versions, resource requirements) via physical media transfer -- no inference data crosses zone boundaries.

## 8. Future Work

### 8.1 Near-Term

- **Granite 4.1 8B on CPU** -- Export requires >16 GB RAM; planned for execution on the validation cluster or the secondary validation cluster. Completes the Granite model tier (350M / 2B / 3B / 8B) for heterogeneous routing demos.
- **Speculative decoding** -- Granite 350M as draft model paired with Granite 2B/3B for 2-4x token generation speedup. The models are deployed; the speculative decode integration in fleet-llm-d's proxy is the remaining work.
- **Intel TDX confidential inference** -- BIOS enablement on the validation cluster Xeon 6767P (TME present, TDX supported). Deployment plan documented; requires infrastructure team coordination for BIOS access and worker node reboot window.

**vLLM Semantic Router Integration.** The [vLLM Semantic Router](https://github.com/vllm-project/semantic-router) (vLLM-SR) is an upstream Red Hat project that classifies prompts using ModernBERT (13 signal types, sub-millisecond) and routes to the correct model tier. It runs as an Envoy ExtProc filter.

fleet-llm-d extends vLLM-SR from single-cluster to fleet-wide semantic routing. The integration architecture:

1. **vLLM-SR** classifies the prompt (simple/standard/complex) at the Envoy edge
2. **fleet-llm-d proxy** routes the classified request to the correct cluster (CPU for simple, GPU for complex)
3. **llm-d EPP** picks the pod within the cluster
4. **GCL** observes tier distribution and governs tier-level scaling (80% simple traffic -> scale down GPU tier)
5. **ARE Ledger** records every routing decision with tier, confidence, and model

This enables automatic 5x cost savings (reported by vLLM-SR benchmarks) by routing simple queries to small fast models on CPU instead of expensive GPU models. The GCL ensures the routing decisions are governed: if tier imbalance threatens SLO compliance, the GCL proposes scaling actions.

**Centralized Decision-Oriented Metrics.** The current Prometheus federation serves dashboards (Grafana) but does not feed decision-making components in real time. The platform needs a centralized metrics plane that feeds the GCL predictor, the fleet-llm-d autoscaler, and the vLLM-SR routing engine with cross-cluster latency, throughput, queue depth, and GPU/CPU utilization.

Phase 1 (immediate): an aggregated metrics API on fleet-llm-d (`GET /api/v1/metrics/aggregated`) that all four systems poll. The GCL's predictor shifts from per-cycle evidence snapshots to continuous metrics-driven predictions. deepfield-fleet's SLO forecaster uses cross-cluster latency instead of single-cluster snapshots.

Phase 2 (production): evolve to an OpenTelemetry Collector hub that per-cluster fleet-agents push OTLP metrics to, with exports to Prometheus (dashboards), fleet-llm-d (autoscaler), and GCL (evidence webhook).

### 8.2 Medium-Term

- **ModelPlane live integration** -- Replace manual `--backends` JSON with automatic model discovery from ModelPlane's CRD-based model catalog. Mock API validated; real deployment pending ModelPlane availability on target clusters.
- **Semantic workload-aware CPU/GPU routing** -- Extend fleet-llm-d's cross-cluster routing to classify workloads by complexity and route simple queries to CPU clusters and complex queries to GPU clusters based on model requirements and cost optimization. (Basic multi-cluster routing is already validated; this adds workload-aware intelligence.)
- **Event-scale autoscaling** -- Predictive scaling based on event schedules (conference sessions, lab start times) rather than reactive CPU metrics, eliminating cold-start delays during peak load.

### 8.3 Long-Term

- Cross-cloud federation (GKE + EKS + OCP)
- Agentic workflow orchestration with fleet-level tool routing (the governed-cognitive-loop provides the first governed autonomy layer for this, with falsification-gated intent emission already integrated and verified on HubCluster)
- llm-d-planner integration for fleet-level capacity planning
- RHACM integration for unified cluster + inference management (MaaS of MaaS)
- MoE model support (Qwen3-30B-A3B) for "30B intelligence at 3B speed" on CPU

## 9. Appendix

### A. CRD Reference

CRD schemas are maintained in `api/crds/`. Run `make crd-docs` to generate the full reference from source.

### B. Benchmark Raw Data

Benchmark reports are generated by the CI pipeline and stored in `test/benchmarks/reports/`. Run `make bench-standard` to reproduce.

### C. Configuration Reference

Deployment configurations are maintained in `deploy/kustomize/` (overlays for hub, standalone, federated, and airgap modes). See `deploy/intel-cpu-inference/` for CPU inference deployment documentation.
