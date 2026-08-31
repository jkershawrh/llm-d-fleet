# Financial Services Deployment Pattern

> **Evidence status:** Reference architecture. Multi-region data residency
> enforcement and SLO-gated canary rollouts are design targets based on
> unit-tested components. Portable exact-model routing and provider-loss
> behavior have multi-cluster product evidence, but this regulated-industry
> pattern has not been independently certified.

## Context

Financial services organizations operate across multiple regions with strict
regulatory constraints on data residency. Every placement, routing, and scaling
decision must be auditable. Model updates require SLO-gated canary rollouts
to prevent customer-facing regressions.

## Architecture

Multi-region hub deployment with regulatory zone enforcement. Each region runs
its own fleet-controller instance (standalone mode) or connects to a central hub.
The ARE immutable ledger records every decision for compliance audit trails.

## fleet-llm-d Capabilities Used

| Requirement | CRD / Capability | Notes |
|---|---|---|
| Multi-region data residency | PlacementPolicy | Label-selector constraints enforce region boundaries |
| SLO-gated canary | ModelLifecycle | Canary strategy with configurable weight and SLO gates |
| Audit trails | ARE ledger integration | Hash-chained evidence for every placement and routing decision |
| Tenant isolation | TenantProfile | Per-LOB quotas with cluster access restrictions |
| Cost attribution | `pkg/cost/` | Per-tenant chargeback reports across GPU types and tiers |

## Example CRD

See [`examples/financial-services/fleet-resources.yaml`](../../examples/financial-services/fleet-resources.yaml) for
PlacementPolicy with regulatory constraints, ModelLifecycle with canary strategy,
and TenantProfile with cost controls.

## Design Targets (Not Yet Validated)

- Regulatory placement constraints preventing cross-region data movement
- SLO-gated canary rollouts with automatic rollback on regression
- Complete audit trail reconstruction via ledger correlation IDs
- Multi-region failover with regulatory-aware routing
