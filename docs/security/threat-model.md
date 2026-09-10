# Portable threat model

Status: implementation threat model for the OSS core. It is not a completed
product-security review or penetration-test report.

## Protected assets

- Tenant identity, role, entitlement, and quota isolation.
- Exact-model and eligible-provider decisions.
- Cluster registration, capability, health, and drain state.
- Routing endpoint, TLS trust, and authentication references.
- Model requests and streamed responses, which must never be logged.
- Governance proposals and external ledger evidence when configured.

## Trust boundaries

1. **Client to Model/API Gateway.** The external gateway owns customer
   authentication, OAuth/OIDC, API keys, subscriptions, and external policy.
2. **Gateway to fleet ingress.** Fleet trusts identity only after Ed25519
   assertion verification and, by default, verified mTLS client identity.
3. **Fleet to routing provider.** Only fleet-qualified exact-model providers
   enter Praxis or llm-d Router state.
4. **Routing provider to serving endpoint.** TLS server identity, trust, and
   authentication are operator-owned transport references.
5. **Controller to member Kubernetes API.** Cluster credentials can apply only
   the scoped resources granted by member-cluster RBAC.
6. **Optional integrations.** Classifiers, observations, proposals, and ledger
   receipts are inputs or evidence; none independently grants actuation.

## Principal threats and controls

| Threat | Implemented control | Remaining proof |
| --- | --- | --- |
| Forged tenant/role headers | Internal headers stripped; signed normalized identity | Live gateway conformance |
| Replayed identity assertion | Short `iat`/`exp` window, issuer/audience, max age | Gateway nonce/replay policy if required |
| Stolen gateway signing key | Public-key-only fleet keyring; overlapping key rotation | Operational rotation exercise |
| Direct bypass of gateway | Verified mTLS by default; NetworkPolicy expected | Deployment network review |
| Cluster/destination header injection | Gateway rebuilds internal routing headers after policy | Live proxy tests |
| SSRF through provider URL | URLs originate in operator/member state; fixed inference paths | Fuzzing plus admission policy review |
| Wrong-model substitution | Exact physical-model filtering before adapter scoring | Independent E2E proof |
| Stale provider remains routable | Freshness/drain/health filters and endpoint tombstones | Failure-injection soak |
| Stale KServe readiness | Generation match, `Ready=True`, published endpoint | Live KServe conformance |
| ConfigMap stale-key retention | Atomic JSON Patch replacement and explicit model tombstones | Live EPP reload test |
| Prompt or credential disclosure | Metadata-only request logging | Log pipeline inspection |
| Optional dependency grants authority | Admission remains fleet-owned; receipts never authorize | Governed-profile review |

## Fail-closed expectations

- Invalid or incomplete trusted identity returns `401`.
- Unknown roles cannot mutate fleet state.
- Missing exact-model capacity returns structured `503`.
- Invalid TLS/auth references cannot produce an eligible endpoint.
- Missing KServe API or stale status cannot become observed capacity.
- A configured ledger failure blocks governed mutations.
- Router/EPP failure cannot bypass fleet selection.

## Explicit non-claims

The OSS suite does not prove the security of a particular identity provider,
OpenShift installation, service mesh, certificate authority, PostgreSQL
service, ledger deployment, container registry, or model server. Those require
deployment-specific review and independent testing.
