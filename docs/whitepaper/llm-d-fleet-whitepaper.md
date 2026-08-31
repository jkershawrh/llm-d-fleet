# llm-d-fleet: Fleet eligibility and orchestration for llm-d

**Status:** OSS architecture and product-evidence paper

**Release:** v0.3.0

**Date:** August 2026

**License:** Apache License 2.0

## Abstract

llm-d provides intelligent inference routing and optimization within a
Kubernetes deployment. Organizations operating several independent llm-d
deployments also need a consistent way to register clusters, inventory exact
model capabilities, apply tenant and placement policy, remove stale or
draining providers, and hand only qualified destinations to a routing data
plane. llm-d-fleet implements that fleet boundary.

The project is intentionally data-plane neutral. Praxis is the validated
reference adapter for v0.3.0, and llm-d Router is the upstream-native beta.
The fleet layer does not replace KServe lifecycle, cluster-local EPP scoring,
KEDA/HPA pod scaling, WVA optimization, or inference runtimes. Optional
observation, governance, classification, and immutable-evidence systems are
integrations rather than OSS-core dependencies.

## 1. Problem and scope

A request crossing cluster or failure-domain boundaries must answer questions
that a cluster-local endpoint picker cannot safely answer alone:

- Which clusters are registered, authorized, fresh, and healthy?
- Which provider serves the requested exact physical model?
- Which tenant, residency, accelerator, and failure-domain constraints apply?
- Is a provider draining or below the availability level advertised to users?
- What failure response is correct when no compatible capacity remains?

llm-d-fleet owns those questions. It produces a normalized eligible-provider
set. The selected routing provider may choose among that set but cannot add an
incompatible, unauthorized, stale, or unhealthy destination.

## 2. Responsibility boundary

The core owns:

- cluster registration and capability inventory;
- logical-to-exact-physical-model resolution;
- tenant admission, quotas, and placement constraints;
- provider health, freshness, draining, and failure-domain status;
- fleet placement bounds and capacity policy;
- eligible-provider reconciliation; and
- an OpenAI-compatible ingress that protects internal routing metadata.

Adjacent systems retain their existing ownership:

- **KServe** owns cluster-local serving resources, revisions, and readiness
  when the KServe serving target is selected.
- **llm-d Router and EPP** own cluster-local endpoint scoring, prefix/session
  behavior, and queue/KV optimization.
- **KEDA and HPA** apply pod replica scaling. WVA may optimize heterogeneous
  variants and feed those scaling primitives.
- **Praxis or llm-d Router** supplies the selected cross-cluster routing data
  plane. Exactly one is authoritative in a deployment.
- **Inference runtimes** such as vLLM or OVMS execute the model.

The executable boundary is documented in the
[product-boundary specification](../architecture/product-boundary.md).

## 3. Architecture

```text
Client
  |
  | OpenAI-compatible request
  v
Fleet inference gateway
  | authenticate, quota, exact-model and policy qualification
  v
Normalized eligible-provider set
  |
  +--> Praxis adapter (validated)
  |
  +--> llm-d Router adapter (beta)
            |
            v
      selected cluster-local llm-d path
            |
            v
      KServe/InferencePool serving target
            |
            v
      exact inference backend
```

The gateway strips or replaces caller-provided internal routing headers. It
propagates a request ID, preserves backend status and streaming semantics, and
returns routing and exact-model metadata. If no compatible provider remains,
it returns structured `503 no_compatible_capacity`; it does not silently
substitute a different physical model.

## 4. Adapter model

The routing-provider interface receives normalized state including cluster and
failure-domain identity, logical and exact physical model, routing and metrics
endpoints, health, freshness, draining state, HA classification, capacity,
optional load signals, and TLS/authentication references.

Adapters translate this state into provider-specific configuration. They do
not own tenant policy, model compatibility, placement eligibility, or health
qualification.

### Praxis

Praxis remains the compatibility default and validated reference path. The
existing Grid resources are emitted behind the adapter boundary without
changing their external contract.

### llm-d Router

The beta adapter generates deterministic, atomically replaced endpoint files
for the Router multi-cluster discovery plugin. One model-specific EPP consumes
each homogeneous exact-model set. Fleet filtering remains authoritative while
upstream discovery and model-aware contracts mature. Queue and KV scoring are
enabled only when genuine, fresh cluster-local aggregates exist.

## 5. Serving and scaling

`FleetInferencePool` retains its backward-compatible InferencePool target and
adds an optional KServe `LLMInferenceService` target. With KServe selected,
fleet reconciles placement intent and reads status; it does not duplicate
KServe workload lifecycle.

`FleetScalingPolicy` expresses fleet budgets, placement bounds, and migration
policy. It is not a replacement pod autoscaler. KEDA over EPP metrics is the
default local scaling path, HPA remains supported, and WVA is an optional
heterogeneous-variant optimizer.

## 6. Optional ecosystem integrations

Community defaults keep semantic classification, GCL DecisionPackage ingress,
immutable evidence, DeepField observations, ModelPlane integration, and event
publishing disabled. The core must place and route correctly when all are
absent.

When configured, integrations remain subordinate to fleet admission:

- a classifier may suggest a tier but cannot override compatibility or policy;
- GCL may submit signed, expiry-bounded proposals but cannot actuate directly;
- an immutable ledger records evidence but its receipt never grants authority;
- DeepField may publish observations but is not in the request path; and
- ModelPack may resolve metadata but does not choose a destination.

The binary's `--production` mode is the governed-evidence profile and retains
its fail-closed external dependency requirements. Those requirements do not
apply to the portable OSS core.

## 7. Product evidence

Release `v0.3.0` demonstrates:

- exact-model qualification before routing-provider selection;
- CPU distribution across two failure domains;
- exact GPU routing to a distinct compatible provider;
- health-based provider removal and restoration within the 30-second target;
- structured GPU-capacity failure without CPU substitution;
- tenant and routing-header authority before adapter optimization; and
- core operation with optional ecosystem integrations absent.

The tagged source passed CI, security, portable conformance, and disposable
three-cluster Kind E2E gates. The release publishes AMD64 and ARM64 archives,
a boundary-checked OSS source archive, signed multi-architecture images,
CycloneDX SBOMs, provenance attestations, and SHA-256 checksums.

The [conformance report](../upstream/multicluster-product-conformance-2026-08-31.md)
defines the exact claims and limitations. Environment-specific capacity and
soak results are deliberately maintained outside the portable release.

## 8. Deliberate non-claims

- A single compatible GPU provider is `degraded/non-HA`, not highly available.
- The disposable reference topology is not certified for production.
- Synthetic queue or KV values are not accepted as routing-quality evidence.
- Control mappings are not third-party compliance certifications.
- Release provenance does not replace independent reproduction or formal
  product security review.

## 9. Remaining proof and upstream work

The next product-proof gate is independent execution of the
[clean-room protocol](../upstream/clean-room-reproduction.md). Formal threat
model review remains open, and GPU HA requires a second compatible provider in
another failure domain.

Upstream work is intentionally narrow: agree on a native dynamic cluster
discovery and health contract, preserve exact-model filtering before EPP
scoring, and maintain conformance behavior across routing implementations.
The [reviewer package](../upstream/reviewer-evidence-package.md) is the entry
point for that discussion.
