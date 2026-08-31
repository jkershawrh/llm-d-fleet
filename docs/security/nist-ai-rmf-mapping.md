# NIST AI RMF crosswalk — llm-d-fleet

> This engineering crosswalk is not a third-party assessment, legal opinion,
> or certification. Operators must evaluate the controls of their complete
> deployment and optional integrations independently.

| Field | Value |
|-------|-------|
| **Framework** | NIST AI Risk Management Framework (AI 100-1, January 2023) |
| **Scope** | fleet-llm-d as AI infrastructure orchestration platform |
| **Owner** | fleet-llm-d Compliance Lead |
| **Last Updated** | August 2026 |

## Overview

The NIST AI RMF defines four core functions: Govern, Map, Measure, and Manage. This document maps fleet-llm-d capabilities and its ecosystem integrations to each function and identifies gaps requiring additional tooling or process.

## Function: GOVERN -- Policies, processes, and accountability

| Category | Requirement | fleet-llm-d Implementation | Status |
|----------|-------------|---------------------------|--------|
| GV-1 | AI risk management policies | PlacementPolicy CRD (`api/crds/placementpolicy.yaml`) enforces GPU constraints, affinity, and zone requirements | Covered |
| GV-1 | Governance pipeline for AI decisions | governed-cognitive-loop (GCL) submits signed, expiry-bounded DecisionPackage CloudEvents to `POST /api/v2/intents` (`pkg/intents/`). GCL owns proposal signing and falsification. | Covered (external) |
| GV-2 | Accountability structures | RBAC with 4 ClusterRoles (controller, agent, viewer, tenant-admin) in `deploy/kustomize/base/rbac.yaml`. Admission webhook (`pkg/controller/webhook.go`) validates CRD mutations. | Covered |
| GV-3 | Workforce AI risk awareness | -- | Gap: no training materials or runbooks for operators |
| GV-4 | Organizational commitment to trustworthy AI | TenantProfile CRD (`api/crds/tenantprofile.yaml`) with quota enforcement (`pkg/tenant/quota/enforcer.go`). Immutable audit trail via ARE Ledger. | Covered |
| GV-5 | Third-party AI risk management | ModelPack OCI provenance (`pkg/modelpack/`) for model metadata. Dependency review in CI (`.github/workflows/security.yaml`). | Partial -- OCI signatures not enforced |
| GV-6 | AI risk communication | FleetRecorder records all decisions to ARE Ledger with `fleetctl verify chains` for stakeholder verification. | Covered |

## Function: MAP -- Context and risk identification

| Category | Requirement | fleet-llm-d Implementation | Status |
|----------|-------------|---------------------------|--------|
| MP-1 | Intended purpose and context | FleetInferencePool CRD (`api/crds/fleetinferencepool.yaml`) defines model serving intent: model, backend, replicas, resource requirements. | Covered |
| MP-2 | AI system classification | ModelLifecycle CRD (`api/crds/modellifecycle.yaml`) tracks model through validation, canary, stable, and deprecated phases via `pkg/lifecycle/rollout/controller.go`. | Covered |
| MP-3 | Benefits and costs assessment | Cost module (`pkg/cost/`) with `GET /api/v1/cost/pricing`, `/cost/tokenomics/{model}`, `/cost/chargeback/{tenant}`. | Covered |
| MP-4 | Deployment context risks | PlacementPolicy CRD defines constraints per deployment context (edge, cloud, sovereign). Placement solver (`pkg/placement/solver/`) and scorer (`pkg/placement/scorer/`) evaluate cluster fitness. | Covered |
| MP-5 | Impact assessment | deepfield-fleet produces observation, finding, forecast, and advisory-remediation CloudEvents. fleet-llm-d consumes these for placement decisions. | Covered (external) |

## Function: MEASURE -- Assessment and monitoring

| Category | Requirement | fleet-llm-d Implementation | Status |
|----------|-------------|---------------------------|--------|
| MS-1 | AI risk metrics | Observability metrics (`pkg/observability/metrics/`): throughput, TTFT p50/p99, queue depth, GPU utilization, KV cache utilization, pool saturation, ready endpoints, inflight requests. Exposed via `GET /api/v1/metrics/fleet`. | Covered |
| MS-2 | AI system evaluation | Soak testing infrastructure (`test/soak/`). 30-hour soak test verified 253 ledger chains. Results in downstream environment evidence. | Covered |
| MS-3 | Continuous monitoring | Fleet-agent reports metrics every heartbeat cycle (`POST /api/v1/agent/status`, `/agent/metrics`). ServiceMonitors in `deploy/kustomize/base/servicemonitors.yaml`. | Covered |
| MS-4 | Measurement feedback loops | Autoscaling collector (`pkg/autoscaling/collector/`) feeds optimizer (`pkg/autoscaling/optimizer/`) which triggers actuator (`pkg/autoscaling/actuator/`). | Covered |
| MS-5 | Bias and fairness metrics | -- | Gap: no bias monitoring capabilities |
| MS-6 | Human oversight of metrics | Dashboard (`web/`) provides real-time fleet visibility. `fleetctl` CLI for operational queries. | Covered |

## Function: MANAGE -- Response and governance actions

| Category | Requirement | fleet-llm-d Implementation | Status |
|----------|-------------|---------------------------|--------|
| MG-1 | Risk response planning | FleetScalingPolicy CRD (`api/crds/fleetscalingpolicy.yaml`) defines scaling behavior. Cluster drain (`POST /api/v1/clusters/{id}/drain`) for incident response. | Covered |
| MG-2 | AI incident management | CVE response process (`docs/security/cve-response-process.md`). Auth failure and RBAC denial recording to ARE Ledger. | Covered |
| MG-3 | Model lifecycle management | ModelLifecycle CRD with canary rollout via `pkg/lifecycle/rollout/controller.go`: CreateRollout, AdvanceRollout, RollbackRollout. SLO gates before promotion. | Covered |
| MG-4 | Decommissioning and deprecation | ModelLifecycle supports deprecation phase. Cluster deregistration via `DELETE /api/v1/clusters/{id}`. | Partial -- no formal decommission audit trail |

## Gap Summary

| Gap | RMF Category | Priority | Recommended Action |
|-----|-------------|----------|-------------------|
| No bias/fairness monitoring | MS-5 | High (financial services) | Integrate with external bias detection tooling; expose fairness metrics alongside inference metrics |
| No operator training materials | GV-3 | Medium | Create runbooks for common fleet operations and risk scenarios |
| OCI model signature verification not enforced | GV-5 | High | Implement SLSA provenance checks in `pkg/modelpack/` |
| No formal decommission audit trail | MG-4 | Medium | Add cluster/model decommission events to FleetRecorder |
| No adversarial robustness testing | MS-2 | Low | Outside fleet-llm-d scope; document as deployer responsibility |

## References

- `api/crds/` -- all 7+ CRD definitions (source of truth for Kubernetes types)
- `pkg/placement/` -- placement solver and scorer
- `pkg/autoscaling/` -- collector, optimizer, actuator pipeline
- `pkg/lifecycle/rollout/controller.go` -- canary rollout controller
- `pkg/observability/metrics/` -- fleet metrics collection
- `pkg/ledger/fleet_recorder.go` -- immutable audit trail
- `pkg/intents/` -- GCL DecisionPackage ingestion
