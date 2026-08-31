# llm-d-fleet


**Fleet-level inference orchestration for [llm-d](https://github.com/llm-d) — qualifying exact-model capacity across clusters and handing the eligible provider set to a supported routing data plane.**

llm-d-fleet is a data-plane-neutral operations control plane for enterprise AI inference fleets. It owns cluster registration, capability inventory, exact-model resolution, tenant admission, placement constraints, provider health, draining, failure-domain status, and eligible-provider reconciliation. Praxis is the validated routing adapter for the current release; the llm-d Router adapter is the upstream-native beta. KServe, llm-d Router/EPP, KEDA, and optionally WVA retain cluster-local serving, endpoint selection, and pod-scaling ownership. DeepField, GCL, the immutable ledger, llm-d-sc, ModelPack, and ModelPlane are optional integrations rather than OSS-core prerequisites.

## Distribution and deployment profiles

This repository develops one OSS codebase. Scale, high availability, and
governed evidence are additive deployment profiles—not separate editions or
forks:

- **OSS core** is the portable Apache-2.0 control plane, agent, APIs, CLI,
  community manifests, tests, and routing-provider contracts. Optional
  ecosystem services are absent and disabled by default.
- **Routing adapters** add either Praxis (validated default) or llm-d Router
  (upstream-native beta) without changing fleet eligibility policy.
- **Production scale/HA** adds stateless gateway and routing replicas, external
  durable state, topology controls, secure transport, capacity guardrails, and
  failure certification. These capabilities remain OSS and portable.
- **Governed evidence** optionally adds authenticated GCL proposals, an
  external immutable ledger, DeepField observations, and related integrations.
  It is not required by OSS core or scale/HA.
- **Environment overlays and evidence** contain physical-cluster names,
  endpoints, image pins, and raw qualification results. They validate the OSS
  capabilities but are not part of the portable source release.

See [deployment profiles](docs/community/deployment-profiles.md) and the
[community release boundary](docs/community/release-boundary.md).
The [documentation index](docs/README.md) identifies authoritative current
material and retained historical references.

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev/)
[![Rust](https://img.shields.io/badge/Rust-1.90+-DEA584.svg)](https://www.rust-lang.org/)
[![CI](https://github.com/jkershawrh/llm-d-fleet/actions/workflows/ci.yaml/badge.svg)](https://github.com/jkershawrh/llm-d-fleet/actions/workflows/ci.yaml)
[![Security](https://github.com/jkershawrh/llm-d-fleet/actions/workflows/security.yaml/badge.svg)](https://github.com/jkershawrh/llm-d-fleet/actions/workflows/security.yaml)

---

> **Maturity notice (August 2026):** The portable OSS core has automated
> product-conformance coverage for a hub, two CPU failure domains, and an
> exact-model GPU provider. Praxis is the validated reference adapter and the
> llm-d Router adapter remains beta. Environment certification, credentials,
> physical topology, and operational evidence are maintained outside this
> public source tree.

See the [OSS and downstream production boundary](docs/community/repository-boundary.md)
before adding deployment or certification material.

## Why llm-d-fleet

llm-d and KServe own cluster-local serving and scheduling. Enterprises operating across independent failure domains still need authenticated capability discovery, exact-model eligibility, fleet policy, and failure-domain health before the Router scores a destination. llm-d-fleet fills that coordination boundary without replacing KServe, EPP, KEDA, WVA, or the selected cross-cluster data plane.

## Architecture

```
  Layer 3: llm-d-fleet (Operations Control Plane)
                         ┌─────────────────────────────────┐
                         │        fleet-controller         │
                         │  (Go control plane, CRD-driven) │
                         │  placement | routing | scaling  │
                         │  lifecycle | tenant | kvcache   │
                         │  PostgreSQL persistence         │
                         └──────────┬──────────┬───────────┘
                                    │          │
  Layer 2: Routing adapter          │          │
                         ┌──────────▼──────────▼───────────┐
                         │ Praxis Grid or llm-d Router     │
                         │ qualified clusters -> EPP score │
                         └──┬──────────┬──────────┬────────┘
                            │          │          │
  Layer 1: Inference        │          │          │
                   ┌────────▼┐ ┌──────▼──┐ ┌────▼────┐
                   │Cluster A│ │Cluster B│ │Cluster N│
                   │ agent   │ │ agent   │ │ agent   │
                   │ llm-d   │ │ llm-d   │ │ llm-d   │
                   │OVMS/vLLM│ │OVMS/vLLM│ │OVMS/vLLM│
                   └─────────┘ └─────────┘ └─────────┘

  Binaries:  fleet-controller, fleetctl (Go)
             fleet-agent, kv-transfer (Rust)
  Data Plane: Praxis (validated) or llm-d Router (upstream-native beta)
  Dashboard: Next.js (TypeScript)
```

## Seven Capabilities

| # | Capability | Description | Status |
|---|-----------|-------------|--------|
| 1 | **Model Placement** | Solver and scorer assign models to clusters based on GPU topology, locality, and policy constraints. | Soak-proven |
| 2 | **Cross-Cluster Eligibility** | Produces an exact-model, policy-qualified provider set for the selected routing adapter. | Praxis soak-proven; Router beta |
| 3 | **Fleet Capacity Policy** | Sets fleet budgets, placement bounds, and migration policy while KEDA/HPA performs local scaling; WVA is optional for heterogeneous variants. | Soak-proven |
| 4 | **Multi-Cluster Observability** | Unified metrics pipeline aggregates per-cluster Prometheus data into fleet-wide Grafana dashboards. | Soak-proven |
| 5 | **Tenant Governance** | Metering and quota enforcement give platform teams per-tenant controls over GPU-hours and throughput. | Soak-proven |
| 6 | **Lifecycle Management** | Rollout controller orchestrates canary/blue-green model version upgrades with SLO gates. | Soak-proven |
| 7 | **KV Cache State Transfer** | Transfers KV cache state between clusters during migration, rescheduling, or failover to minimize cold-start latency. | Contract/unit evidence |

### Custom Resource Definitions

fleet-llm-d ships 12 CRDs. Ten are fleet-owned resources and two (`GridSite`
and `InferenceProvider`) are the Praxis Grid integration contract:

| CRD | Purpose |
|-----|---------|
| `FleetInferencePool` | Defines a fleet-wide pool of model replicas spanning multiple clusters. |
| `PlacementPolicy` | Constrains where models may be placed (topology, region, compliance). |
| `FleetRoutingPolicy` | Configures cross-cluster routing rules, weights, and failover. |
| `TenantProfile` | Declares tenant quotas, metering rules, and priority classes. |
| `FleetScalingPolicy` | Sets autoscaling targets, thresholds, and per-cluster bounds. |
| `ModelLifecycle` | Specifies rollout strategy and production gate criteria. |
| `KVCacheTransferPolicy` | Governs when and how KV cache state is migrated between clusters. |
| `FleetCluster` | Records cluster identity, capacity, health, and lifecycle state. |
| `FleetIntent` | Stores admitted desired changes submitted through the intent APIs. |
| `FleetOperation` | Tracks authorization, actuation, observation, and terminal outcome. |
| `GridSite` | Represents a Praxis Grid site generated from fleet cluster state. |
| `InferenceProvider` | Represents a routable model provider generated for Praxis Grid. |

## Integrations

### Routing providers

Exactly one routing adapter is authoritative per deployment. Select it with
`--routing-provider`, `FLEET_ROUTING_PROVIDER`, or Helm
`controller.routingProvider`: `praxis` (default), `llm-d-router`, or
`disabled`. Adapters receive the same fleet-qualified provider set and cannot
add an incompatible cluster or rewrite the exact physical model.

Routing providers consume a platform-neutral endpoint contract containing the
routing and metrics URLs, TLS server identity, trust reference, authentication
reference, health freshness, failure domain, and exact physical model.
OpenShift re-encrypt Routes are the validated transport for the current Red
Hat deployment, but they are not required by the OSS core. Gateway API is the
preferred portable Kubernetes target; MCS, service meshes, and external
gateways can publish the same contract without changing fleet policy.

### Praxis AI Gateway

[Praxis AI](https://github.com/praxis-proxy/ai) is the validated reference data plane for the current physical deployment. Its `GridSite` and `InferenceProvider` resources are emitted by the Praxis adapter without changing their existing contract. It provides:

- **Model-based routing**: Routes requests to the correct backend based on the model name in the request
- **Token counting**: Tracks prompt and completion tokens per request for metering
- **Access logging**: Structured logs for every inference request
- **Protocol translation**: OpenAI, Anthropic, MCP, and A2A protocol support (roadmap)

Praxis Grid extends this to multi-site with SWIM membership discovery, CRDT state propagation, and mTLS between sites. ConnectLink + NIXL provides GPU-to-GPU KV cache transfer (RDMA/RoCE for GPU, TCP for CPU, OFI for Gaudi3).

See [`docs/architecture/praxis-integration.md`](docs/architecture/praxis-integration.md) for the full integration architecture.

### llm-d Router and KServe

The upstream-native adapter writes one deterministic watched endpoint file per
exact physical model for llm-d Router's `multicluster-file-discovery` plugin.
It removes stale, draining, unavailable, unauthorized, and incompatible
providers before EPP scoring. Queue, KV, prefix, session, and final endpoint
selection remain Router/EPP responsibilities. `FleetInferencePool` also
supports the additive `kserveLLMInferenceService` serving target; KServe then
owns model workload, revision, readiness, draining, Gateway, and local Router
lifecycle. The existing `inferencePool` target remains the default.

The optional `grid-signals-publisher` converts a cluster-local Prometheus or
active provider-health source into a small, pool-level contract over strict
mTLS. It discards source labels and never exports pod, container, instance, or
rank identity. Missing queue/KV signals reduce scoring quality but do not
invent load data or make an otherwise qualified provider incompatible.

### ModelPack (CNCF model-spec)

fleet-llm-d consumes [ModelPack](https://github.com/model-spec) artifacts -- OCI-packaged models with structured metadata -- as its canonical model format. The `modelpack` package resolves model references, validates signatures, and extracts hardware requirements used by the placement solver.

### Standalone Immutable Ledger

[are-immutable-ledger](https://github.com/jkershawrh/are-immutable-ledger) is the
independent audit spine for this ecosystem. It stores hash-chained fleet, GCL,
and DeepField evidence and issues portable proof receipts. Receipts prove that
an entry was recorded; they are not credentials and never authorize a fleet
mutation. The ledger-owned `are.ledger.v1.ImmutableLedgerService` gRPC contract
is canonical. `pkg/ledger` also implements the repository's optional `/api/*`
REST gateway for explicit compatibility/development deployments.
The controller fails startup if `grpc` is selected today; it will advertise
that mode only after the ledger-owned protobuf is consumed through a generated,
pinned Go client. It never falls back to in-memory receipts for a configured
external-ledger failure.

### ModelPlane Integration

fleet-llm-d sits on top of [ModelPlane](https://github.com/modelplane) as the operations layer in a three-layer stack:

```
  ┌──────────────────────────────────────────┐
  │            fleet-llm-d                   │  Operations layer
  │  placement | routing | scaling | cost    │  (this project)
  │  tenant | lifecycle | observability      │
  ├──────────────────────────────────────────┤
  │            ModelPlane                    │  Infrastructure layer
  │  ModelDeployment | ModelCluster          │  (Crossplane-based)
  │  cluster lifecycle | resource mgmt       │
  ├──────────────────────────────────────────┤
  │            llm-d                         │  Inference layer
  │  EPP | WVA | KV cache | prefill/decode   │  (within-cluster)
  └──────────────────────────────────────────┘
```

The `modelplane` package (`pkg/modelplane/`) provides six integration points: CRD consumption (reading ModelDeployment and ModelCluster resources), policy injection (annotating ModelDeployments with fleet placement decisions), cost integration (feeding GPU pricing into fleet cost projections), compliance bridge (forwarding ModelPlane events to the standalone immutable ledger), routing integration (using ModelCluster health for traffic decisions), and scaling integration (coordinating fleet autoscaling with ModelPlane resource limits). Three API endpoints expose ModelPlane state: `/api/v1/modelplane/clusters`, `/api/v1/modelplane/deployments`, and `/api/v1/modelplane/cost/{deployment}`.

**Prototype evidence.** The checked-in demo used `cmd/modelplane-mock/` to
exercise the watcher and cost paths with CRD-shaped fixtures. That proves the
mock contract path only. It is not evidence of the pinned, official ModelPlane
provider, Gateway API ownership, or observed multi-cluster actuation.

An optional governed-observability ecosystem is `deepfield-fleet -> governed-cognitive-loop ->
fleet-llm-d -> are-immutable-ledger`: DeepField owns observations and
forecasts, GCL owns signed and falsified proposals, fleet owns admission,
authorization, desired/observed state, and actuation, and the ledger owns
tamper-evident evidence. None of DeepField, GCL, or the ledger is required by
the default community installation. The existing `--production` switch names
the governed-evidence production contract and therefore still requires signed
GCL admission and an authenticated external ledger. ModelPlane and llm-d remain infrastructure and
within-cluster inference providers below the fleet boundary.

### Governed Cognitive Loop

The [governed-cognitive-loop](https://github.com/jkershawrh/governed-cognitive-loop) sits above fleet-llm-d as the governed autonomy layer. It receives classifications from deepfield-fleet, derives constraints from evidence, optimizes under hard constraints, challenges every plan through a falsification gate, and sends typed intents to fleet-llm-d only when the action survives all checks.

fleet-llm-d evaluates received intents against its CRD-defined policies before actuating. The GCL governs the decision; fleet-llm-d governs the execution.

GCL submits signed, expiry-bounded `DecisionPackage` proposals to the v2 intent
boundary. A submission acknowledgement is not execution. Fleet admission and
approval policy determines whether an operation may actuate, while the
standalone immutable ledger records admission and outcome evidence without
granting authority.

Production v2 admission fails closed unless the request is a verified
`application/cloudevents+json` GCL DecisionPackage. The unsigned
`application/json` shape is self-asserted development/operator compatibility
only and is disabled by default. It can be enabled deliberately with
`--allow-operator-json-intents` or
`FLEET_ALLOW_OPERATOR_JSON_INTENTS=true`; Helm exposes the same switch as
`controller.allowOperatorJSONIntents` and defaults it to `false`.

### Cost Model

fleet-llm-d includes a full cost model (`pkg/cost/`) for GPU inference economics:

- **GPU Pricing** -- Pricing table covering 6 GPU types (A100-40GB, A100-80GB, H100-80GB, H200-141GB, B200-192GB, MI300X-192GB) across 3 tiers (on-demand, reserved, spot).
- **Tokenomics** -- Cost-per-million-tokens calculation per model, factoring GPU type, utilization, and throughput.
- **Chargeback** -- Per-tenant cost attribution reports for enterprise billing integration.
- **Budget Alerts** -- Configurable alert thresholds on tenant and fleet-wide GPU spend with projection-based early warning.

Six API endpoints: `/api/v1/cost/pricing`, `/api/v1/cost/tokenomics/{model}`, `/api/v1/cost/chargeback/{tenant}`, `/api/v1/cost/projection`, `/api/v1/cost/savings`, `/api/v1/cost/alerts`.

## Security

- **Authentication**: HMAC-SHA256 bearer tokens with role-based access (admin, operator, viewer, tenant)
- **Rate Limiting**: Per-IP and per-tenant token bucket middleware
- **TLS**: Optional HTTPS via `--tls-cert` and `--tls-key` flags
- **RBAC**: Least-privilege controller and agent roles plus fleet-viewer and fleet-tenant-admin roles
- **Network Policies**: Default-deny with explicit allowlists per component
- **Container Hardening**: UBI base images, non-root (UID 65534), read-only filesystem, drop ALL capabilities
- **Webhook Validation**: Admission webhook rejects invalid CRD specs
- **Audit Trail**: When an external immutable ledger is configured, governed
  mutations fail closed on recording errors; the OSS core does not claim that
  every authentication or RBAC event is durably recorded by default.

## Quick Start

### Community profile

```bash
kubectl apply -k api/crds
kubectl create namespace fleet-llm-d
kubectl -n fleet-llm-d create configmap fleet-cluster-identity \
  --from-literal=cluster-id=community-cluster-01
helm upgrade --install fleet charts/fleet-llm-d \
  --namespace fleet-llm-d \
  --values charts/fleet-llm-d/values-standalone-dev.yaml
```

This profile is for evaluation and development. See the
[community installation guide](docs/community/installation.md) before enabling
external ingress, durable state, a routing provider, or production controls.

### Local Development

```bash
# Prerequisites: Go 1.26+, Rust 1.90+, podman or docker

# Build binaries
make build-go          # → bin/fleet-controller, bin/fleetctl

# Start the controller (in-memory mode, no external deps)
./bin/fleet-controller --port 8080

# Register a cluster
./bin/fleetctl --server http://localhost:8080 clusters register \
  --id my-cluster --name "My Cluster" --region us-east

# View the test matrix
./bin/fleetctl matrix --format table
```

## Illustrative examples

Example CRDs demonstrate possible policy shapes. They are not certified
customer configurations or capacity recommendations:

| Pattern | Directory | Key Features |
|---|---|---|
| **Telco AI Grid** | [`examples/telco-edge/`](examples/telco-edge/) | 30+ edge sites, geographic routing, 50ms latency target |
| **Financial Services** | [`examples/financial-services/`](examples/financial-services/) | Regulatory data-residency and SLO-gated canary design |
| **Sovereign Cloud** | [`examples/sovereign-cloud/`](examples/sovereign-cloud/) | Air-gapped zones, GPU-as-a-Service multi-tenancy, scale-to-zero |

## Deployment Modes

| Mode | Description | Details |
|------|-------------|---------|
| **Hub** | RHACM-style hub managing spoke clusters; Kubernetes Lease election keeps one controller active while standby replicas remain live. | See [`deploy/kustomize/overlays/hub/`](deploy/kustomize/overlays/hub/) |
| **Standalone** | Single-node development/CI deployment with convenience dependencies; not a production default. | See [`deploy/kustomize/overlays/standalone/`](deploy/kustomize/overlays/standalone/) |
| **Federated** | Peer-to-peer mesh where multiple fleet-controllers coordinate as equals. | See [`deploy/kustomize/overlays/federated/`](deploy/kustomize/overlays/federated/) |

The [Kustomize deployment guide](deploy/kustomize/README.md) and
[Helm chart guide](charts/fleet-llm-d/README.md) document the controller
and agent port contracts, required cluster identity, disruption budgets,
and production-safe external dependency configuration.

## Dashboard

The llm-d-fleet dashboard is a Next.js application that renders the state and
optional integrations exposed by the controller. A page being present does
not imply that its external data source is configured.

**Pages:**

1. **Overview** -- Fleet health summary, aggregate GPU utilization, active model count.
2. **Clusters** -- Per-cluster status, capacity, and connectivity.
3. **Models** -- Model inventory, placement map, and version history.
4. **Tenants** -- Tenant quota usage, metering dashboards, and policy editor.
5. **Rollouts** -- Active and historical rollouts with production gate status.
6. **Compliance** -- ARE Ledger audit trail, compliance posture, and attestation records.
7. **Test Matrix** -- Cross-cluster test results, compatibility matrix, and gate progression.

## Testing

```bash
make test              # Run all tests
make test-unit         # Go and Rust unit tests
make test-bdd          # BDD scenarios
make test-contracts    # Contract tests (proto + OpenAPI validation)
make test-e2e          # End-to-end tests (requires running infrastructure)
make test-portable-conformance # Topology-neutral product contract
```

```bash
# Architecture proofs about how the system works
go test -tags=architecture ./test/architecture/...

# Security tests: auth, rate limiting, webhook validation
go test -tags=security ./test/security/...

# Soak test: sustained load for configurable duration
./test/soak/run-soak.sh --duration 7200 --rps 10
```

### Current product evidence

Architectural tests prove code-level invariants; they do not alone prove an
assembled deployment. `make test-portable-conformance` exercises the portable
product contract, and the disposable multi-cluster Kind workflow exercises
runtime discovery, placement, readiness, and inference.

The [current conformance report](docs/upstream/multicluster-product-conformance-2026-08-31.md)
records supported claims and deliberate non-claims. The
[reviewer evidence package](docs/upstream/reviewer-evidence-package.md) links
release provenance, limitations, and the independent clean-room protocol.
Long-duration, capacity, and governed-profile results are deployment-specific
and remain outside the portable release.

<details>
<summary>Historical test and deployment snapshots</summary>

The following results are retained for engineering traceability. They span
earlier software, topology, transport, and optional-ecosystem revisions and
must not be presented as `v0.3.0` product certification.

### Architectural proof snapshot

Architectural assertions are exercised by tests in `test/architecture/`.
These tests are design evidence; they do not by themselves prove assembled
runtime behavior:

| Category | Claims | Method | What's Proven |
|---|---|---|---|
| Reconciliation | 5 | EDD | Webhook → solver → phase transitions → events |
| Routing | 6 | TDD | Model selection, latency, failover, header injection |
| Tenant Governance | 5 | TDD | Quota enforcement, budget caps, multi-tenant isolation |
| Lifecycle | 5 | TDD | Canary, SLO gates, rollback |
| Autoscaling | 4 | TDD | Scale up/down, GPU cap, cross-cluster migration |
| Compliance | 7 | CDD | Every decision → ARE ledger, chain verification |
| Event Flow | 4 | EDD | Pub/sub + HTTP external delivery |
| Multi-Cluster | 3 | TDD | Cross-cluster routing, failover, multi-cluster placement |
| Security | 2 | TDD | Rate limiting, webhook validation |
| Cost Model | 4 | TDD | GPU pricing accuracy, tokenomics calculation, chargeback aggregation, budget alerts |
| ModelPlane | 5 | TDD | CRD consumption, policy injection, cost integration, compliance bridge, routing integration |

### Test Harness (Demo Cluster)

The historical demo harness recorded nine suites against one OpenShift demo
deployment. Those results remain useful regression data, but they are not the
required hub-plus-two-spoke release-candidate gate.

| Suite | Result | Highlights |
|-------|--------|------------|
| Smoke | 24/24 pass | All 16 endpoints healthy |
| Stress | Pass | Survived 500 concurrent goroutines, no breaking point |
| Pressure | 4/4 pass | Concurrent writes, race detection, rapid register/deregister 1000x |
| Chaos | 8/8 pass | 1MB body, invalid JSON, unicode, null bytes, burst 1000 |
| Red Team | 11/11 pass | Duplicate registration returns 409 Conflict |
| Latency | Pass | health p50=0.4ms, auth-reads p50=0.45ms, auth-writes p50=0.44ms |
| Throughput | Pass | healthz 2,000 rps, GET clusters 812 rps, POST clusters 2,000 rps |
| Soak | Pass | 30 min, 15,950 requests, 0 errors, 0.00% error rate |
| Security | Pass | TLS enforced, HTTP rejected, 0 Go CVEs (Trivy) |

**Go microbenchmarks:** Token generation 2.9M ops/s, token validation 2.0M ops/s, routing decision 19.5M ops/s.

See [`test/harness/`](test/harness/) for the reproducible harness source.

### Ecosystem Stress Testing

The optional governed profile can be exercised with deepfield-fleet, GCL,
fleet-llm-d, and an external immutable ledger. Environment-specific results
are intentionally not committed to the OSS repository.

| Phase | Result | Highlights |
|---|---|---|
| Pressure | 7/7 | 0 errors at 50 concurrent governance cycles, signal payloads up to 1,000 |
| Degradation | 10/10 | Fleet health p50=2ms under GCL load, all scenarios degrade gracefully |
| Soak | 6/6 | 300 cycles, 0 errors, 1.2x drift; mixed concurrent (60s): 479 requests, 0% error rate |
| Pen Testing | 5/5 | SQL injection, path traversal, malformed input all handled |

The [current OSS whitepaper](docs/whitepaper/llm-d-fleet-whitepaper.md)
describes the supported architecture and evidence boundary. Historical
governed-profile stress details remain in the clearly labeled extended
engineering record.

### Environment Certification

Long-duration, failure-injection, and capacity evidence belongs to the
downstream environment repository. Public releases contain the harness,
acceptance criteria, and topology-neutral product-conformance results—not
cluster addresses, credentials, deployment snapshots, or private telemetry.

Upstream and product reviewers can start with the
[`llm-d-fleet` reviewer evidence package](docs/upstream/reviewer-evidence-package.md),
which links the product boundary, portable conformance result, release
integrity evidence, limitations, and independent clean-room protocol.

### Resilience (On-Cluster)

6 resilience tests passed: fleet controller pod kill (9ms recovery), GCL pod kill (8ms), simultaneous kill (12ms/10ms), rapid restart 5x (avg 7ms), post-disruption soak (0% error rate).

### Observability and Security

- Prometheus text format metrics at `:9091/metrics` (8 metrics: counters, gauges, memory, goroutines)
- NetworkPolicies: default-deny with per-component allowlists deployed on all 3 clusters (HubCluster, CpuCluster, GpuCluster)
- Ecosystem test matrix: `test/matrix/ecosystem-matrix.yaml` (CDD/TDD/BDD/EDD/CBT/Soak rubric)

### Production Gate Model

| Stage | Gate | Criteria | Status |
|-------|------|----------|--------|
| 0 | **Red** | Interfaces defined and executable tests authored | Passed |
| 1 | **Yellow** | Unit, BDD, and contract tests pass | Passed |
| 2 | **Green** | Integration tests pass, capability soak on real cluster | Passed (HubCluster) |
| 3 | **Blue** | Real multi-cluster, performance, and chaos gates pass | In progress |
| 4 | **Gold** | All capabilities, 72-hour soak, signed external evidence, and no critical security findings | Not promoted |

See [`test/matrix/matrix.yaml`](test/matrix/matrix.yaml) and [`test/matrix/rubric.yaml`](test/matrix/rubric.yaml).

### Historical illustrative deployment patterns

| Pattern | Example Customers | Profile | Reference |
|---------|-------------------|---------|-----------|
| **Telco** | Telco Edge Provider, Mobile Network Operator | 30+ edge sites, latency-sensitive placement, distributed GPU pools. | [`docs/customer-patterns/telco-ai-grid.md`](docs/customer-patterns/telco-ai-grid.md) |
| **Sovereign** | Government, regulated industries | Air-gapped deployment, data residency enforcement, ARE Ledger integration. | [`docs/customer-patterns/sovereign-cloud.md`](docs/customer-patterns/sovereign-cloud.md) |

</details>

## Project Structure

```
fleet-llm-d/
├── api/
│   └── crds/                    # 12 CRD definitions
├── cmd/
│   ├── fleet-controller/        # Go control plane binary
│   ├── fleetctl/                # CLI tool
│   └── modelplane-mock/         # ModelPlane mock API server
├── pkg/
│   ├── placement/               # solver, scorer
│   ├── routing/                 # balancer, policy
│   ├── autoscaling/             # collector, optimizer
│   ├── lifecycle/               # rollout
│   ├── tenant/                  # metering, quota
│   ├── observability/           # metrics
│   ├── kvcache/                 # transfer
│   ├── modelpack/               # CNCF model-spec integration
│   ├── ledger/                  # ARE Ledger client
│   ├── modelplane/              # ModelPlane integration (adapter, watcher, policy injector)
│   ├── cost/                    # GPU pricing, tokenomics, chargeback, budget alerts
│   ├── store/                   # events, postgres
│   ├── cluster/                 # client
│   └── apis/                    # generated API types
├── crates/
│   ├── fleet-agent/             # Rust per-cluster agent
│   ├── fleet-common/            # Shared Rust types
│   ├── fleet-ledger/            # Rust ledger integration
│   └── kv-transfer/             # KV cache transfer engine
├── web/                         # Next.js (TypeScript) UI
├── deploy/
│   ├── kustomize/overlays/      # hub, standalone, federated
│   ├── docker/                  # Dockerfiles (UBI base, non-root)
│   └── demo-cluster/             # Demo cluster deployment manifests
├── examples/                    # Customer CRD examples (Telco, Financial Services, Sovereign)
├── workflows/                   # Deployment workflow definitions
├── docker-compose.yml           # Local dev infrastructure
├── docs/
│   ├── whitepaper/              # Current paper + archived engineering records
│   ├── customer-patterns/       # Illustrative reference architectures
│   ├── upstream/                # llm-d contribution and evidence package
│   └── proposals/               # Historical and exploratory proposals
├── hack/
│   ├── deploy-demo.sh           # One-click deployment script
│   └── local-dev.sh             # Kind multi-cluster dev setup
└── test/
    ├── architecture/            # Architectural proof tests
    ├── bdd/                     # BDD scenarios
    ├── contracts/               # Proto + OpenAPI validation
    ├── security/                # Auth integration tests
    ├── soak/                    # Sustained load test harness
    └── benchmarks/              # Workloads + scenarios
```

## REST API

The fleet controller's OpenAPI contract currently defines 43 paths. See
[`api/openapi/fleet-api.yaml`](api/openapi/fleet-api.yaml) for the complete
OpenAPI 3.1 specification; generated counts should not be treated as a stable
compatibility promise.

## Infrastructure

| Component | Purpose |
|-----------|---------|
| PostgreSQL | Optional durable state store; disabled in the community default. |
| HTTP Event Sink | Optional CloudEvents-compatible endpoint for fleet event streaming (can target Kafka REST proxy). No native Kafka or Redis client -- lib/pq is the sole Go dependency. |
| Prometheus + Grafana | Optional monitoring and dashboarding integrations. |
| External immutable ledger | Optional independently operated evidence service; required only by the governed production profile. |

## Contributing

Use the build and test commands above and open an issue or pull request in the
project repository. A dedicated contributor guide has not yet been added.


## License

This project is licensed under the [Apache License 2.0](LICENSE).

```
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```
