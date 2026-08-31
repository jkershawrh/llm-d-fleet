# Telco AI Grid Deployment Pattern

> **Evidence status:** Reference architecture. Multi-cluster topology is a design
> target, not measured production evidence for a 30-site carrier deployment.
> Portable three-cluster routing and failover behavior are supported by the
> current conformance report; the scale and latency targets below are unproven.

## Context

Telecommunications providers operate 30+ edge sites co-located at cell towers and
regional data centers. Each site runs a small GPU or CPU inference cluster. Real-time
workloads (voice AI, fraud detection, RAN optimization) require sub-50ms latency,
which mandates geographic routing to the nearest site.

## Architecture

A hub-and-spoke topology: a central hub runs the fleet-controller, Praxis AI gateway,
and ARE ledger. Regional hub clusters aggregate traffic and provide failover.
Edge site clusters run fleet-agent instances and lightweight inference models.

## fleet-llm-d Capabilities Used

| Requirement | CRD / Capability | Notes |
|---|---|---|
| 30+ edge sites | FleetCluster | One FleetCluster per site |
| Geographic routing | FleetRoutingPolicy | `preferLocal` strategy routes to nearest site |
| Sub-50ms latency | Semantic routing | Simple prompts route to local CPU/OVMS; complex to regional GPU |
| Tenant isolation | TenantProfile | Per-LOB quotas, rate limits, GPU budgets |
| Usage metering | `pkg/tenant/metering` | Per-tenant token consumption and cost attribution |
| Canary rollouts | ModelLifecycle | 10% canary with SLO gates before fleet-wide promotion |

## Example CRD

See [`examples/telco-edge/fleet-resources.yaml`](../../examples/telco-edge/fleet-resources.yaml) for a
complete set of PlacementPolicy, FleetRoutingPolicy, TenantProfile, and FleetInferencePool resources
configured for this pattern.

## Design Targets (Not Yet Validated)

- Sub-50ms TTFT at edge sites via geographic routing
- Automatic failover to regional hubs when edge sites are unavailable
- Method of Procedure (MOP) prompt prefix sharing across sites for throughput gains
- Tenant self-service via TenantProfile CRDs
