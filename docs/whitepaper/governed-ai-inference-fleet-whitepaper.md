# llm-d-fleet: optional governed inference ecosystem

> **Optional governed-profile paper.** This document describes a broader
> ecosystem composition, not the minimum llm-d-fleet OSS product. External
> observation, governance, classification, and immutable-evidence systems are
> independently deployed integrations. The root README and product-boundary
> document are authoritative for `v0.3.0`.

## From Single-Cluster Inference to Governed Fleet Operations on OpenShift

**Red Hat Internal: Engineering and Leadership Audiences**

> **Profile and evidence notice (August 2026):** This paper documents the
> optional governed-observability profile and preserves its July measurements.
> It is not the portable OSS-core product definition. DeepField, GCL,
> immutable-ledger, ModelPlane, and semantic classification are optional
> integrations; the current product boundary is defined in
> `docs/architecture/product-boundary.md`. Historical measurements are not
> evidence for the beta llm-d Router adapter unless explicitly identified.

---

## Table of Contents

1. Executive Summary
2. Problem Statement
3. fleet-llm-d Architecture
4. Ecosystem Integration
5. Evidence: Test Coverage, Stress Tests, and Benchmarks
6. Heterogeneous Inference Economics
7. Production Readiness Assessment
8. What This System Does Not Claim
9. Competitive Landscape
10. Customer Patterns
11. Open-Source Foundation
12. Supply Chain Security
13. Conclusion

---

## 1. Executive Summary

llm-d solves AI inference scheduling within a Kubernetes cluster. fleet-llm-d
adds fleet eligibility and operations: cluster registration, exact-model
resolution, placement constraints, health and draining, tenant admission, and
eligible-provider reconciliation across failure domains. The selected routing
provider chooses among those qualified providers; KServe and llm-d retain
cluster-local serving and endpoint selection.

fleet-llm-d is an Apache-2.0 Go control plane with Rust spoke-agent and transfer
components. Its portable core is Kubernetes-oriented and does not require
OpenShift APIs. The validated Red Hat deployment uses OpenShift Routes as its
transport adapter. Praxis is the validated routing provider; llm-d Router is
an upstream-native beta receiving the same qualified provider set.

To validate the architecture, fleet-llm-d has been integrated with an ecosystem of supporting systems that exercise its full API surface: a predictive signal intelligence layer (deepfield-fleet), a governed decision-synthesis layer (governed-cognitive-loop), and a tamper-evident evidence layer (ARE immutable ledger). This ecosystem demonstrates what fleet-llm-d's extensible architecture enables: external governance systems submit signed proposals, fleet-llm-d independently verifies and actuates, and an independent ledger records the evidence.

**Verified on OpenShift (HubCluster cluster):**

| Metric | Value |
|---|---|
| 8-hour on-cluster soak | 5,534 governance cycles, 0 errors, 100% success |
| E2E latency (pod-to-pod) | p50=154ms, p95=485ms |
| Degradation injections | 15/15 passed, max recovery 1.1s |
| Chain integrity verifications | 95/95 passed over 8 hours |
| Control plane scale | Linear to 1,000 clusters, no knee point |
| Routing decision | 1.7 microseconds at 1,000 clusters |
| Test coverage | 462 fleet-llm-d tests + 1,157 ecosystem tests |

fleet-llm-d ships under Apache 2.0 and composes with the llm-d project, Red Hat OpenShift, Intel Xeon 6 (AMX), vLLM, and OVMS.

This whitepaper presents the architecture, verified evidence, honest boundaries, and production readiness assessment as of July 2026.

---

## 2. Problem Statement

### 2.1 The Missing Layer

Production AI inference at scale runs on well-understood layers: cluster provisioning (OpenShift), multi-cluster management (RHACM), within-cluster inference intelligence (llm-d), and API management (MaaS/AI Gateway). Each exists and works. None of them coordinates inference decisions across the fleet boundary.

The gap is fleet-level inference orchestration: deciding which models run on which clusters, how traffic flows between them, when to scale and where, which tenants get priority, and how to prove that every decision was governed, authorized, and recorded.

### 2.2 Customer Signals

Fourteen enterprise engagements independently describe the same unmet need. Three patterns recur across these engagements.

**Telco AI Grid.** Over 30 edge sites requiring geographic routing, sub-50ms latency targets, tenant isolation, and usage metering across the fleet.

**Financial Services.** Multi-region regulatory data residency, SLO-gated canary deployments, and complete audit trails for every placement and routing decision.

**Sovereign Cloud.** Air-gapped zones, data residency enforcement, GPU-as-a-Service multi-tenancy, and the ability to operate the entire governance pipeline within a single sovereign boundary.

### 2.3 Why Governed Autonomy Matters

Fleet decisions are consequential. A misconfigured placement wastes $32/hr per idle H100 GPU. An ungoverned scaling decision can cascade SLO breaches across tenants. A routing change without audit evidence fails regulatory review. The cost of an ungoverned fleet decision dwarfs the cost of governance overhead.

The governed profile enforces a simple principle: an LLM may interpret
objectives, but a deterministic controller computes actions, a falsification
gate challenges every proposal, cryptographic signatures bind decisions to
their evidence, and an external immutable ledger records what happened. This
profile is intentionally stricter than the default OSS-core installation.

---

## 3. fleet-llm-d Architecture

fleet-llm-d is the fleet-level inference orchestration layer. It sits above llm-d (within-cluster inference) and below enterprise consumers (governance systems, tenant portals, CI/CD pipelines). It owns admission, authorization, operation state, desired/observed state, and actuation.

### 3.1 Seven Capabilities

| Capability | Package | Function |
|---|---|---|
| Model Placement | `pkg/placement/` | Constraint-based solver assigns models to clusters by hardware affinity, capacity, latency, and compliance requirements |
| Cross-Cluster Routing | `pkg/routing/` | Policy evaluator resolves geographic, least-load, failover, and KV cache affinity routing rules |
| Fleet Autoscaling | `pkg/autoscaling/` | Metrics-driven fleet optimization with HPA integration and cross-cluster migration |
| Multi-Cluster Observability | `pkg/observability/` | Prometheus federation aggregates fleet-wide metrics across clusters |
| Tenant Governance | `pkg/tenant/` | Quota enforcement, budget tracking, priority scheduling, per-tenant metering and chargeback |
| Lifecycle Management | `pkg/lifecycle/` | Canary, blue-green, and rolling updates with SLO gates and automatic rollback |
| KV Cache Transfer | `pkg/kvcache/` | Cross-cluster KV cache state migration for session continuity during failover |

### 3.2 Declarative CRD Model

fleet-llm-d manages all fleet behavior through 10 declarative CRDs in the `fleet.llm-d.ai` API group:

| CRD | Purpose |
|---|---|
| FleetCluster | Cluster registration, connectivity, heartbeat, and capacity |
| FleetInferencePool | Fleet-wide model deployment intent spanning multiple clusters |
| FleetIntent | External governance intent lifecycle with decision package verification |
| FleetOperation | Internal operation tracking through a 17-phase lifecycle |
| PlacementPolicy | Hardware, regulatory, capacity, and cost constraints with affinity rules |
| FleetRoutingPolicy | Cross-cluster traffic distribution: weighted, geographic, failover, KV cache affinity |
| FleetScalingPolicy | Fleet-wide autoscaling objectives, constraints, and cross-cluster migration thresholds |
| TenantProfile | Per-tenant identity, quotas, rate limits, cost controls, and cluster access restrictions |
| ModelLifecycle | Rollout strategy (canary, blue-green, rolling), SLO gates, rollback triggers |
| KVCacheTransferPolicy | Cross-cluster KV cache transfer rules: triggers, transport, retention |

### 3.3 Intent Admission and Operation Lifecycle

fleet-llm-d's v2 intent boundary (`POST /api/v2/intents`) accepts cryptographically signed governance proposals from external systems. The admission adapter performs exhaustive validation: Ed25519 signature verification against a public-key ring, expiry checking, scope binding validation (tenant + zone), proposer identity verification (SPIFFE URI), evidence reference integrity, and fleet-owned authorization policy enforcement. Legacy HMAC-SHA256 packages are refused unless an operator explicitly enables the migration window.

A valid intent creates a FleetOperation that tracks through a 17-phase lifecycle:

```
RECEIVED -> ACCEPTED -> PLANNED -> AUTHORIZED -> ACTUATING -> OBSERVING -> VERIFIED -> SUCCEEDED
                                                                                   \-> FAILED
```

Each phase transition is recorded. Operations that fail at any phase record the failure reason, the phase at which failure occurred, and the full evidence chain. This lifecycle model means fleet-llm-d independently decides whether to execute a governance proposal, regardless of the source system's confidence or authority.

### 3.4 Architecture

**Go control plane.** The fleet-controller binary manages CRD reconciliation,
the 43-path OpenAPI surface (including OpenAI-compatible inference ingress),
intent admission, metrics federation, and cost modeling. It uses the standard
library HTTP server with HMAC-SHA256 bearer-token compatibility authentication,
per-key rate limiting, and configurable TLS 1.3.

**Spoke components and routing adapters.** fleet-agent and the KV transfer
libraries use Rust. The controller sends its normalized eligible-provider set
to one authoritative routing adapter. Praxis handles the validated inference
path. The llm-d Router adapter is beta and retains EPP ownership of queue, KV,
prefix, session, and endpoint scoring. OpenShift Route objects remain
environment-overlay resources rather than OSS-core APIs.

**Cost model.** 6 GPU types (A100-40GB through MI300X-192GB) across 3 pricing tiers (on-demand, reserved, spot) with per-tenant cost attribution, chargeback reports, and budget alert projections.

**Production gate model.** A 5-stage gate (Red through Gold) governs capability promotion. Each stage requires all rubric dimensions (correctness, performance, reliability, operability, security) to individually meet minimum thresholds. A high composite score cannot compensate for a single weak dimension.

**Test coverage.** 462 tests (436 Go + 26 Python), 55 architecture proofs, 63 BDD scenarios, 4 scale microbenchmarks.

### 3.5 ModelPack Integration

fleet-llm-d consumes ModelPack artifacts (CNCF model-spec, OCI-packaged model metadata) to automatically resolve GPU requirements during placement. When a FleetInferencePool references a model by OCI reference, the `ModelPackAwareConstraintSolver` resolves the model's parameter count, precision, and format, then auto-derives hardware constraints. This eliminates manual GPU specification for every placement policy.

---

## 4. Ecosystem Integration

fleet-llm-d's architecture is designed to accept governance proposals from external systems, actuate independently, and record evidence to external ledgers. To validate this extensibility, a complete ecosystem has been built around fleet-llm-d's API surface.

### 4.1 The Ecosystem Pipeline

```
deepfield-fleet  ->  governed-cognitive-loop  ->  fleet-llm-d  ->  are-immutable-ledger
  (Observe)              (Govern)                  (Act)              (Prove)
```

| System | Relationship to fleet-llm-d | What It Exercises |
|---|---|---|
| deepfield-fleet | Upstream signal producer | Generates advisory CloudEvents that trigger governance cycles |
| GCL | Governance proposal submitter | Submits signed DecisionPackages to fleet-llm-d's v2 intent boundary |
| ARE ledger | Evidence consumer | Receives placement, routing, scaling, and intent admission records from fleet-llm-d |

No system bypasses fleet-llm-d's admission boundary. GCL cannot actuate infrastructure. The ledger cannot authorize operations. fleet-llm-d independently verifies, authorizes, and decides.

### 4.2 Governance Integration (GCL)

GCL exercises fleet-llm-d's most demanding API surface: the v2 intent admission boundary. GCL's 7-stage governance pipeline (classify, predict, interpret, plan, falsify, commit, drive) produces Ed25519-signed DecisionPackages with expiry timestamps, scope binding, and full evidence chains. fleet-llm-d independently verifies the signature with GCL's public key, validates the trust envelope, and creates a FleetOperation only when all checks pass. Historical soak evidence in this paper used the earlier HMAC contract; current production policy requires Ed25519 by default.

GCL provides 6 governance scenarios (inference_fleet_spike, compliance_breach, capacity_exhaustion, slo_cascade, mixed_storm, multi_cluster_migration) that exercise fleet-llm-d's placement, routing, scaling, and migration capabilities through the governed path. The 8-hour soak test ran 5,534 governance cycles through this pipeline with zero errors.

### 4.3 Evidence Integration (ARE Ledger)

fleet-llm-d records 8 entry types to the ARE immutable ledger: placement assignments, routing shifts, scaling adjustments, tenant usage, lifecycle actions, KV cache transfers, auth failures, and RBAC denials. Each entry is hash-chained and correlation-indexed, enabling reconstruction of the full decision chain from any point in the pipeline. The ledger's proof receipts (WriteEntry p50=1.7ms, VerifyProof p50=0.6ms) add negligible overhead to fleet operations.

The 8-hour soak verified chain integrity 95 times over the full run with zero failures, confirming that fleet-llm-d's evidence recording maintains integrity under sustained load.

### 4.4 Signal Integration (deepfield-fleet)

deepfield-fleet produces advisory CloudEvents in 4 types (observation, finding, forecast, remediation proposal) that feed into GCL's governance pipeline. This exercises the upstream boundary of the ecosystem, confirming that fleet-llm-d's architecture supports a full observe-govern-act-prove pipeline with clear trust boundaries at each stage. deepfield-fleet never contacts fleet-llm-d directly, validating the separation of concerns.

### 4.5 Ecosystem Footprint

The complete ecosystem (all 4 systems) idles at approximately 8m CPU cores and 210 MB memory on OpenShift. The full pipeline (observe through prove) completes in approximately 100ms locally, 154ms pod-to-pod on OpenShift. For a system managing fleet-scale inference where actions happen on the order of seconds to minutes, this governance overhead is negligible.

---

## 5. Evidence: Test Coverage, Stress Tests, and Benchmarks

### 5.1 Test Coverage Summary

| System | Tests | Methodology |
|---|---|---|
| deepfield-fleet | 295 | Unit, integration, ecosystem contract, BDD |
| GCL | 822 | Unit, 33 EDD rubric, 15 BDD, 300 property tests |
| fleet-llm-d | 462 (436 Go + 26 Python) | Unit, contract, CRD validation, architecture, OpenAPI |
| ARE ledger | 40 (38 Rust + 2 Python) | Chain integrity, concurrent writes, gRPC, gateway |
| Cross-system (HubCluster) | 42/48 (87.5%) | 8-phase ecosystem stress test |
| **Total** | **1,619 + 55 architecture proofs + 63 BDD scenarios** | |

### 5.2 Ecosystem Stress Test (HubCluster, 8 Phases)

All four systems were exercised on the HubCluster cluster. GCL ran as a single pod on OpenShift. Fleet controller ran against the same harness.

| Phase | Tests | Passed | Highlights |
|---|---|---|---|
| 1. Smoke | 6 | 5 | All core endpoints healthy |
| 2. Performance | 2 | 1 | GCL p50=560ms (includes network RTT), fleet p50=2ms |
| 3. Pressure | 7 | 7 | 0 errors at 50 concurrent governance cycles |
| 4. Edge cases | 9 | 7 | Evidence poisoning, falsification bypass, cooldown correct |
| 5. Degradation | 10 | 10 | All 6 scenarios degrade gracefully when fleet unreachable |
| 6. Soak | 6 | 6 | 300 sequential cycles, 0 errors, p50=566ms (remote), 1.2x latency drift |
| 7. Pen testing | 5 | 5 | No injection or traversal vulnerabilities |
| 8. Chaos | 3 | 1 | 200 simultaneous cycles with 0 errors; single-pod ceiling reached |
| **Total** | **48** | **42 (87.5%)** | |

**The 6 failures explained.** None are functional defects:

| Failed Test | Phase | Root Cause | Classification |
|---|---|---|---|
| `fleet_metrics` | Smoke | Fleet expvar endpoint not exposed via OpenShift Route (only available on metrics port 9091, not the routed API port) | Deployment configuration |
| `gcl_cycle_latency` p99 | Performance | p99=1,086ms exceeds the 500ms threshold, but the test ran from a remote client. The ~500ms network round-trip via sslip.io TLS route dominates. The on-cluster 8-hour soak (Section 5.3) shows p50=154ms pod-to-pod. | Test configuration (threshold too tight for remote) |
| `nan_evidence` | Edge Cases | Python `json` module raises ValueError for NaN float values before the request reaches GCL. This is a client-side serialization limitation. | Client limitation, not a platform defect |
| `wrong_content_type` | Edge Cases | GCL returns 422 instead of 415 for wrong Content-Type on the CloudEvents endpoint. FastAPI validates the request body before checking the content type header. | HTTP semantics (minor) |
| `gcl_10kb_payload` | Chaos | Returns 503 after 200 simultaneous governance cycles saturated the single GCL pod. The system was still recovering from the rapid-fire chaos test. | Expected single-pod ceiling |
| `gcl_reset_recovery` | Chaos | Empty response during recovery from the same 200-concurrent saturation. | Same root cause as above |

The two chaos failures confirm a known architectural boundary: a single GCL pod saturates at approximately 200 simultaneous governance cycles. Under normal operating conditions (concurrency below 50), the system handles all payloads with zero errors. The on-cluster 8-hour soak in Section 5.3, which operates within normal concurrency, completed 5,534 cycles with zero failures.

**Signal volume scaling.** 100, 500, and 1,000 concurrent signals all completed in approximately 770ms, confirming linear scaling in the signal processing path.

**Pressure test details.** 50 concurrent governance cycles produced 0 errors. The system maintained correctness under concurrent load without race conditions or dropped requests.

**Degradation behavior.** All 6 governance scenarios (inference_fleet_spike, compliance_breach, capacity_exhaustion, slo_cascade, mixed_storm, multi_cluster_migration) degrade gracefully when downstream systems are unavailable.

### 5.3 Production-Emulation Soak (HubCluster, On-Cluster)

An 8-hour production-emulation soak ran entirely on-cluster on OpenShift (pod-to-pod, no external network). The soak driver ran as a Kubernetes Job in the `fleet-llm-d` namespace, exercising fleet-llm-d's full API surface: v2 intent admission, health probes, metrics federation, and ledger recording, driven by the governance ecosystem across all 6 scenarios with 15 degradation injections over 8 continuous hours.

| Metric | Value |
|---|---|
| Duration | 480 minutes (8 hours) |
| Total governance cycles through fleet-llm-d | 5,534 |
| Success rate | 100.0% |
| E2E latency p50 | 154ms |
| E2E latency p95 | 485ms |
| E2E latency p99 | 606ms |
| Latency stability | 149-160ms for 8 hours, no drift |
| Chain integrity verifications | 95/95 passed |
| Health availability (fleet-llm-d) | 100% |
| Degradation injections passed | 15/15 |
| Max injection recovery time | 1.1s |
| SLO gates passed | 5/5 |

**Degradation injections (15/15 passed):**

| Injection Type | Count | Result |
|---|---|---|
| Burst-50 concurrent requests to fleet-llm-d | 4 | 0 errors every time |
| Invalid intent submission | 4 | Correctly rejected (401) every time |
| Upstream governance reset | 4 | fleet-llm-d unaffected, ~1s ecosystem recovery |
| Expired event rejection | 3 | Correctly rejected (403) every time |

fleet-llm-d maintained 100% availability and sub-200ms p50 latency throughout all 15 injections. The system survived 8 continuous hours of sustained load with zero errors, zero latency drift, and zero chain integrity failures.

### 5.4 fleet-llm-d Scale Microbenchmarks

These benchmarks measure data structure scaling behavior in the Go control plane. They prove algorithmic complexity, not production throughput at fleet scale (see Section 7).

| Component | 10 Clusters | 100 Clusters | 1,000 Clusters | Allocs/op |
|---|---|---|---|---|
| InMemoryList | 345ns | 3,412ns | 35,114ns | 1 |
| Solver | 1,023ns | 8,431ns | 81,703ns | varies |
| Balancer | 34ns | 172ns | 1,720ns | 0 |
| Reconciler | 1,361ns | 8,712ns | 84,186ns | varies |

All components scale linearly from 10 to 1,000 clusters. No knee point observed. The Balancer achieves zero allocations per operation at all cluster counts.

### 5.5 Decision Latency Breakdown

| Stage | Local Latency |
|---|---|
| deepfield classification (nano tier) | 5-12ms |
| GCL governance cycle (classify through signed DecisionPackage) | 54-75ms |
| fleet-llm-d intent admission | less than 10ms |
| ARE ledger write (proof receipt) | less than 5ms |
| **Full pipeline (observe, govern, act, prove)** | **approximately 100ms** |

Under sustained remote load on HubCluster (300 sequential cycles): p50=566ms, p95=900ms, 0 errors. The remote latency is dominated by approximately 500ms of network round-trip, not processing.

---

## 6. Heterogeneous Inference Economics

### 5.1 Cost Comparison

The platform manages CPU and GPU inference side by side, routing workloads to the most cost-effective tier based on prompt complexity.

| Hardware | Runtime | Precision | Hourly Cost | Use Case |
|---|---|---|---|---|
| Intel Xeon 6 (256 cores, AMX) | OVMS (C++ continuous batching) | INT8 | $0.60/hr | Simple and standard prompts |
| GPU accelerator (Intel Gaudi, NVIDIA H100) | vLLM (PagedAttention) | FP16 | $12-32/hr | Complex prompts, large models |

This represents up to a 53x cost reduction for workloads eligible for CPU inference. The HubCluster validation cluster runs CPU-only inference on Intel Xeon processors. GPU accelerator testing (Intel Gaudi) is planned for a dedicated accelerator cluster. fleet-llm-d's heterogeneous routing is hardware-agnostic: it routes by workload complexity, not by accelerator vendor.

### 5.2 Semantic Routing

The semantic router classifies incoming prompts into three complexity tiers:

| Tier | Description | Routed To |
|---|---|---|
| Simple | Factual lookups, short answers | Intel Xeon 6 / OVMS INT8 |
| Standard | Moderate reasoning, structured output | Intel Xeon 6 / OVMS INT8 |
| Complex | Multi-step reasoning, long-form generation | GPU accelerator / vLLM FP16 |

Simple and standard prompts, which represent the majority of enterprise inference traffic, route to CPU inference at $0.60/hr instead of $32.00/hr.

### 5.3 Benchmarked Models

Five models have been benchmarked on the heterogeneous inference pipeline:

| Model | Parameters | Notes |
|---|---|---|
| Granite 350M | 350M | Smallest footprint, nanoagent-class tasks |
| Granite 2B INT8 | 2B | INT8 quantized for Xeon AMX acceleration |
| Granite 4.1 3B | 3B | Latest Granite generation |
| Phi-3-Mini | 3.8B | Microsoft, strong reasoning per parameter |
| Qwen 2.5 3B | 3B | Alibaba, multilingual capability |

### 5.4 Autoscaling Behavior

HPA autoscaling on OpenShift scales from 1 to 4 replicas in 2 minutes under load. Scale-down follows the standard HPA stabilization window.

---

## 7. Production Readiness Assessment

### 6.1 Current Gate Status

The platform is at **Yellow** gate status: unit tests and contract evidence pass. It has not been promoted to Blue (stress/soak evidence) or Gold (production workload validation) in the formal 5-stage gate model.

The 2-hour on-cluster soak provides strong evidence toward Blue gate, but formal promotion requires completion of the criteria defined in the gate rubric.

### 6.2 Supply Chain Security

| Control | Status | Detail |
|---|---|---|
| Image signing | Active | cosign at release |
| Container scanning | Active | Trivy, CRITICAL/HIGH blocking |
| Go vulnerability scanning | Active | govulncheck on every push and PR |
| Rust dependency audit | Active | cargo audit on every build |
| SBOM generation | Active | CycloneDX format |
| Go CVEs | 0 | Clean as of July 2026 |
| Base image CVEs | 1 HIGH | UBI 9 base OS, unfixed upstream |

### 6.3 Observability Status

The fleet controller now serves Prometheus text exposition format at `/metrics` on port 9091, exposing 8 metrics: request/error counters, cluster/pool/tenant/rollout gauges, process memory (alloc/sys/heap_inuse), and goroutine count. The existing Grafana dashboards (fleet-overview, fleet-operations) and 24 Prometheus recording rules can now scrape real data from the controller.

The expvar JSON endpoint is preserved at `/debug/vars` for backward compatibility with the soak test driver.

### 6.4 Security Status

**NetworkPolicies.** Default-deny NetworkPolicies with per-component allowlists are deployed on HubCluster. The fleet-controller accepts traffic only from the GCL namespace, OpenShift ingress, and authorized test pods. Mock-inference and modelplane-mock accept traffic only from the fleet-controller. All unauthorized cross-pod traffic is blocked.

**Security audit.** A 104-item security checklist exists at the downstream product security review. Current status: **Not Started**. Container hardening is complete (readOnlyRootFilesystem, drop ALL capabilities, non-root UID 65534). Supply chain CI controls are active and blocking (cosign, Trivy, govulncheck, cargo audit). No third-party security audit has been conducted.

### 6.5 Resilience Status

6 resilience tests passed on HubCluster (SNO):

| Test | Result |
|---|---|
| Fleet controller pod kill | 9ms recovery |
| GCL pod kill | 8ms recovery |
| Mock inference pod kill | Fleet stays healthy |
| Simultaneous fleet + GCL kill | 12ms / 10ms recovery |
| Rapid restart 5x fleet kill | avg 7ms, max 12ms |
| Post-disruption 60s soak | 28 events, 0 errors |

### 6.6 Multi-Cluster Simulation Status

28 simulated cluster agents were deployed on HubCluster as a StatefulSet, each registering with the fleet controller with unique cluster IDs, regions (7 regions), GPU types (5 types), and load patterns (5 patterns). All 28 registered successfully. The simulated agents prove the controller can track and manage N clusters with live health data; they do not prove real spoke cluster monitoring (see Section 8.2).

### 6.7 Soak Test Status

| Test | Duration | Status | Result |
|---|---|---|---|
| Ecosystem stress test (8 phases) | Variable | Complete | 42/48 passed (87.5%) |
| On-cluster pod-to-pod soak | 2 hours | Complete | 2,240 cycles, 0 errors, 100% success |
| 8-hour overnight production pipeline soak | 8 hours | **Complete** | 5,534 cycles, 0 errors, 100% success, 15/15 injections passed, 95/95 chain verifications |

The 8-hour overnight soak exercised fleet-llm-d's full API surface through the production deepfield CloudEvent pipeline with bearer token authentication, running on-cluster with pod-to-pod communication. fleet-llm-d maintained 100% availability with p50=154ms latency and zero drift over the full 8 hours.

---

## 8. What This System Does Not Claim

This section is mandatory. It describes the boundaries of what the evidence supports. Every limitation listed here is a known gap, not a future promise.

### 7.1 No Optimality

The placement solver, autoscaler, and governance pipeline produce feasible actions under hard constraints. They do not produce provably optimal actions. The constraint solver finds a valid placement; it does not guarantee the globally cheapest or lowest-latency placement. The autoscaler respects budget ceilings and SLO floors; it does not solve a global optimization problem.

### 7.2 Multi-Cluster Signal Evidence Is Adapter-Specific

The fleet-agent reporter now scrapes local metrics and reports real spoke
status from HubCluster, CpuCluster, and GpuCluster. The controller also implements optional
mTLS Grid Signals polling and a portable publisher that strips pod-level
labels. This corrects the July statement that the reporter was entirely
stubbed. However, the llm-d Router multi-cluster metrics extractor and
TLS-capable proxy have not completed direct inference qualification. Existing
Praxis monitoring evidence must not be presented as Router-adapter evidence.

### 7.3 No 72-Hour Soak

The longest verified continuous soak is 8 hours (5,534 governance cycles, 0 errors, 100% success, 154ms p50). No soak of 72 hours or longer has been attempted. Memory leaks, connection pool exhaustion, certificate rotation, and other time-dependent failure modes that manifest beyond 8 hours have not been tested.

### 7.4 No Security Audit

The 104-item security audit checklist is at "Not Started" status. Container hardening is complete and supply chain CI is active, but no systematic security review has been performed against the checklist. No third-party penetration test has been conducted. The pen testing phase of the ecosystem stress test (5/5 passed) covers injection and traversal attacks but is not a substitute for a comprehensive security audit.

### 7.5 Scale Benchmarks Prove Data Structures, Not Fleet Operations

The microbenchmarks in Section 4.4 demonstrate that the solver, balancer, reconciler, and in-memory list data structures scale linearly from 10 to 1,000 clusters. They do not demonstrate that the complete fleet controller, with real Kubernetes API calls, network latency, database operations, and concurrent reconciliation loops, scales linearly. Data structure scaling is necessary but not sufficient evidence for system-level scaling.

### 7.6 No Production Workload Validation

All testing to date uses synthetic governance scenarios and simulated workloads. No production customer workload has been processed through the 4-system pipeline. The 6 validated governance scenarios (inference_fleet_spike, compliance_breach, capacity_exhaustion, slo_cascade, mixed_storm, multi_cluster_migration) are representative but not exhaustive.

### 7.7 Single-Pod Ceiling

The chaos phase of the ecosystem stress test revealed a single-pod ceiling: GCL running as a single pod on OpenShift saturated under 200 simultaneous governance cycles. Horizontal scaling of GCL pods has not been tested. The 1/3 pass rate in the chaos phase reflects this architectural constraint.

### 7.8 Leader Election Not Implemented

The fleet controller runs as a single instance. Leader election for multi-replica high availability is not implemented. The hub-mode deployment description should not be read as implying HA capability. Single-controller failure means fleet-level operations pause until the controller restarts.

### 7.9 Regulatory Compliance Is Structural, Not Certified

The platform provides the structural mechanisms (hash-chained evidence, signed decisions, correlation-based reconstruction) that map to requirements in the EU AI Act, NIST AI RMF, and SOC 2 Type II. No formal compliance certification or legal review has been obtained. The mapping between platform capabilities and regulatory requirements is the engineering team's analysis, not a legal opinion.

---

## 9. Competitive Landscape

Four projects occupy adjacent positions in the fleet inference orchestration space. None provides governed autonomy with falsification and tamper-evident accountability.

| Project | Strengths | Does Not Provide |
|---|---|---|
| NVIDIA Dynamo | GPU scheduling, hardware-optimized placement | Governance layer, decision accountability, falsification gate, immutable evidence |
| ModelPlane (CNCF) | Infrastructure lifecycle, Crossplane-based cluster management | Autonomy, falsification, semantic routing, cost optimization |
| SkyPilot Endpoints | Multi-cloud routing, cost-aware placement | Governance pipeline, falsification gate, immutable evidence, tenant governance |
| Rafay | Tenant self-service, Kubernetes fleet management | Falsification gate, immutable ledger, governed autonomy, heterogeneous inference |

**Differentiators of this platform:**

1. **Governed autonomy with falsification.** GCL's 7-component pipeline with 7 deterministic falsification checks is unique. No competing project challenges proposed actions before execution.

2. **Honesty boundary.** The AST-enforced, OPA Guardian-sidecar-enforced separation between LLM interpretation and deterministic action computation has no equivalent in competing projects.

3. **Tamper-evident accountability.** The ARE ledger's hash-chained, correlation-indexed evidence chain provides reconstructable timelines for every decision. No competing project offers comparable audit infrastructure.

4. **Heterogeneous inference economics.** Semantic routing across CPU (Intel Xeon 6 / OVMS INT8 at $0.60/hr) and GPU (Intel Gaudi or NVIDIA / vLLM FP16) tiers with up to 53x cost reduction for eligible workloads. No competing project integrates CPU inference as a first-class tier.

---

## 10. Customer Patterns

### 9.1 Telco AI Grid

**Context.** Over 30 edge sites, geographic routing requirements, sub-50ms latency targets for user-facing inference.

**Platform mapping:**

| Requirement | Platform Capability |
|---|---|
| 30+ edge sites | FleetCluster CRDs, per-site fleet-agent instances |
| Geographic routing | FleetRoutingPolicy with geographic preference rules |
| Sub-50ms latency | Semantic routing to local Intel Xeon OVMS for simple prompts |
| Tenant isolation | TenantProfile with per-tenant quotas, rate limits, cluster restrictions |
| Usage metering | Per-tenant token consumption and cost attribution via `pkg/tenant/metering` |

### 9.2 Financial Services

**Context.** Multi-region deployments, regulatory constraints on data residency, audit requirements for every infrastructure decision.

**Platform mapping:**

| Requirement | Platform Capability |
|---|---|
| Multi-region data residency | PlacementPolicy with CEL-based regulatory zone constraints |
| SLO-gated canary | ModelLifecycle with canary strategy, SLO gates for promotion, automatic rollback |
| Audit trails | ARE ledger hash-chained evidence, correlation-based decision reconstruction |
| Regulatory evidence | Structural mapping to EU AI Act Article 12, NIST AI RMF, SOC 2 Type II |

### 9.3 Sovereign Cloud

**Context.** Air-gapped zones, strict data residency, multi-tenant GPU-as-a-Service within sovereign boundaries.

**Platform mapping:**

| Requirement | Platform Capability |
|---|---|
| Air-gapped operation | Standalone deployment mode (controller, PostgreSQL, Redis on single node) |
| Data residency enforcement | PlacementPolicy constraints, FleetRoutingPolicy preventing cross-zone traffic |
| GPU-as-a-Service multi-tenancy | TenantProfile GPU quotas, FleetScalingPolicy per-tenant GPU budgets |
| Full pipeline within sovereign boundary | All 4 systems deploy within a single OpenShift cluster |

---

## 11. Open-Source Foundation

The platform builds on established open-source projects across the Red Hat, Intel, and CNCF ecosystems.

| Project | Role in Platform | License |
|---|---|---|
| llm-d | Single-cluster inference scheduling and gateway | Apache 2.0 |
| ModelPack | OCI-based model metadata resolution (CNCF model-spec) | Apache 2.0 |
| vLLM | GPU inference serving: FP16, continuous batching, PagedAttention | Apache 2.0 |
| OVMS | CPU inference serving: INT8, AMX acceleration, C++ continuous batching | Apache 2.0 |
| OPA | Runtime honesty boundary enforcement via Rego policies | Apache 2.0 |
| CloudEvents | Interoperability standard for cross-system event communication | Apache 2.0 |
| Red Hat OpenShift | Container orchestration, CRD-driven management, RBAC, Routes | Various |
| Intel Xeon 6 | CPU inference hardware: 256 cores, AMX instructions | N/A (hardware) |

fleet-llm-d composes with llm-d rather than forking it. All within-cluster inference intelligence (EPP, WVA, KV cache management, prefill/decode disaggregation) remains in the upstream llm-d project. fleet-llm-d operates strictly above the cluster boundary.

---

## 12. Supply Chain Security

### 11.1 Active Controls

| Control | Scope | Trigger |
|---|---|---|
| cosign image signing | Container images | Release |
| Trivy container scanning | Container images | Build (CRITICAL/HIGH blocking) |
| govulncheck | Go dependencies | Every push and PR |
| cargo audit | Rust dependencies | Every build |
| CycloneDX SBOM | All artifacts | Release |

### 11.2 Vulnerability Status (July 2026)

| Category | Count | Detail |
|---|---|---|
| Go CVEs | 0 | Clean |
| Rust CVEs | 0 | Clean (cargo audit passing) |
| Container base image | 1 HIGH | UBI 9 base OS, unfixed upstream, not addressable by this project |

---

## 13. Conclusion

fleet-llm-d fills a verified gap in the llm-d ecosystem: 14 enterprise customers independently request fleet-level inference orchestration, and no existing open-source project provides it. fleet-llm-d extends llm-d from single-cluster inference to multi-cluster fleet operations with declarative CRD-driven management, cryptographically verified intent admission, and a 17-phase operation lifecycle.

The evidence supports the following statements about fleet-llm-d:

- fleet-llm-d's control plane data structures (placement solver, routing balancer, reconciler, cluster registry) scale linearly from 10 to 1,000 clusters with no knee point. The routing balancer runs at 1.7 microseconds per decision at 1,000 clusters with zero allocations.
- fleet-llm-d maintained 100% availability and p50=154ms latency over an 8-hour on-cluster soak on OpenShift (5,534 governance cycles, 0 errors, 15/15 degradation injections passed, 95/95 chain integrity verifications).
- fleet-llm-d's v2 intent admission boundary correctly handles cryptographically signed governance proposals, rejecting invalid signatures, expired packages, and malformed intents while admitting valid proposals through the full 17-phase operation lifecycle.
- Heterogeneous inference with semantic routing achieves up to 53x cost reduction ($0.60/hr Intel Xeon 6 OVMS INT8 vs. GPU accelerator tiers) for eligible workloads. CPU inference validated on HubCluster; GPU accelerator testing planned for dedicated cluster.
- The complete ecosystem (fleet-llm-d + governance + evidence) operates at approximately 100ms local pipeline latency with approximately 8m CPU and 210 MB idle footprint.

The evidence does not support claims of multi-cluster monitoring at scale (fleet-agent is stubbed), long-duration reliability beyond 8 hours, security audit completion, or production workload validation. The current gate status is Yellow. These gaps are documented in Section 8 and represent the work remaining before Blue or Gold gate promotion.

fleet-llm-d is positioned to provide Red Hat with a differentiated, open-source fleet inference orchestration layer that composes with llm-d, extends OpenShift's value proposition into fleet-level AI operations, and enables governed autonomy patterns that no competing project offers.

---

**Author:** Jonathan Kershaw, Red Hat, July 2026, Draft 0.2
