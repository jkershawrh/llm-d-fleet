# llm-d-fleet architecture

> **Current architecture.** This overview describes the `v0.3.0` OSS
> responsibility boundary. Praxis is the validated routing provider and llm-d
> Router is the upstream-native beta; neither is part of the fleet core.

## Three-Layer Stack

The diagram below shows the measured Praxis deployment. Praxis is one routing
adapter, not part of the fleet core. The upstream-native alternative is a
cluster-scoped llm-d Router/EPP consuming the same fleet-qualified provider
set. KServe may own cluster-local `LLMInferenceService` lifecycle, and KEDA
remains the default local scaling primitive.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Client Request                                   │
│                   POST /v1/chat/completions                             │
└──────────────────────────┬──────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  LAYER 3: llm-d-fleet (Operations Control Plane)                        │
│                                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │  Placement   │  │  Lifecycle   │  │   Tenant     │                   │
│  │  Solver      │  │  Rollouts    │  │  Governance  │                   │
│  └──────┬───────┘  └──────────────┘  └──────────────┘                   │
│         │                                                               │
│  ┌──────┴───────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │  Autoscaling │  │ Observability│  │  Semantic    │                   │
│  │  Optimizer   │  │ Federation   │  │  Router      │                   │
│  └──────────────┘  └──────────────┘  └──────┬───────┘                   │
│                                             │ classifies prompt         │
│  ┌──────────────┐  ┌──────────────┐         │ → selects tier            │
│  │  PostgreSQL  │  │  KV Cache    │         │                           │
│  │  Persistence │  │  Coordinator │         │                           │
│  └──────────────┘  └──────────────┘         │                           │
│                                             │                           │
│  Go control plane │ CRD-driven │ single dep (lib/pq)                    │
└──────────────────────────┬──────────────────────────────────────────────┘
                           │ routing decision
                           ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  LAYER 2: Selected routing provider                                     │
│                                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │ Praxis       │  │ llm-d Router │  │ Request ID   │                   │
│  │ (validated)  │  │   (beta)     │  │ propagation │                   │
│  └──────────────┘  └──────────────┘  └──────────────┘                   │
│                                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                   │
│  │ Qualified    │  │ Adapter-owned│  │ Verified TLS │                   │
│  │ provider set │  │ translation │  │ and identity │                   │
│  └──────────────┘  └──────────────┘  └──────────────┘                   │
│                                                                         │
│  One authoritative provider │ no client-selected internal routing       │
└───────┬─────────────────────┬─────────────────────┬─────────────────────┘
        │                     │                     │
        ▼                     ▼                     ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  Cluster A    │   │  Cluster B    │   │  Cluster N    │
│               │   │               │   │               │
│  ┌─────────┐  │   │  ┌─────────┐  │   │  ┌─────────┐  │
│  │ llm-d   │  │   │  │ llm-d   │  │   │  │ llm-d   │  │
│  │EPP+KEDA │  │   │  │EPP+KEDA │  │   │  │EPP+KEDA │  │
│  └────┬────┘  │   │  └────┬────┘  │   │  └────┬────┘  │
│       │       │   │       │       │   │       │       │
│  ┌────▼────┐  │   │  ┌────▼────┐  │   │  ┌────▼────┐  │
│  │  OVMS   │  │   │  │  vLLM   │  │   │  │  OVMS   │  │
│  │  (CPU)  │  │   │  │  (GPU)  │  │   │  │  (CPU)  │  │
│  │ Xeon 6  │  │   │  │ H100    │  │   │  │ Gaudi3  │  │
│  └─────────┘  │   │  └─────────┘  │   │  └─────────┘  │
└───────┬───────┘   └───────┬───────┘   └───────┬───────┘
        │                   │                   │
        └───────────────────┼───────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────────────────┐
│  OPTIONAL: KV-transfer transport (not required by fleet routing)         │
│                                                                         │
│  ┌─────────────-─┐  ┌──────────────┐  ┌──────────────┐                  │
│  │  TCP          │  │  RDMA/RoCE   │  │  OFI         │                  │
│  │  (CPU↔CPU)    │  │  (GPU↔GPU)   │  │  (Gaudi3)    │                  │
│  │  ✓ Implemented│  │  Stubbed     │  │  Planned     │                  │
│  └────────────-──┘  └──────────────┘  └──────────────┘                  │
│                                                                         │
│  KV cache transfer │ prefix sharing │ live migration without cold start │
└─────────────────────────────────────────────────────────────────────────┘
```

## Optional Governed-Observability Event Flow

This flow is an optional deployment profile. DeepField, GCL, and the immutable
ledger are not dependencies of core fleet eligibility or routing.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  DeepField  │     │     GCL     │     │ fleet-llm-d │     │ ARE Ledger  │
│  (Observe)  │     │  (Govern)   │     │   (Act)     │     │  (Prove)    │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │                   │
       │  1. Observe       │                   │                   │
       │  ─────────────    │                   │                   │
       │  Monitor fleet    │                   │                   │
       │  metrics, detect  │                   │                   │
       │  anomalies,       │                   │                   │
       │  forecast SLO     │                   │                   │
       │  breaches         │                   │                   │
       │                   │                   │                   │
       │  Advisory         │                   │                   │
       │  CloudEvent       │                   │                   │
       ├──────────────────►│                   │                   │
       │                   │                   │                   │
       │                   │  2. Govern        │                   │
       │                   │  ─────────────    │                   │
       │                   │  Classify         │                   │
       │                   │  constraints,     │                   │
       │                   │  predict horizon, │                   │
       │                   │  interpret via    │                   │
       │                   │  LLM, compute     │                   │
       │                   │  action           │                   │
       │                   │  deterministically│                   │
       │                   │                   │                   │
       │                   │  Falsification    │                   │
       │                   │  gate (7 checks)  │                   │
       │                   │                   │                   │
       │                   │  Signed           │                   │
       │                   │  DecisionPackage  │                   │
       │                   ├──────────────────►│                   │
       │                   │                   │                   │
       │                   │                   │  3. Act           │
       │                   │                   │  ─────────────    │
       │                   │                   │  Verify signature │
       │                   │                   │  Check expiry     │
       │                   │                   │  Check scope      │
       │                   │                   │  Apply fleet      │
       │                   │                   │  authorization    │
       │                   │                   │                   │
       │                   │                   │  Execute:         │
       │                   │                   │  • Place model    │
       │                   │                   │  • Scale replicas │
       │                   │                   │  • Route traffic  │
       │                   │                   │  • Migrate KV     │
       │                   │                   │                   │
       │                   │                   │  Record to ledger │
       │                   │                   ├──────────────────►│
       │                   │                   │                   │
       │                   │                   │                   │  4. Prove
       │                   │                   │                   │  ─────────
       │                   │                   │                   │  Hash-chain
       │                   │                   │                   │  entry,
       │                   │                   │                   │  return
       │                   │                   │                   │  proof
       │                   │                   │  Proof receipt    │  receipt
       │                   │                   │◄─────────────────┤
       │                   │                   │                   │
       ▼                   ▼                   ▼                   ▼
```

## Ecosystem Pipeline Detail

### Stage 1: Observe (DeepField)

```
Raw Signals ──► Nanoagents ──► Microagents ──► Macroagents
                (rules,        (rules +        (LLM-backed,
                 zero cost)     optional LLM)   multi-signal)
                    │               │               │
                    └───────────────┴───────────────┘
                                    │
                              Advisory CloudEvent
                           (advisory_only: true)
                                    │
                                    ▼
                            Classification:
                            • SLO breach forecast
                            • Blast radius scope
                            • Anomaly type
```

### Stage 2: Govern (GCL)

```
Advisory CloudEvent
        │
        ▼
┌─ Constraint Classification ──► Deterministic rules first
│                                  LLM only for ambiguous
│
├─ Horizon Prediction ──────────► Metric trajectories + confidence
│
├─ Objective Interpretation ────► LLM frames situation as objective
│                                  (never computes action)
│
├─ Deterministic Optimization ──► Numpy controller computes action:
│                                  scale, pre-warm, shed, migrate,
│                                  rollback, alert
│
├─ Falsification Gate ──────────► 7 checks challenge the proposal:
│                                  capacity? magnitude? warmup?
│                                  confidence? compliance? bounds?
│                                  target available?
│
├─ OPA Honesty Boundary ────────► Guardian sidecar blocks action
│                                  fields from LLM responses
│
└─ Signed DecisionPackage ──────► Ed25519 signed
                                   Expiry-bounded
                                   Scope-bound (tenant + zone)
                                   Full evidence chain attached
```

### Stage 3: Act (fleet-llm-d)

```
Signed DecisionPackage
        │
        ▼
┌─ Admission ───────────────────► Verify GCL producer signature
│                                  Check expiry (reject if stale)
│                                  Check scope binding
│
├─ Authorization ───────────────► Apply fleet-owned policy
│                                  (fleet-llm-d decides independently)
│
├─ FleetOperation Created ──────► RECEIVED → ACCEPTED → PLANNED
│                                  → AUTHORIZED → ACTUATING
│
├─ Actuation ───────────────────► Placement: solver + scorer
│                                  Scaling: optimizer + collector
│                                  Routing: Praxis gateway update
│                                  KV Transfer: ConnectLink
│                                  Lifecycle: canary/blue-green
│
├─ Outcome ─────────────────────► SUCCEEDED or FAILED
│
└─ Record ──────────────────────► Every operation → ARE Ledger
                                   (proof receipt, not authorization)
```

### Stage 4: Prove (ARE Immutable Ledger)

```
Fleet Operation Record
        │
        ▼
┌─ Hash-Chain Entry ────────────► entry_type, content, correlation_id
│                                  agent_id, source_id, timestamp
│                                  SHA-256 hash linking to previous
│
├─ Chain Verification ──────────► Any entry type chain can be verified
│                                  Detect tampering or gaps
│
├─ Correlation Reconstruction ──► Query correlation_id to rebuild
│                                  full decision chain:
│                                  classify → predict → interpret →
│                                  plan → falsify → propose/reject
│
└─ Proof Receipt ───────────────► Content-addressed verification
                                   Receipt proves entry was recorded
                                   Receipt is NOT a credential
                                   Receipt NEVER authorizes action
```

## Inference Routing Flow

```
Client Request (model="auto")
        │
        ▼
┌─ Semantic Router ─────────────► Classify prompt complexity
│   (fleet-llm-d)                  simple │ standard │ complex
│                                         │          │
│                              ┌──────────┘          │
│                              ▼                     ▼
│                         CPU Tier              GPU Tier
│                         (OVMS)                (vLLM)
│                         $0.60/hr              $12-32/hr
│                              │                     │
├─ Placement Solver ────────►  Which cluster has capacity?
│                              Data residency ok?
│                              GPU/CPU match?
│                                    │
├─ Praxis AI Gateway ──────►  Route to selected backend
│                              Count tokens
│                              Log access
│                                    │
├─ llm-d EPP ──────────────►  Pick specific pod within cluster
│                                    │
└─ Inference Backend ──────►  OVMS (CPU) or vLLM (GPU)
                                     │
                              Response to client
```

## CRD Model

```
┌─────────────────────────────────────────────────────────┐
│                    Fleet CRDs                           │
│                                                         │
│  FleetCluster         ── registered cluster identity    │
│  FleetInferencePool   ── model replicas across clusters │
│  FleetIntent          ── desired state declaration      │
│  FleetOperation       ── tracked mutation lifecycle     │
│  PlacementPolicy      ── where models may run           │
│  FleetRoutingPolicy   ── how traffic is distributed     │
│  FleetScalingPolicy   ── autoscaling targets/bounds     │
│  TenantProfile        ── quotas, metering, priority     │
│  ModelLifecycle        ── rollout strategy + gates      │
│  KVCacheTransferPolicy ── transfer triggers + transport │
└─────────────────────────────────────────────────────────┘
```

## Historical reference topology

> This sanitized snapshot predates `v0.3.0` and used NodePort bridges. It is
> retained to explain earlier measurements, not as current transport guidance
> or production certification. Portable deployments supply their own verified
> transport and topology.

```
┌─────────────────────────────────────────┐
│  HubCluster SNO -- Hub (Intel Xeon, 256 CPU)│
│                                         │
│  fleet-llm-d namespace:                 │
│    fleet-controller (Go + PostgreSQL)   │
│    praxis-ai (gateway, 6 model routing) │
│    deepfield-fleet                      │
│    fleet-grafana                        │
│                                         │
│  triforce namespace:                    │
│    ovms-granite-2b (real inference)     │
│    + 5 more OVMS models                 │
│                                         │
│  immutable-ledger namespace:            │
│    ledger (Rust gRPC core)              │
│    ledger-gateway (Python REST)         │
│    postgres (ledger DB)                 │
│                                         │
│  governed-cognitive-loop namespace:     │
│    gcl-app (signed DecisionPackages)    │
│                                         │
│  Network policies:                      │
│    default-deny ingress                 │
│    open egress (OVN-K limitation)       │
└──────────┬──────────────────┬───────────┘
           │ NodePort bridge  │ NodePort bridge
           ▼                  ▼
┌──────────────────────┐ ┌─────────────────────────────┐
│  CpuCluster -- CPU Spoke  │ │  GpuCluster -- GPU Spoke        │
│  (Xeon 6 + AMX, 2TB)│ │  (H100 NVL 94GB, SNO)      │
│                      │ │                             │
│  triforce namespace: │ │  inference namespace:       │
│    ovms-granite-2b   │ │    vllm (GPU inference)     │
│    (CPU inference)   │ │    H100 NVL 94GB            │
│                      │ │                             │
│  Role: CPU inference │ │  Role: GPU inference spoke  │
│  spoke, OVMS         │ │  GPU provider cluster  │
└──────────────────────┘ └─────────────────────────────┘

Cross-cluster routing: Praxis AI on HubCluster routes to
CpuCluster and GpuCluster via NodePort bridges (pre-Grid).
```
