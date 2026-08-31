# SOC 2 Type II Evidence Collection Plan

> Evidence-collection template only; not an audit report or SOC 2 attestation.
> Operators must scope and validate controls with their auditor.

| Field | Value |
|-------|-------|
| **Framework** | AICPA SOC 2 Type II |
| **Scope** | fleet-llm-d platform controls across all deployment modes |
| **Audit Period** | 12 months (rolling) |
| **Owner** | fleet-llm-d Compliance Lead |
| **Last Updated** | August 2026 |

## Trust Service Criteria Mapping

### CC: Security (Common Criteria)

| Criteria | Requirement | fleet-llm-d Control | Evidence Source | Collection Method |
|----------|-------------|---------------------|-----------------|-------------------|
| CC6.1 | Logical access security | RBAC with 4 ClusterRoles: controller, agent, viewer, tenant-admin | `deploy/kustomize/base/rbac.yaml` | Quarterly export of ClusterRole/ClusterRoleBinding definitions |
| CC6.1 | Authentication controls | HMAC-SHA256 bearer tokens with configurable TTL | `pkg/auth/token.go` | Monthly review of token TTL configuration and rotation logs |
| CC6.2 | Access provisioning | Admission webhook validates CRD mutations by role | `pkg/controller/webhook.go` | Continuous: auth failure events from `FleetRecorder.RecordAuthFailure` |
| CC6.3 | Access removal | Cluster deregistration, tenant deletion | `DELETE /api/v1/clusters/{id}`, tenant API | Quarterly access review reports |
| CC6.6 | Threats to system boundaries | Network policies: default-deny-all, per-component allow | `deploy/kustomize/base/network-policies.yaml` | Monthly NetworkPolicy audit |
| CC6.8 | Vulnerability management | govulncheck, cargo audit, npm audit, Trivy | `.github/workflows/security.yaml` | Continuous: weekly scan results archived as CI artifacts |
| CC7.2 | Monitoring for anomalies | Rate limiting per-key token bucket; auth failure ledger events | `pkg/auth/ratelimit.go`, `RecordAuthFailure`, `RecordRBACDenial` | Continuous: ledger event stream |
| CC8.1 | Change management | Git-based workflow with PR review, CI gates, signed releases | `.github/workflows/ci.yaml`, `release.yaml` | Continuous: git log, PR merge records, cosign signatures |

### A: Availability

| Criteria | Requirement | fleet-llm-d Control | Evidence Source | Collection Method |
|----------|-------------|---------------------|-----------------|-------------------|
| A1.1 | Capacity management | FleetScalingPolicy CRD with autoscaling pipeline | `api/crds/fleetscalingpolicy.yaml`, `pkg/autoscaling/` | Monthly: scaling event history from ARE Ledger |
| A1.2 | Recovery objectives | Canary rollout with automated rollback on SLO breach | `pkg/lifecycle/rollout/controller.go` | Per-rollout: rollout state records |
| A1.2 | System resilience | Soak testing: 30-hour continuous operation validated | `test/soak/`, downstream environment evidence | Quarterly: soak test execution reports |
| A1.3 | Recovery testing | Edge site network partition handling by fleet-agent | `crates/fleet-agent/` | Quarterly: partition simulation results |

### C: Confidentiality

| Criteria | Requirement | fleet-llm-d Control | Evidence Source | Collection Method |
|----------|-------------|---------------------|-----------------|-------------------|
| C1.1 | Confidential data identification | Tenant isolation via TenantProfile CRD and QuotaEnforcer | `pkg/tenant/quota/enforcer.go` | Monthly: tenant access boundary review |
| C1.2 | Confidential data disposal | -- | -- | Gap: no formal data retention/disposal policy |
| C1.3 | Confidential data access restriction | Namespace isolation, tenant-scoped API access | RBAC + admission webhook | Continuous: RBAC denial events from ARE Ledger |

### PI: Processing Integrity

| Criteria | Requirement | fleet-llm-d Control | Evidence Source | Collection Method |
|----------|-------------|---------------------|-----------------|-------------------|
| PI1.1 | Processing integrity objectives | ARE Immutable Ledger with SHA-256 hash chains for all fleet decisions | `pkg/ledger/fleet_recorder.go` | Continuous: `fleetctl verify chains` |
| PI1.2 | Processing accuracy | Placement solver validates GPU requirements against ModelPack metadata | `pkg/placement/solver/`, `pkg/modelpack/` | Per-placement: placement decision records in ledger |
| PI1.3 | Processing completeness | 9 event types recorded; benchmark verified at 10/100/1000 entries | `pkg/ledger/recorder_bench_test.go` | Quarterly: chain completeness verification |

## Continuous Evidence Collection

| Evidence Type | Source | Frequency | Retention |
|---------------|--------|-----------|-----------|
| RBAC configuration snapshots | `deploy/kustomize/base/rbac.yaml` + live cluster state | Monthly | 14 months |
| Authentication/authorization events | ARE Ledger (`RecordAuthFailure`, `RecordRBACDenial`) | Continuous | Per ledger retention policy |
| Vulnerability scan results | `.github/workflows/security.yaml` CI artifacts | Weekly | 12 months |
| Change management records | Git commit history, PR reviews, CI pipeline results | Continuous | Indefinite (git) |
| Cosign image signatures | `.github/workflows/release.yaml` | Per release | Indefinite |
| SBOM artifacts | CycloneDX in `.github/workflows/security.yaml` | Per release | 12 months |
| Soak test reports | `test/soak/` execution results | Quarterly | 14 months |
| Scaling event history | ARE Ledger (`RecordScalingEvent`) | Continuous | Per ledger retention policy |
| Penetration test reports | `test/security/pen_test.py` + third-party | Annually + quarterly | 14 months |
| CVE response records | `docs/security/cve-response-process.md` workflow | Per incident | 14 months |

## Gaps and Remediation

| Gap | Trust Criteria | Priority | Remediation |
|-----|---------------|----------|-------------|
| No formal data retention/disposal policy | C1.2 | High | Define retention periods per data type; implement automated purge |
| Cluster registration/deregistration not in audit trail | CC8.1, PI1.3 | Medium | Add events to FleetRecorder |
| No centralized evidence repository | All | Medium | Stand up evidence collection system (e.g., Vanta, Drata, or internal) |
| TLS and workload identity are operator-provisioned rather than automatic | CC6.6, C1.3 | High | Use production mode, PostgreSQL `sslmode=verify-full`, HTTPS Praxis/ledger endpoints, pinned agent CA bundles, and document certificate rotation; add mutual workload identity. |

## References

- `pkg/ledger/fleet_recorder.go` -- immutable audit trail (9 event types)
- `deploy/kustomize/base/rbac.yaml` -- RBAC definitions (4 ClusterRoles)
- `deploy/kustomize/base/network-policies.yaml` -- network security policies
- `.github/workflows/security.yaml` -- vulnerability scanning pipeline
- `.github/workflows/release.yaml` -- signed release process
- `docs/security/cve-response-process.md` -- incident response process
