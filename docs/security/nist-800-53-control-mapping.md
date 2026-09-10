# NIST 800-53 Rev 5 Control Mapping

> Engineering crosswalk only; not a third-party assessment or authorization.
> Platform- and operator-owned controls must be evaluated in each deployment.

| Field | Value |
|-------|-------|
| **Framework** | NIST SP 800-53 Revision 5 |
| **Scope** | fleet-llm-d platform controls; delegated controls noted |
| **Owner** | fleet-llm-d Compliance Lead |
| **Last Updated** | August 2026 |

## Scope and Shared Responsibility

fleet-llm-d runs on OpenShift/Kubernetes and cloud infrastructure. Controls are categorized as:

- **fleet-llm-d**: directly implemented in fleet-llm-d code or configuration
- **Delegated to platform**: OpenShift, Kubernetes, or cloud provider responsibility
- **Shared**: fleet-llm-d configures; platform enforces

## AC -- Access Control

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| AC-2 | Account management | 4 ClusterRoles (controller, agent, viewer, tenant-admin) in `deploy/kustomize/base/rbac.yaml`. Service accounts per component. | fleet-llm-d |
| AC-3 | Access enforcement | AuthorizationMiddleware in `pkg/server/routes.go` checks roles against HTTP methods. Admission webhook (`pkg/controller/webhook.go`) validates CRD mutations. | fleet-llm-d |
| AC-4 | Information flow enforcement | NetworkPolicy default-deny-all with per-component allow rules in `deploy/kustomize/base/network-policies.yaml`. | Shared |
| AC-6 | Least privilege | Non-root containers (USER 65534:65534), readOnlyRootFilesystem, drop ALL capabilities. Scoped RBAC roles. | fleet-llm-d + platform |
| AC-7 | Unsuccessful logon attempts | Per-key token bucket rate limiter (`pkg/auth/ratelimit.go`). Auth failures recorded to ARE Ledger (`RecordAuthFailure`). | fleet-llm-d |
| AC-17 | Remote access | TLS for controller API (`--tls-cert`/`--tls-key`). `fleetctl` supports `--tls-ca` for custom CA. | fleet-llm-d |
| AC-24 | Access control decisions | RBAC + admission webhook + rate limiting. GCL DecisionPackage validation in `pkg/intents/`. | fleet-llm-d |

## AU -- Audit and Accountability

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| AU-2 | Event logging | FleetRecorder records 9 event types to ARE Immutable Ledger: placement, routing, scaling, tenant usage, lifecycle, KV cache transfer, auth failure, RBAC denial. | fleet-llm-d |
| AU-3 | Content of audit records | Each record includes: type, agentID, sourceID, correlationID, content (JSON), SHA-256 hash, chain position, timestamp. | fleet-llm-d |
| AU-6 | Audit record review | `fleetctl verify chains` CLI command. Dashboard (`web/`) for real-time monitoring. | fleet-llm-d |
| AU-9 | Protection of audit info | ARE Ledger is external infrastructure with its own database. Hash chains provide tamper evidence. Receipts never authorize execution. | fleet-llm-d + ARE |
| AU-10 | Non-repudiation | SHA-256 hash chains with `ProofReceipt` providing portable proof of each decision. GCL signatures on DecisionPackages. | fleet-llm-d + GCL |
| AU-11 | Audit record retention | -- | Gap: no configurable retention policy |
| AU-12 | Audit record generation | Automated via FleetRecorder methods called from placement, routing, scaling, lifecycle, and auth code paths. | fleet-llm-d |

## CM -- Configuration Management

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| CM-2 | Baseline configuration | CRD schemas in `api/crds/` define the configuration baseline for all 7+ fleet resource types. Kustomize overlays in `deploy/kustomize/`. | fleet-llm-d |
| CM-3 | Configuration change control | Git-based workflow with CI gates (`.github/workflows/ci.yaml`). All CRD mutations validated by admission webhook. Changes recorded to ARE Ledger. | fleet-llm-d |
| CM-6 | Configuration settings | PlacementPolicy, FleetScalingPolicy, FleetRoutingPolicy, KVCacheTransferPolicy CRDs define operational parameters. | fleet-llm-d |
| CM-7 | Least functionality | Minimal container images (UBI base, non-root, read-only filesystem). Single Go dependency (lib/pq). | fleet-llm-d |
| CM-8 | System component inventory | FleetCluster CRD (`api/crds/fleetcluster.yaml`) tracks registered clusters. FleetInferencePool CRD tracks model deployments. | fleet-llm-d |
| CM-11 | User-installed software | Container images signed with cosign (`.github/workflows/release.yaml`). ModelPack for OCI model metadata. | fleet-llm-d |

## IA -- Identification and Authentication

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| IA-2 | User identification/authentication | Short-lived Ed25519 assertions from a configured trusted gateway after issuer, audience, time-window, signature, and verified-mTLS checks (`pkg/auth/trusted_proxy.go`). HMAC bearer tokens remain an explicit compatibility/development provider (`pkg/auth/token.go`). | fleet-llm-d + gateway |
| IA-3 | Device identification/authentication | Fleet-agent identification via cluster registration (`POST /api/v1/clusters`). | fleet-llm-d |
| IA-4 | Identifier management | Service accounts per component (fleet-controller, fleet-agent). Tenant IDs in TenantProfile CRD. | fleet-llm-d + platform |
| IA-5 | Authenticator management | Trusted gateway public keys and assertion lifetime are operator-managed; compatibility token TTL is configurable via `fleetctl login`; `--tls-ca` supplies custom CA bundles. | fleet-llm-d + platform |
| IA-8 | Identification/auth for non-org users | External OIDC/SSO belongs to the selected Model/API Gateway, which maps authenticated identities into the signed internal assertion contract. | Delegated to gateway/operator |
| IA-9 | Service identification/authentication | -- | Gap: fleet-agent (Rust) does not yet use mTLS for controller communication |

## SC -- System and Communications Protection

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| SC-7 | Boundary protection | NetworkPolicy default-deny-all. Namespace isolation. Ingress restrictions. | Shared |
| SC-8 | Transmission confidentiality | Controller HTTPS via `--tls-cert`/`--tls-key`; `fleetctl` via `--tls-ca`; agent supports a pinned CA through `--tls-ca-cert`; production startup requires PostgreSQL `sslmode=verify-full` and HTTPS Praxis/ledger endpoints. | Partial -- see SC-8 gap |
| SC-12 | Cryptographic key management | Ed25519 gateway verification keys, compatibility HMAC secrets, and TLS certificates are supplied and rotated by the operator/platform. | Delegated to platform |
| SC-13 | Cryptographic protection | Ed25519 gateway assertions, optional HMAC compatibility tokens, SHA-256 ledger hash chains, and content hashing. | fleet-llm-d |
| SC-28 | Protection of information at rest | -- | Delegated to platform (dm-crypt/LUKS, cloud encryption) |

**SC-8 Gap**: TLS is configurable rather than automatically provisioned for
controller/agent communication, and mutual workload identity is not yet
implemented. KV cache transfer (TCP) has no TLS. Production mode rejects
PostgreSQL connections that do not use `sslmode=verify-full`; development mode
can still be configured less safely and must not be treated as production.

## SI -- System and Information Integrity

| Control | Description | Implementation | Responsibility |
|---------|-------------|----------------|----------------|
| SI-2 | Flaw remediation | CVE response process with severity-based SLAs (48hr Critical, 7d High). Weekly scans via govulncheck, cargo audit, npm audit, Trivy. | fleet-llm-d |
| SI-3 | Malicious code protection | Container image scanning (Trivy). SBOM generation (CycloneDX). Cosign image signing. | fleet-llm-d |
| SI-4 | System monitoring | ServiceMonitors in `deploy/kustomize/base/servicemonitors.yaml`. Fleet metrics via `pkg/observability/metrics/`. Agent heartbeat monitoring. | fleet-llm-d |
| SI-5 | Security alerts/advisories | Dependency review on PRs. GitHub Security Advisories. Monday CI security scans. | fleet-llm-d |
| SI-7 | Software/firmware/info integrity | Cosign image signatures. ARE Ledger hash chain verification. CycloneDX SBOM. | fleet-llm-d |
| SI-10 | Information input validation | Admission webhook validates CRD inputs. API handlers validate request payloads. | fleet-llm-d |

## Delegated Controls Summary

These controls are the responsibility of the underlying platform (OpenShift/Kubernetes/cloud provider):

| Control Family | Controls Delegated | Platform |
|---------------|-------------------|----------|
| PE (Physical/Environmental) | PE-1 through PE-20 | Cloud provider / data center operator |
| MP (Media Protection) | MP-1 through MP-8 | Cloud provider |
| CP (Contingency Planning) | CP-2, CP-7, CP-8, CP-9, CP-10 | Cloud provider + OpenShift |
| SC-28 (Data at rest encryption) | SC-28 | Cloud provider (dm-crypt/LUKS) |
| SC-12 (Key management) | SC-12 | Cloud provider / Vault / cert-manager |
| PS (Personnel Security) | PS-1 through PS-8 | Customer organization |

## References

- `deploy/kustomize/base/rbac.yaml` -- RBAC definitions
- `deploy/kustomize/base/network-policies.yaml` -- network policies
- `pkg/auth/trusted_proxy.go` -- trusted gateway assertion verification
- `pkg/auth/token.go` -- compatibility/development token implementation
- `pkg/ledger/fleet_recorder.go` -- audit event recording
- `api/crds/` -- CRD schema definitions
- `.github/workflows/security.yaml` -- vulnerability scanning
- `.github/workflows/release.yaml` -- signed releases
