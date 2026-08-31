# EU AI Act Article 12 -- Record-Keeping Mapping

> Engineering mapping only; not legal advice, conformity assessment, or a
> compliance certification. Immutable evidence is an optional external
> integration outside the OSS-core default.

| Field | Value |
|-------|-------|
| **Regulation** | EU AI Act (Regulation 2024/1689), Article 12 |
| **Scope** | fleet-llm-d as infrastructure for high-risk AI system deployment |
| **Owner** | fleet-llm-d Compliance Lead |
| **Last Updated** | August 2026 |

## Context

Article 12 of the EU AI Act requires providers and deployers of high-risk AI systems to maintain automatic logging capabilities that enable traceability throughout the system's lifecycle. fleet-llm-d serves as the orchestration layer managing model placement, routing, scaling, and lifecycle -- making its audit trail a key component of Article 12 compliance for deployers.

## Record-Keeping Infrastructure

When configured, llm-d-fleet records governed decisions to an external
immutable ledger via `FleetRecorder` (`pkg/ledger/fleet_recorder.go`). The OSS
core defaults this integration to disabled. Each configured record includes:

- SHA-256 content hash (`computeInputHash`)
- Chain position within a typed hash chain
- Timestamp, agent ID, source ID, and correlation ID
- Full content payload for evidence verification

Verification: `FleetRecorder.VerifyAllChains()` validates all 5 chain types. CLI access via `fleetctl verify chains`.

## Article 12 Requirements Mapping

| Art. 12 Requirement | fleet-llm-d Coverage | Implementation | Status |
|---------------------|---------------------|----------------|--------|
| **12(1)** Automatic logging of events during operation | FleetRecorder records 9 event types to the ARE Ledger with tamper-evident hash chains | `pkg/ledger/fleet_recorder.go`: `RecordPlacement`, `RecordRoutingChange`, `RecordScalingEvent`, `RecordLifecycleEvent`, `RecordKVCacheTransfer`, `RecordTenantUsage`, `RecordAuthFailure`, `RecordRBACDenial` | Covered |
| **12(2)** Logging conformity with recognized standards | ARE Ledger uses SHA-256 hash chains with independent proof verification | `pkg/ledger/types.go`: `ProofReceipt`, `ChainVerification` | Covered |
| **12(3a)** Period of operation (start/end) | Model lifecycle events record deploy, promote, rollback timestamps | `RecordLifecycleEvent` with action field (deploy/promote/rollback) | Covered |
| **12(3b)** Reference database against which input data is checked | ModelPack OCI metadata resolution provides model provenance | `pkg/modelpack/` resolves GPU requirements, precision, format | Partial -- OCI signature verification not yet enforced |
| **12(3c)** Input data for which the search has led to a match | Placement decisions record model, cluster, GPU type, and scoring rationale | `RecordPlacement` with model, cluster, gpuType, reason fields | Covered |
| **12(3d)** Identification of natural persons involved in verification | Auth tokens carry subject identity; RBAC denials record subject | `pkg/auth/token.go` Claims (Subject, Role); `RecordRBACDenial` records subject | Covered |
| **12(4)** Logging appropriate to intended purpose of the system | Event types are scoped to fleet orchestration decisions, not inference content | FleetRecorder never records inference request/response payloads | Covered |

## Event Type Coverage Detail

| Event Type | Ledger Method | Chain Type | Records |
|------------|---------------|------------|---------|
| Model placement | `RecordPlacement` | placement | Model, cluster, replicas, GPU type, placement rationale |
| Routing change | `RecordRoutingChange` | routing | Model, source/target cluster, weight delta, reason |
| Autoscaling | `RecordScalingEvent` | scaling | Cluster, pool, from/to replicas, scaling reason |
| Tenant usage | `RecordTenantUsage` | tenant_usage | Tenant ID, model, cluster, tokens consumed, cost |
| Lifecycle (deploy/promote/rollback) | `RecordLifecycleEvent` | lifecycle | Model, version, action, cluster, detail map |
| KV cache transfer | `RecordKVCacheTransfer` | kv_transfer | Source/target cluster, model, bytes, cache hash |
| Auth failure | `RecordAuthFailure` | security | Remote address, failure reason |
| RBAC denial | `RecordRBACDenial` | security | Subject, resource, action |

## Verification and Export

| Capability | Implementation | CLI Command |
|------------|----------------|-------------|
| Hash chain integrity verification | `FleetRecorder.VerifyAllChains()` | `fleetctl verify chains` |
| Individual proof verification | `ProofReceipt` with `ProofVerification` | Programmatic via `pkg/ledger/client.go` |
| Soak-tested durability | 253 chains verified over 30 hours | `test/soak/` results in downstream environment evidence |
| Offline verification | Hash chain verification requires no network | Supports air-gapped sovereign deployments |

## Identified Gaps

| Gap | Art. 12 Relevance | Remediation |
|-----|-------------------|-------------|
| Cluster registration/deregistration events not recorded | 12(1) completeness | Add `RecordClusterRegistration` and `RecordClusterDeregistration` to FleetRecorder |
| Tenant CRUD events not recorded | 12(1) completeness | Add `RecordTenantCreate`, `RecordTenantUpdate`, `RecordTenantDelete` |
| ModelPack OCI signature verification not enforced | 12(3b) input database integrity | Implement SLSA provenance verification in `pkg/modelpack/` |
| No structured export format for regulatory submission | 12(2) standards conformity | Define a JSON/CSV export of ledger records for regulator consumption |
| Retention period not configurable per jurisdiction | 12 general | Add configurable retention policies to the ARE Ledger client |

## References

- `pkg/ledger/fleet_recorder.go` -- FleetRecorder with all 9 event recording methods
- `pkg/ledger/types.go` -- FleetDecision, LedgerReceipt, ProofReceipt, ChainVerification types
- `pkg/modelpack/` -- OCI model metadata resolution
- `pkg/auth/token.go` -- JWT claims with subject identity
- the downstream product security review -- overall audit status
