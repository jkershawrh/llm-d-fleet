# llm-d-fleet + Praxis migration record

**Status:** Draft  
**Author:** James Kershaw, AI Field Engineering  
**Audience:** Office of the Chief AI Architect, Red Hat  
**Date:** July 2026

> Current-state note (v0.3.0): this document contains the original phased
> migration plan for historical context. Phase 1 is complete, the standalone
> `fleet-gateway` crate has been removed, the Go controller hosts the supported
> OpenAI-compatible ingress, and accepted requests are forwarded to external
> Praxis. Any earlier fallback or A/B instructions involving `fleet-gateway`
> are superseded by the August update near the end of this document.
> It is not the portable OSS architecture or the current product boundary;
> see `product-boundary.md` for those normative definitions.

---

## Executive Summary

fleet-llm-d is Red Hat's fleet-level inference orchestration platform, providing
multi-cluster placement, routing, autoscaling, tenant isolation, governed
autonomy, and tamper-evident compliance for large-scale Model-as-a-Service
(MaaS) deployments. The current project ships 12 CRDs, a Go controller with an
OpenAI-compatible admission and inference surface, Rust agent and KV-transfer
components, and an external Praxis integration across heterogeneous
infrastructure -- from Intel Xeon CPU clusters running OVMS to GPU-equipped
nodes running vLLM. The sections below preserve the migration rationale that
led to this architecture.

Praxis fills that gap. Praxis AI is a composable, filter-pipeline Rust proxy
purpose-built for AI workloads: it speaks OpenAI, Anthropic, and emerging
agentic protocols (MCP, A2A) natively, applies prompt-level guardrails and token
accounting inline, and injects credentials per-tenant. Praxis Grid adds a
multi-site control plane with SWIM-protocol gossip membership, CRDT state
propagation, mTLS, and metrics-driven scoring -- enabling peer-to-peer site
discovery that complements RHACM's hub-spoke topology. ConnectLink extends this
architecture downward into the accelerator fabric, providing NIXL-based
GPU-to-GPU KV cache transfer with TCP fallback for CPU-only environments.

The original combined design paired fleet-level operations control with
Praxis and accelerator-native cache transfer, then demonstrated optional
DeepField, GCL, and immutable-evidence integrations. Those integrations are
not OSS-core prerequisites and are outside the initial upstream llm-d
acceptance criteria. Current packaging keeps them independently deployable;
the governed-evidence profile enables them explicitly.

---

## 1. Current Architecture

> **Note:** fleet-gateway has been replaced by Praxis AI. The diagram below shows the pre-Praxis architecture for reference.

```
                          Ecosystem Pipeline
    +-----------+    +-----+    +-----------+    +--------+
    | DeepField |--->| GCL |--->| fleet-llm-d |--->| ARE    |
    | (observe) |    |(gov)|    |   (act)     |    |(prove) |
    +-----------+    +-----+    +------+------+    +--------+
                                       |
                    +-----------------+|+-----------------+
                    |                  ||                  |
              +-----v------+    +-----vv-----+    +-------v----+
              |  fleet-     |    |  fleet-     |    |  fleet-    |
              |  controller |    |  gateway    |    |  agent     |
              |  (Go)       |    |  (Rust/axum)|    |  (Rust)    |
              +------+------+    +------+------+    +-----+------+
                     |                  |                  |
              +------v------+          |           +------v------+
              | PostgreSQL  |          |           |  Cluster    |
              | (state)     |          |           |  Backends   |
              +-------------+          |           +------+------+
                                       |                  |
                                +------v------------------v------+
                                |          Inference Backends     |
                                |  OVMS (OpenVINO) | vLLM (CPU)  |
                                +--------------------------------+

    CRDs (10):  FleetCluster, FleetInferencePool, FleetIntent,
                FleetOperation, FleetRoutingPolicy, FleetScalingPolicy,
                KVCacheTransferPolicy, ModelLifecycle, PlacementPolicy,
                TenantProfile

    gRPC services (7):  fleet, placement, routing, tenant,
                        kvcache, lifecycle, observability
```

**What works today:**

- Go control plane: placement solver/scorer, routing policies, autoscaling,
  tenant metering/quota, model lifecycle (canary/blue-green), intent
  evaluation with GCL governance, ledger integration
- Rust data plane: fleet-gateway was an axum reverse proxy with weighted/latency/cost
  balancing, health probing, and cluster discovery (now replaced by Praxis AI); fleet-agent per cluster
- KV transfer coordinator with TransferProtocol trait, gRPC transport, and
  NIXL bridge (stubbed)
- Full ecosystem pipeline: DeepField observations feed GCL decisions feed
  fleet-llm-d actuation feed ARE ledger proofs

**What is missing:**

| Gap                              | Impact                                              |
|----------------------------------|------------------------------------------------------|
| No AI protocol translation       | Clients must speak raw OpenAI; no Anthropic, no MCP  |
| No agentic routing               | Cannot route MCP tool calls or A2A agent messages    |
| No prompt-level guardrails       | Guardrails require separate sidecar deployment       |
| No token accounting at proxy     | Metering depends on backend-reported usage only      |
| No credential injection          | Each tenant manages its own API key distribution     |
| No peer-to-peer site discovery   | Gateway relies on control plane polling, not gossip  |
| No CRDT state propagation        | Config changes require full reconciliation loop      |
| NIXL bridge is stubbed           | KV cache transfer limited to in-process streaming    |
| No multi-fabric transport        | No RDMA, RoCE, or OFI paths for accelerator xfer    |

---

## 2. Target Architecture

```
    +-----------+    +-----+    +-----------+    +--------+
    | DeepField |--->| GCL |--->| fleet-llm-d |--->| ARE    |
    | (observe) |    |(gov)|    |   (act)     |    |(prove) |
    +-----------+    +-----+    +------+------+    +--------+
                                       |
    ===================================|============================
     LAYER 3: Operations Control Plane |  (fleet-llm-d, Go)
    ===================================|============================
              |                        |                   |
        +-----v------+          +-----v------+      +-----v------+
        | fleet-      |          | Placement  |      | Tenant     |
        | controller  |          | Solver     |      | Metering   |
        +------+------+          +-----+------+      +-----+------+
               |                       |                    |
               |  ConfigMap overlays   |  token reports     |
               |  (routing weights,    |  (per-request      |
               |   model placement)    |   accounting)      |
               v                       v                    v
    ===================================================================
     LAYER 2: Programmable AI Data Plane  (Praxis AI + Grid, Rust)
    ===================================================================
        |                    |                    |                |
  +-----v------+    +-------v------+    +--------v-----+   +------v-----+
  | Praxis AI  |    | Praxis AI    |    | Praxis Grid  |   | Praxis Grid|
  | Gateway    |    | Filter       |    | SWIM Mesh    |   | CRDT State |
  | (protocol  |    | Pipeline     |    | (site disc-  |   | (config    |
  |  xlation)  |    | (guardrails, |    |  overy,      |   |  propag-   |
  |            |    |  cred inject,|    |  health,     |   |  ation)    |
  |  OpenAI <->|    |  enrichment) |    |  scoring)    |   |            |
  |  Anthropic |    |              |    |              |   |            |
  |  MCP / A2A |    |              |    |              |   |            |
  +-----+------+    +------+-------+    +-------+------+   +-----+------+
        |                  |                    |                 |
        +------------------+--------------------+-----------------+
                                    |
    ===================================================================
     LAYER 1: Accelerator Fabric  (ConnectLink + NIXL)
    ===================================================================
        |                    |                    |
  +-----v------+    +-------v------+    +--------v---------+
  | TCP         |    | RDMA / RoCE  |    | OFI (libfabric)  |
  | (CPU-to-CPU |    | (GPU-to-GPU  |    | (Gaudi3-to-Gaudi3|
  |  fallback)  |    |  H100/A100)  |    |  Intel accel.)   |
  +------+------+    +------+-------+    +--------+---------+
         |                  |                     |
  +------v------------------v---------------------v----------+
  |                   Inference Backends                      |
  |  OVMS (OpenVINO/Xeon) | vLLM CPU | vLLM GPU | Gaudi3    |
  +---------------------------------------------------------+
```

**Key design properties:**

1. The ecosystem pipeline (DeepField -> GCL -> fleet-llm-d -> ARE) is unchanged.
   Praxis operates below fleet-llm-d's control plane, never above it.
2. fleet-llm-d retains all CRDs and the Go control plane. Praxis consumes
   placement and routing decisions as ConfigMap overlays -- it does not make
   fleet-level decisions.
3. ConnectLink's transport selection (TCP vs RDMA vs OFI) is automatic based
   on available hardware. CPU-only clusters use TCP; GPU clusters use
   RDMA/RoCE; Gaudi3 clusters use OFI/libfabric.
4. Each layer is independently deployable and testable. Praxis AI can run
   without Grid (single-site). ConnectLink can run without Praxis (direct
   fleet-gateway).

---

## 3. What Praxis Brings

| Capability             | Current fleet-gateway            | Praxis AI + Grid                           |
|------------------------|----------------------------------|--------------------------------------------|
| **Protocol support**   | Raw HTTP reverse proxy           | OpenAI, Anthropic, MCP, A2A native         |
| **Site discovery**     | Control plane polling (10s)      | SWIM gossip (sub-second convergence)        |
| **Security**           | mTLS (manual cert management)    | mTLS with automatic rotation, per-filter    |
| **Agentic routing**    | None                             | MCP tool routing, A2A agent dispatch        |
| **Prompt guardrails**  | None (requires external sidecar) | Inline filter pipeline (regex, PII, toxicity)|
| **Token accounting**   | None (backend-reported only)     | Per-request token counting at proxy layer   |
| **Credential inject**  | None                             | Per-tenant API key injection via filters    |
| **Observability**      | Prometheus metrics + traces      | Prometheus + OpenTelemetry + per-filter span|
| **Extensibility**      | Requires Rust code changes       | Composable filter pipeline (WASM planned)   |
| **Config propagation** | Full reconciliation loop         | CRDT eventual consistency (< 200ms)         |
| **Load balancing**     | Weighted, latency, cost          | Metrics-driven scoring + all fleet-gateway  |
| **Maturity**           | Production-tested (fleet scope)  | Alpha (single-site proven, multi-site beta) |
| **Language**           | Rust (axum)                      | Rust (composable filter framework)          |
| **License**            | Apache 2.0                       | MIT (praxis core), Apache 2.0 (AI/Grid)    |

---

## 4. Integration Points

### 4.1 Placement Decisions --> Praxis Grid Routing Overlays

fleet-llm-d's placement solver (`pkg/placement/solver/`, `pkg/placement/scorer/`)
produces per-model cluster assignments stored in `PlacementPolicy` CRDs. Today
the fleet-gateway reads these via control plane polling. With Praxis Grid, the
fleet-controller renders placement decisions as ConfigMap overlays that Praxis
Grid's overlay renderer consumes directly.

```
fleet-controller                      Praxis Grid
      |                                     |
      |  1. PlacementPolicy CRD updated     |
      |------------------------------------->|
      |  2. Render ConfigMap overlay:        |
      |     routing-overlay-granite-8b.yaml  |
      |     { weights: {cpucluster: 0.7,          |
      |                  hubcluster: 0.3},       |
      |       model: granite-8b }            |
      |------------------------------------->|
      |  3. Grid propagates via CRDT         |
      |     to all site gateways             |
      |                                     |
```

The ConfigMap overlay format is a Praxis Grid primitive. fleet-llm-d generates
overlays from its existing `FleetRoutingPolicy` and `PlacementPolicy` CRDs
through a new adapter in `pkg/routing/praxis_overlay.go`. The overlay includes
cluster weights, model affinity, tenant overrides, and failover targets.

### 4.2 Praxis AI Token Accounting --> fleet-llm-d Tenant Metering

Praxis AI's filter pipeline includes a token accounting filter that counts
prompt and completion tokens per request. Today fleet-llm-d's tenant metering
(`pkg/tenant/metering/`) relies on backend-reported usage, which arrives
asynchronously and may be incomplete for streaming responses.

With Praxis AI, token counts are captured at the proxy layer and reported to
fleet-llm-d's metering service via a lightweight gRPC stream:

```
Praxis AI Gateway                    fleet-llm-d
      |                                     |
      |  1. Request completes               |
      |  2. Filter extracts:                |
      |     - tenant_id (from header)       |
      |     - model_id                      |
      |     - prompt_tokens: 1,247          |
      |     - completion_tokens: 892        |
      |     - latency_ms: 340               |
      |------------------------------------->|
      |  3. fleet-llm-d updates             |
      |     TenantProfile.status.usage      |
      |     and quota enforcement            |
      |                                     |
```

This uses the existing `tenant.proto` gRPC service, extended with a
`ReportTokenUsage` RPC. The Praxis AI filter emits usage records that map
directly to fleet-llm-d's `TenantProfile` CRD usage tracking.

### 4.3 Praxis Grid SWIM Membership --> fleet-llm-d Cluster Registry

fleet-llm-d maintains a cluster registry via `FleetCluster` CRDs. Today the
fleet-agent on each cluster reports health via gRPC to the fleet-controller.
Praxis Grid's SWIM protocol provides faster, decentralized membership with
failure detection in O(log N) probe rounds.

The integration adds a `SWIMSyncAdapter` in `pkg/cluster/client/` that
subscribes to Praxis Grid's membership events and reconciles them with
`FleetCluster` CRD status:

- **Member join:** Create or update `FleetCluster` with `status.connected: true`
- **Member suspect:** Set `FleetCluster` `status.health: Degraded`
- **Member leave/fail:** Set `FleetCluster` `status.connected: false`, trigger
  placement re-evaluation
- **Metadata updates:** Sync capacity, GPU count, model inventory from SWIM
  metadata tags

fleet-llm-d remains the source of truth for cluster registration (SWIM does not
create clusters). SWIM provides faster health signal; the existing fleet-agent
gRPC health check continues as a secondary confirmation path.

### 4.4 ConnectLink Transfer --> fleet-llm-d KV Transfer Coordinator

fleet-llm-d's `crates/kv-transfer/` already defines the `TransferProtocol`
trait with `connect()`, `send_blocks()`, `receive_blocks()`, and `close()`
methods, along with a `TransferCoordinator` managing job lifecycle
(Pending -> InProgress -> Completed/Failed/Cancelled).

ConnectLink implements `TransferProtocol` with three concrete backends:

| Backend           | Transport    | Use Case                        | Hardware Required     |
|-------------------|--------------|---------------------------------|-----------------------|
| `TcpTransfer`     | TCP sockets  | CPU-to-CPU KV cache transfer    | Any (Xeon, ARM, etc.) |
| `RdmaTransfer`    | RDMA / RoCE  | GPU-to-GPU via NIXL             | NVIDIA H100/A100      |
| `OfiTransfer`     | OFI/libfabric| Gaudi3-to-Gaudi3 transfer       | Intel Gaudi3          |

The existing `NixlBridge` in `crates/kv-transfer/src/nixl_bridge.rs` becomes
one of three ConnectLink backends. The `KVCacheTransferPolicy` CRD's
`spec.transport.protocol` field selects the backend:

```yaml
apiVersion: fleet.llm-d.ai/v1alpha1
kind: KVCacheTransferPolicy
metadata:
  name: cpucluster-to-hubcluster
spec:
  transport:
    protocol: auto          # auto | tcp | rdma | ofi
    maxBandwidthMbps: 10000
  source:
    clusterRef: cpucluster
  target:
    clusterRef: hubcluster
```

When `protocol: auto`, the coordinator probes both endpoints and selects the
highest-performance available transport.

### 4.5 fleet-llm-d CRDs --> Praxis Grid CRDs Mapping

| fleet-llm-d CRD         | Praxis Grid Equivalent     | Sync Direction        | Notes                                    |
|--------------------------|----------------------------|-----------------------|------------------------------------------|
| `FleetCluster`           | `GridSite`                 | fleet -> Grid         | fleet-llm-d creates; Grid discovers      |
| `FleetRoutingPolicy`     | `RouteOverlay`             | fleet -> Grid         | Rendered as ConfigMap overlays            |
| `FleetInferencePool`     | `BackendGroup`             | fleet -> Grid         | Model-to-backend mapping                 |
| `TenantProfile`          | `TenantPolicy`             | fleet -> Grid         | Quota and priority propagation            |
| `PlacementPolicy`        | `RouteOverlay` (weights)   | fleet -> Grid         | Placement weights become routing weights  |
| `FleetScalingPolicy`     | (no equivalent)            | fleet only            | Scaling remains fleet-llm-d concern       |
| `FleetIntent`            | (no equivalent)            | fleet only            | Governance remains fleet-llm-d concern    |
| `FleetOperation`         | (no equivalent)            | fleet only            | Operations remain fleet-llm-d concern     |
| `ModelLifecycle`          | (no equivalent)            | fleet only            | Rollouts remain fleet-llm-d concern       |
| `KVCacheTransferPolicy`  | (no equivalent)            | fleet only            | Transfer policies remain fleet-llm-d      |

Sync is unidirectional: fleet-llm-d is the authoritative control plane. Praxis
Grid consumes translated CRDs as read-only configuration. A
`PraxisCRDTranslator` controller in `pkg/routing/` watches fleet-llm-d CRDs
and renders the corresponding Praxis Grid resources.

---

## 5. What Changes, What Stays

| Component                | Current                          | Target                                 | Change Type |
|--------------------------|----------------------------------|----------------------------------------|-------------|
| fleet-controller (Go)    | Placement, routing, scaling      | Same + Praxis overlay rendering        | **Enhance** |
| fleet-gateway (Rust)     | axum reverse proxy               | Praxis AI gateway (replaces)           | **Replace** |
| fleet-agent (Rust)       | Per-cluster health + sync        | Same + SWIM sidecar integration        | **Enhance** |
| PostgreSQL               | State persistence                | Same                                   | **Keep**    |
| Prometheus + Grafana     | Metrics + dashboards             | Same + Praxis filter metrics           | **Enhance** |
| CRDs (10)                | Fleet state model                | Same (Praxis CRDs are derived)         | **Keep**    |
| gRPC services (7)        | Control plane APIs               | Same + ReportTokenUsage RPC            | **Enhance** |
| KV transfer coordinator  | In-process streaming backend     | ConnectLink multi-transport backends   | **Enhance** |
| NIXL bridge              | Stubbed                          | ConnectLink RDMA backend               | **Enhance** |
| Praxis AI gateway        | (does not exist)                 | Programmable AI proxy with filters     | **Add**     |
| Praxis Grid              | (does not exist)                 | SWIM mesh + CRDT state propagation     | **Add**     |
| ConnectLink TCP          | (does not exist)                 | CPU-to-CPU KV cache transfer           | **Add**     |
| ConnectLink OFI          | (does not exist)                 | Gaudi3 accelerator fabric              | **Add**     |
| DeepField integration    | CloudEvent observations          | Same                                   | **Keep**    |
| GCL integration          | DecisionPackage intents          | Same                                   | **Keep**    |
| ARE ledger integration   | Tamper-evident proofs            | Same                                   | **Keep**    |
| fleetctl CLI             | Operator tooling                 | Same + Praxis status commands          | **Enhance** |
| Web dashboard (Next.js)  | Fleet observability UI           | Same + Praxis filter/mesh panels       | **Enhance** |

---

## 6. CPU and Heterogeneous Inference Compatibility

Praxis operates at the HTTP/streaming protocol layer. It has no dependency on
GPU hardware, CUDA, or any accelerator SDK. Every Praxis filter -- protocol
translation, guardrails, token accounting, credential injection -- processes
HTTP request/response bodies and headers. This makes Praxis fully compatible
with any inference backend that exposes an OpenAI-compatible HTTP endpoint.

### Supported Backend Matrix

| Backend                  | Hardware           | Protocol        | Praxis Compatible | ConnectLink Transport |
|--------------------------|--------------------|-----------------|--------------------|-----------------------|
| OVMS (OpenVINO)          | Intel Xeon CPU     | OpenAI HTTP     | Yes                | TCP                   |
| vLLM CPU mode            | Any x86_64 CPU     | OpenAI HTTP     | Yes                | TCP                   |
| vLLM GPU mode            | NVIDIA H100/A100   | OpenAI HTTP     | Yes                | RDMA / RoCE           |
| vLLM Gaudi3              | Intel Gaudi3       | OpenAI HTTP     | Yes                | OFI / libfabric       |
| llm-d (KServe)           | GPU or CPU         | OpenAI HTTP     | Yes                | Depends on hardware   |
| Any OpenAI-compatible    | Any                | OpenAI HTTP     | Yes                | TCP (default)         |

### ConnectLink Transport Selection

ConnectLink selects the optimal transport based on hardware availability at both
endpoints of a KV cache transfer:

```
Source Cluster          Target Cluster         Transport Selected
-----------------------------------------------------------------
Xeon CPU (OVMS)    -->  Xeon CPU (OVMS)        TCP sockets
Xeon CPU (vLLM)    -->  H100 GPU (vLLM)        TCP (no GPU at source)
H100 GPU (vLLM)    -->  H100 GPU (vLLM)        RDMA / RoCE via NIXL
Gaudi3 (vLLM)      -->  Gaudi3 (vLLM)          OFI / libfabric
H100 GPU           -->  Gaudi3                  TCP (heterogeneous fallback)
```

**Design principle:** ConnectLink always falls back to TCP when the
highest-performance transport is unavailable. A CPU-only fleet gets full KV
cache transfer capability over TCP with no additional hardware requirements.
RDMA and OFI paths are additive optimizations, not prerequisites.

This is a deliberate architectural choice: Red Hat customers start with CPU
inference on commodity Xeon hardware and add GPU/Gaudi3 capacity
incrementally. The architecture must never require GPU hardware to function.

---

## 7. Phased Roadmap

### Phase 1: Praxis AI Gateway (Weeks 1-6) -- COMPLETE

**Objective:** Replace fleet-gateway with Praxis AI for single-site inference
routing, proving compatibility with OVMS CPU backends. **Status: Complete.** fleet-gateway Rust crate has been removed; Praxis AI is the production inference data plane.

| Deliverable                                   | Owner          | Dependencies        |
|-----------------------------------------------|----------------|---------------------|
| Deploy Praxis AI alongside fleet-gateway      | Data Plane     | Praxis AI alpha     |
| Configure OpenAI protocol filter              | Data Plane     | --                  |
| Integrate fleet-controller overlay rendering  | Control Plane  | Praxis ConfigMap API|
| Implement ReportTokenUsage gRPC RPC           | Control Plane  | tenant.proto update |
| Token accounting filter -> fleet-llm-d meter  | Data Plane     | ReportTokenUsage    |
| Credential injection filter for tenants       | Data Plane     | TenantProfile CRDs  |
| A/B traffic split: fleet-gateway vs Praxis AI | Data Plane     | --                  |
| Soak test: 72h on CpuCluster cluster w/ OVMS       | QE             | CpuCluster access         |

**Success criteria:**
- Praxis AI routes 100% of inference traffic to OVMS backends with no
  regressions in latency p50/p99
- Token accounting matches backend-reported usage within 2% tolerance
- Credential injection works for 3+ tenant profiles
- Historical success criterion: keep fleet-gateway as a canary fallback during
  migration. This criterion is superseded; the crate has since been removed.

### Phase 2: Praxis Grid Multi-Site Mesh (Weeks 7-14)

**Objective:** Deploy Praxis Grid SWIM mesh across two sites, proving
sub-second failure detection and CRDT config propagation.

| Deliverable                                   | Owner          | Dependencies        |
|-----------------------------------------------|----------------|---------------------|
| Deploy Praxis Grid on CpuCluster + HubCluster clusters | Infra          | Phase 1 complete    |
| SWIM membership bootstrap (2-site mesh)       | Data Plane     | Praxis Grid beta    |
| SWIMSyncAdapter in pkg/cluster/client/        | Control Plane  | SWIM event API      |
| CRDT propagation of routing overlays          | Data Plane     | Phase 1 overlays    |
| mTLS between Grid sites (auto-rotation)       | Security       | cert-manager        |
| PraxisCRDTranslator controller                | Control Plane  | CRD mapping spec    |
| Failover test: kill CpuCluster, verify HubCluster      | QE             | 2-site mesh         |
| Metrics-driven site scoring integration       | Observability  | Prometheus fed.     |

**Success criteria:**
- SWIM detects site failure in < 2 seconds
- CRDT config propagation completes in < 200ms for 2-site mesh
- FleetCluster status reflects SWIM membership within one reconciliation cycle
- mTLS rotation completes without traffic interruption

### Phase 3: ConnectLink KV Cache Transfer (Weeks 9-20)

**Objective:** Implement ConnectLink TCP transport for CPU-to-CPU KV cache
transfer, extending to RDMA when GPU hardware is available.

| Deliverable                                   | Owner          | Dependencies         |
|-----------------------------------------------|----------------|----------------------|
| ConnectLink TcpTransfer implementing TransferProtocol | Data Plane | kv-transfer trait  |
| Integration with TransferCoordinator lifecycle| Data Plane     | TcpTransfer          |
| KVCacheTransferPolicy `protocol: auto` logic  | Control Plane  | Hardware probing     |
| TCP soak test: warm migration, CpuCluster cluster  | QE             | TcpTransfer          |
| ConnectLink RdmaTransfer (NIXL bridge real)   | Data Plane     | NIXL SDK, GPU nodes  |
| OfiTransfer stub for Gaudi3 path              | Data Plane     | OFI/libfabric        |
| Prefix tree sync over TCP between 2 clusters  | Data Plane     | TcpTransfer          |
| Benchmark: TCP vs RDMA transfer throughput    | QE             | GPU test cluster     |

**Success criteria:**
- TCP KV cache transfer completes warm migration for Granite-8B in < 30s
  between CpuCluster clusters
- TransferCoordinator lifecycle (Pending -> InProgress -> Completed) works
  for TCP backend
- `protocol: auto` correctly selects TCP on CPU-only clusters
- Hot failover completes in < 5s for active KV cache

### Phase 4: Agentic Routing + Production Hardening (Weeks 13-24)

**Objective:** Enable MCP/A2A agentic routing through Praxis AI, integrate
Anthropic protocol support, and harden for production MaaS.

| Deliverable                                   | Owner          | Dependencies         |
|-----------------------------------------------|----------------|----------------------|
| MCP tool routing filter in Praxis AI          | Data Plane     | Phase 1 gateway      |
| A2A agent dispatch filter                     | Data Plane     | MCP filter           |
| Anthropic protocol translation filter         | Data Plane     | Phase 1 gateway      |
| Prompt enrichment filter (RAG context inject) | Data Plane     | --                   |
| Multi-provider credential vault integration   | Security       | HashiCorp Vault      |
| 3-site mesh (CpuCluster + HubCluster + customer site)  | Infra          | Phase 2 mesh         |
| End-to-end governance: GCL -> Praxis -> ARE   | Control Plane  | Phases 1-3           |
| Production hardening: rate limiting, circuit  | Data Plane     | --                   |
|   breaker, retry budget per filter            |                |                      |
| Load test: 10K concurrent requests, 5 models  | QE             | Full stack            |
| Security audit: Praxis filter pipeline        | Security       | Phase 4 filters      |

**Success criteria:**
- MCP tool calls route through Praxis AI to correct backend with < 10ms
  overhead
- Anthropic-format requests translate to OpenAI backend calls transparently
- 3-site mesh maintains consistency under network partition (2-of-3 quorum)
- Full pipeline soak: 168h continuous operation with no data loss

---

## 8. RHACM + RHOAI Integration

### Layered Responsibility Model

```
+------------------------------------------------------------------+
|  RHACM (Red Hat Advanced Cluster Management)                      |
|  - Cluster lifecycle (provision, upgrade, decommission)           |
|  - Hub-spoke topology management                                  |
|  - Policy-based cluster governance                                |
|  - Cluster registration and identity                              |
+------------------------------------------------------------------+
         |                                |
         | Cluster inventory              | Policy constraints
         v                                v
+------------------------------------------------------------------+
|  fleet-llm-d (Fleet Inference Operations)                         |
|  - Model placement across RHACM-managed clusters                  |
|  - Inference routing policies                                     |
|  - Tenant isolation and metering                                  |
|  - Autoscaling and model lifecycle                                |
|  - Governed autonomy (GCL intents)                                |
|  - Tamper-evident compliance (ARE ledger)                          |
+------------------------------------------------------------------+
         |                                |
         | ConfigMap overlays             | Token reports
         v                                v
+------------------------------------------------------------------+
|  Praxis AI + Grid (Programmable AI Data Plane)                    |
|  - Protocol translation (OpenAI, Anthropic, MCP, A2A)            |
|  - SWIM mesh (peer-to-peer, complements RHACM hub-spoke)         |
|  - Inline guardrails, credential injection                        |
|  - CRDT config propagation                                        |
+------------------------------------------------------------------+
         |
         | Inference requests
         v
+------------------------------------------------------------------+
|  RHOAI (Red Hat OpenShift AI) per cluster                         |
|  - KServe model serving                                           |
|  - llm-d within-cluster inference routing                         |
|  - Model runtime (vLLM, OVMS, Caikit)                            |
|  - GPU operator, Node Feature Discovery                           |
+------------------------------------------------------------------+
```

### How the layers interact:

**RHACM manages cluster lifecycle; fleet-llm-d manages inference operations.**
RHACM provisions and upgrades OpenShift clusters. fleet-llm-d discovers these
clusters (via `FleetCluster` CRDs or RHACM's ManagedCluster resources) and
decides which models run where. RHACM does not make inference-level decisions;
fleet-llm-d does not provision clusters.

**RHOAI provides within-cluster KServe + llm-d; fleet-llm-d + Praxis provides
the fleet layer.** Each RHACM-managed cluster runs RHOAI with KServe for model
serving and llm-d for within-cluster request routing (prefix-aware scheduling,
KV cache management). fleet-llm-d orchestrates across these clusters -- placing
models, balancing load, managing tenants. Praxis provides the programmable
network path between the fleet layer and the per-cluster RHOAI endpoints.

**Praxis Grid's SWIM mesh complements RHACM's hub-spoke with peer-to-peer
routing.** RHACM uses a hub-spoke topology: the hub cluster manages spoke
clusters. This is correct for cluster lifecycle but introduces a bottleneck for
real-time inference routing. Praxis Grid's SWIM gossip mesh enables spoke-to-spoke
communication for latency-sensitive operations (request forwarding, KV cache
transfer coordination, health propagation) without routing through the hub.

```
         RHACM Hub-Spoke                    Praxis Grid SWIM Mesh
         (cluster lifecycle)                (inference routing)

              Hub                           CpuCluster <---> HubCluster
             / | \                            |    \   /    |
            /  |  \                           |     \ /     |
         Spoke Spoke Spoke                 Site-C  Site-D  Site-E
```

Both topologies coexist. RHACM hub-spoke handles cluster provisioning, policy
distribution, and observability aggregation. Praxis SWIM handles real-time
health gossip, config propagation, and request forwarding between inference
sites. fleet-llm-d sits above both, consuming RHACM cluster inventory and
Praxis health signals to make placement and routing decisions.

---

## 9. Competitive Differentiation

### Capability Comparison

| Capability                     | fleet-llm-d + Praxis       | NVAIE + Rafay              | Anyscale (Ray Serve)       |
|--------------------------------|----------------------------|----------------------------|----------------------------|
| Fleet orchestration            | 10 CRDs, multi-cluster     | NIM + manual deploy        | Ray cluster scoped         |
| AI protocol gateway            | Praxis AI (composable)     | Triton only                | Ray Serve only             |
| Governed autonomy              | GCL signed intents         | None                       | None                       |
| Tamper-evident compliance      | ARE immutable ledger       | None                       | None                       |
| Multi-site mesh                | Praxis Grid SWIM + CRDT   | Hub-spoke only             | Single cluster             |
| KV cache transfer              | ConnectLink (TCP/RDMA/OFI) | NIXL (GPU-only)            | Ray object store            |
| Agentic routing (MCP/A2A)      | Praxis AI filters          | None                       | None                       |
| Hardware support               | Xeon CPU, H100, Gaudi3     | NVIDIA GPU only            | NVIDIA GPU primarily       |
| Cluster management integration | RHACM native               | Proprietary                | None                       |
| Model serving integration      | RHOAI/KServe native        | NIM proprietary            | Ray Serve proprietary      |
| License                        | Apache 2.0 + MIT           | Proprietary                | Proprietary (BSL)          |

### Unique Differentiators

**Only platform with the full governance stack.** No competitor combines fleet
orchestration + governed autonomy (GCL) + tamper-evident compliance (ARE) +
programmable AI gateway (Praxis) + multi-fabric accelerator transfer
(ConnectLink). Each piece is open source and independently valuable; together
they form a compliance-ready MaaS platform that regulated industries require.

**Hardware-agnostic by design.** NVIDIA's stack (NVAIE, NIM, NIXL) requires
NVIDIA GPUs at every layer. fleet-llm-d + Praxis runs on Intel Xeon CPUs today
(OVMS + OpenVINO), adds NVIDIA GPU support via standard vLLM, and extends to
Intel Gaudi3 via OFI -- all without architectural changes. Customers are not
locked to a single silicon vendor.

**Open source with enterprise backing.** The entire stack is open source:
fleet-llm-d (Apache 2.0), Praxis (MIT + Apache 2.0), ConnectLink (Apache 2.0),
NIXL (Apache 2.0). Red Hat provides the enterprise distribution, support, and
integration with RHACM + RHOAI. Customers avoid proprietary lock-in while
getting production-grade support.

**Peer-to-peer at the inference layer.** RHACM's hub-spoke topology is correct
for cluster lifecycle but wrong for real-time inference routing. Praxis Grid's
SWIM mesh provides sub-second peer-to-peer health detection and config
propagation between inference sites -- a capability that neither NVAIE nor
Anyscale offers.

---

## 10. Risks and Mitigations

| Risk                                         | Likelihood | Impact   | Mitigation                                                                                          |
|----------------------------------------------|------------|----------|------------------------------------------------------------------------------------------------------|
| **Praxis maturity (alpha)**                  | High       | Medium   | Keep fleet-gateway as fallback during Phase 1-2. Canary deployment with A/B traffic split. Praxis AI is single-binary Rust with no runtime dependencies beyond what fleet-gateway already requires. |
| **ConnectLink hardware dependency**          | Medium     | Low      | TCP mode requires no special hardware. RDMA and OFI paths are additive. CPU-only environments lose transfer speed but not functionality. |
| **SWIM mesh complexity at scale**            | Medium     | Medium   | Start with 2-site mesh (CpuCluster + HubCluster). SWIM is proven at scale (HashiCorp Consul, Serf). Praxis Grid's implementation is based on the same protocol with tuned probe intervals. |
| **CRD translation drift**                    | Medium     | Medium   | PraxisCRDTranslator controller is auto-generated from CRD schemas. Contract tests verify translation correctness on every CI run. |
| **Token accounting accuracy**                | Low        | High     | Dual-path validation: Praxis proxy-level counts vs backend-reported usage. Discrepancies > 2% trigger alerts. Metering for billing uses the lower of the two values (conservative). |
| **mTLS certificate management**              | Low        | High     | Praxis Grid uses cert-manager with automatic rotation. Fallback to manual cert injection if cert-manager is unavailable. Rotation tested in Phase 2 soak. |
| **CRDT conflict resolution**                 | Low        | Medium   | fleet-llm-d is the single authoritative writer. Praxis Grid CRDTs are read-only replicas. Last-writer-wins with fleet-llm-d's wall clock as tiebreaker eliminates split-brain. |
| **Performance overhead of filter pipeline**  | Low        | Medium   | Praxis filters are zero-copy where possible (Rust ownership model). Benchmark target: < 1ms added latency for the full filter chain. Phase 1 soak validates this. |
| **Ecosystem pipeline disruption**            | Very Low   | Critical | DeepField, GCL, and ARE integrations are at Layer 3 (fleet-llm-d control plane). Praxis operates at Layer 2 (data plane). The ecosystem pipeline has no dependency on Praxis and cannot be disrupted by Praxis failures. |

### Fallback Strategy

If Praxis integration encounters blocking issues at any phase, the fallback
path is clear:

- **Phase 1 blocked (historical):** The original fallback was to continue with
  fleet-gateway. This option is no longer available because Phase 1 completed
  and the crate was removed.
- **Phase 2 blocked:** Continue with control-plane-polled cluster discovery.
  SWIM mesh is an optimization, not a prerequisite.
- **Phase 3 blocked:** Continue with in-process streaming transfer backend.
  TCP transport can be added to fleet-gateway's kv-transfer crate directly.
- **Phase 4 blocked:** MCP/A2A routing can be added as fleet-gateway middleware.
  Protocol translation can be a standalone sidecar.

**Update (August 2026):** Phase 1 is complete. fleet-gateway has been removed
and Praxis AI is the production data plane. The Phase 1 fallback to
fleet-gateway is no longer applicable. Phases 2-4 fallback strategies remain
valid for their respective scopes.

No phase creates an irreversible dependency. Each phase delivers standalone
value and can be paused or rolled back independently.

---

*This document describes a target architecture. Implementation timelines are
estimates subject to resource availability, hardware access, and upstream
Praxis release cadence. The phased approach ensures that each integration
delivers measurable value before the next phase begins.*
