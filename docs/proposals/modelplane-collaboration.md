# ModelPlane + llm-d-fleet collaboration proposal

> **Historical collaboration draft.** Product naming, repository links, and
> implementation state in the body reflect the proposal date. The current OSS
> project is [llm-d-fleet](https://github.com/jkershawrh/llm-d-fleet), and the
> [product boundary](../architecture/product-boundary.md) is authoritative.

## To: Bassam Tabbara, Nic Cope (ModelPlane / Upbound)
## From: Jonathan Kershaw, Red Hat AI Engineering
## Date: July 2026

---

## Summary

I've built fleet-llm-d, an open-source (Apache 2.0) operations layer for fleet-level inference orchestration. After analyzing ModelPlane's architecture, we believe our projects are **complementary, not competitive** — ModelPlane handles infrastructure (provisioning, scheduling, caching, routing), while fleet-llm-d handles operations (tenant governance, cost/tokenomics, compliance, SLO-aware autoscaling).

I've already built the integration layer: fleet-llm-d consumes ModelPlane CRDs, injects fleet policies into ModelDeployments, records all ModelPlane state changes to an immutable compliance ledger, and computes cost/chargeback from InferenceClass GPU pricing.

**We'd like to explore collaboration and get access to the ModelPlane package for live integration testing.**

## What fleet-llm-d Adds to ModelPlane

ModelPlane users get these capabilities for free by deploying fleet-llm-d alongside:

| Capability | What It Does | Why ModelPlane Users Need It |
|---|---|---|
| **Tenant Governance** | Per-tenant quotas, rate limits, GPU budgets, cost caps | Enterprise customers need multi-tenant isolation — namespace-scoping isn't enough |
| **Cost Model** | GPU pricing (6 types × 3 tiers), cost-per-token, chargeback reports, budget alerts | No FinOps story in ModelPlane today; enterprises need chargeback |
| **Compliance/Audit** | ARE Immutable Ledger — hash-chained, tamper-evident decision records | EU AI Act, SOC 2, NIST AI RMF require auditable deployment decisions |
| **SLO-Aware Autoscaling** | Scales based on TTFT/throughput targets, not just replica count | ModelPlane only does whole-replica scaling; fleet-llm-d optimizes for SLOs |
| **Lifecycle Management** | SLO-gated canary rollouts with automatic rollback | ModelPlane has weighted routing; fleet-llm-d adds the SLO gate logic |

## What ModelPlane Adds to fleet-llm-d

fleet-llm-d users get these capabilities by deploying ModelPlane underneath:

| Capability | What It Does | Why fleet-llm-d Users Need It |
|---|---|---|
| **Cluster Provisioning** | Provision GKE/EKS clusters from CRDs | fleet-llm-d assumes clusters exist; ModelPlane creates them |
| **DRA-Aware Scheduling** | CEL device matching, capacity-aware spread | More sophisticated than fleet-llm-d's constraint solver |
| **Model Weight Caching** | PVC hydration from HuggingFace | fleet-llm-d has no model caching |
| **Gateway API Routing** | Traefik + Envoy Gateway, production-grade | More robust than fleet-llm-d's custom proxy |
| **Engine Agnostic** | Works with any container-based engine | fleet-llm-d is llm-d focused |

## Three-Layer Architecture

```
fleet-llm-d (operations)     ← tenant governance, cost, compliance, SLO scaling
     ↕
ModelPlane (infrastructure)   ← provisioning, scheduling, caching, routing
     ↕
llm-d (within-cluster)       ← EPP, KV cache, P/D disagg, flow control
```

## Integration Built

We've already implemented the integration layer (1,942 lines of Go):

- `pkg/modelplane/watcher.go` — polls ModelPlane API for CRD changes
- `pkg/modelplane/adapter.go` — converts InferenceCluster→ClusterInfo, ModelEndpoint→Backend
- `pkg/modelplane/policy.go` — injects placement annotations, replica counts, canary weights
- `pkg/modelplane/compliance.go` — records all ModelPlane events to ARE Ledger
- `pkg/cost/modelplane.go` — computes deployment cost from InferenceClass pricing
- 50 architecture proof tests, all passing

## Ask

1. **Access to the ModelPlane XPKG** — the `xpkg.crossplane.io/modelplaneai/modelplane:v0.1.0` package is private on GHCR. We'd like access for integration testing.

2. **Joint testing** — deploy ModelPlane + fleet-llm-d together and validate the full three-layer stack.

3. **CRD alignment** — ensure our policy injection annotations don't conflict with ModelPlane's scheduling.

4. **Potential upstream proposal** — discuss whether fleet-llm-d's governance capabilities should be proposed as a ModelPlane extension or remain a separate project.

## Project

- **GitHub:** https://github.com/jkershawrh/llm-d-fleet
- **Architecture:** 50 proven claims, 500+ tests, deployed on OpenShift with real inference
- **License:** Apache 2.0
