# llm-d-fleet v0.4.0-rc.1 review brief

## Meeting objective

Determine whether fleet-level provider eligibility, health, and exact-model
discovery should proceed as an llm-d upstream proposal, where its contracts
should live, and which interfaces need joint ownership with llm-d Router and
KServe. This is an architecture and incubation discussion—not a production
certification or product commitment.

## Thirty-second description

llm-d-fleet is an Apache-2.0, data-plane-neutral fleet control plane. It
qualifies which clusters may serve an exact physical model using identity,
policy, health freshness, draining state, capability, and failure-domain
constraints. It then publishes only that eligible set to one routing provider.
Praxis is the validated reference adapter; the llm-d Router adapter is beta.
KServe, EPP, KEDA, and optional WVA retain cluster-local ownership.

## Problem being proposed upstream

Cluster-local inference routing cannot by itself establish whether another
cluster is authorized, fresh, compatible with the exact model, draining, or in
the required failure domain. A fleet eligibility layer supplies that bounded
candidate set without taking ownership of EPP scoring, pod autoscaling, serving
lifecycle, or the inference data path.

## Architectural boundary

| Concern | Owner |
|---|---|
| Registration, capability inventory, exact-model eligibility, fleet policy, draining, failure-domain health | llm-d-fleet |
| Cluster-local serving lifecycle, revisions, readiness, local Gateway/Router resources | KServe when selected |
| Queue/KV/prefix/session scoring and endpoint selection | llm-d Router/EPP |
| Local pod scaling | KEDA/HPA; WVA optionally optimizes heterogeneous variants |
| Cross-cluster request forwarding | Selected data plane; Praxis validated, Router beta |
| OIDC, user login, MFA, user lifecycle | External Model/API Gateway and IdP |
| Governance, evidence, semantic classification, observations | Optional adapters; outside initial upstream acceptance |

## Candidate evidence

- One authoritative routing-provider interface with Praxis, llm-d Router, and
  disabled modes.
- Exact physical-model filtering before adapter publication; no adapter or
  caller may widen the eligible set.
- OpenAI-compatible gateway behavior with internal routing-header rejection.
- Trusted-gateway identity assertions using Ed25519 plus issuer, audience,
  lifetime, and verified-mTLS checks.
- KServe `serving.kserve.io/v1alpha2` rendering and current-generation ready
  status ingestion.
- Deterministic Router files, atomic replacement, last-valid retention,
  provider withdrawal tombstones, and optional pool-level Grid Signals.
- Portable contract, BDD, architecture, security, public-boundary, and
  disposable multi-cluster E2E coverage.
- Signed release workflow, SBOMs, provenance attestations, checksums, and
  multi-architecture artifacts.

## Important limitations

- Router v0.10.0 file discovery is IPv4-only and cannot carry DNS authority/SNI
  for verified cross-cluster TLS. Router support remains beta until a compatible
  released discovery contract is available and qualified.
- KServe behavior is unit-qualified but has not completed independent live
  cluster conformance.
- The reference GPU topology has one exact-model provider and therefore makes
  no GPU-HA claim.
- No independent penetration test, product security approval, clean-room
  reproduction, or production certification is claimed.
- Named clusters, credentials, raw telemetry, capacity data, and operational
  overlays are intentionally outside the public repository.

## Decisions requested

1. Is fleet eligibility/discovery a valid llm-d problem boundary?
2. Should the normalized discovery contract live in llm-d Router, an incubation
   repository, a KServe integration, or a multi-cluster Well-Lit Path?
3. Which fields should an upstream contract standardize: cluster identity,
   exact model, routing/metrics endpoints, TLS identity, health freshness,
   draining, failure domain, and capability status?
4. Should discovery be API/watch based rather than a watched-file bridge?
5. What minimum conformance suite would maintainers require before the Router
   adapter leaves beta?
6. Which KServe status and graceful-drain semantics should fleet consume rather
   than duplicate?
7. Who should join a focused technical follow-up with SIG Router/KServe?

## Suggested 45-minute agenda

1. 5 minutes — problem statement and ownership boundary.
2. 10 minutes — request/event flow and exact-model safety invariant.
3. 10 minutes — Router discovery gap and proposed upstream contract.
4. 5 minutes — KServe and KEDA/WVA alignment.
5. 5 minutes — portable evidence and explicit limitations.
6. 10 minutes — placement decision, owners, and next contribution slice.

## Reviewer reading order

1. [Review readiness and open gates](review-readiness.md)
2. [Product boundary](../architecture/product-boundary.md)
3. [Architecture overview](../architecture-diagram.md)
4. [Fleet eligibility and Router design](fleet-eligibility-router-design.md)
5. [Router release qualification](router-release-qualification.md)
6. [KServe qualification](../architecture/kserve-qualification.md)
7. [Trusted identity boundary](../architecture/trusted-identity-boundary.md)
8. [Threat model](../security/threat-model.md)
9. [Portable conformance result](multicluster-product-conformance-2026-08-31.md)
10. [Focused upstream proposal](proposal-fleet-level-multicluster-orchestration.md)
11. [RFC discussion text](rfc-discussion.md)
12. [Independent reproduction protocol](clean-room-reproduction.md)

## Desired meeting outcome

Leave with a named upstream home, one maintainer-reviewed contract scope, an
agreed first contribution, explicit reviewers, and a follow-up date. Keep
Praxis productization, governance/evidence systems, private deployment proof,
and generalized production claims outside that first contribution.
