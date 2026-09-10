# Upstream and product review readiness

## Review object

- Repository: <https://github.com/jkershawrh/llm-d-fleet>
- Published release: `v0.3.0` at
  `84f07898e82d3b9a37aa8cdfd61e9aa889bdb43b`
- Post-release review candidate: current `main`; reviewers must record the
  resolved commit SHA used for review and reproduction
- License: Apache License 2.0

The candidate is an OSS implementation for technical review. It is not a Red
Hat product commitment, production certification, security attestation, or new
tagged release.

## Ready for source review

| Area | Implemented evidence | Current boundary |
|---|---|---|
| Fleet ownership | Registration, exact-model eligibility, admission, placement constraints, health, draining, and failure-domain status | Does not replace KServe, EPP, KEDA, WVA, or a routing data plane |
| Routing-provider boundary | One authoritative provider: Praxis, llm-d Router, or disabled; adapters cannot expand the qualified set | Praxis is the validated default; Router remains beta |
| Trusted identity | Normalized identity plus short-lived Ed25519 trusted-gateway assertions; internal identity/routing headers are stripped | External OIDC, login, MFA, and user lifecycle belong to the selected gateway/IdP |
| KServe target | `serving.kserve.io/v1alpha2` rendering, server-side apply, observed-generation/readiness/address ingestion | Unit-qualified; live KServe conformance remains external |
| Router publication | Deterministic atomic files, exact-model filtering, stale/draining exclusion, withdrawal tombstones, and last-valid retention | Released Router v0.10.0 cannot represent DNS authority/SNI for this TLS path |
| Optional ecosystem | GCL, immutable ledger, DeepField, llm-d-sc, ModelPack, ModelPlane, and signals are optional and disabled in the community profile | Governed-evidence qualification is a separate downstream profile |
| Security baseline | Threat model, default-deny manifests, auth tests, dependency scans, SBOM/release workflows, and public-boundary checks | No independent penetration test or product security sign-off is claimed |
| Portable proof | Unit, BDD, contract, manifest, public-boundary, and disposable multi-cluster conformance assets | Named-cluster capacity and operational evidence remain private and non-portable |

## External gates still required

1. Run identity conformance through the selected production Model/API Gateway,
   including OIDC mapping, key rotation, revocation, clock skew, replay, and
   verified workload transport.
2. Run KServe reconciliation and graceful-drain conformance on a cluster with
   the pinned supported KServe release and CRDs.
3. Requalify the Router path after a released discovery contract can carry DNS
   endpoints, separate metrics endpoints, TLS authority/SNI, and reload
   semantics; then run the complete cross-cluster TLS and failure matrix.
4. Have an independent reviewer execute the clean-room reproduction protocol.
5. Complete formal architecture, product security, legal/licensing, support,
   upgrade/rollback, and penetration-test review before downstream product use.
6. Add a second compatible GPU failure domain before advertising GPU HA.

## Reviewer entry points

1. [Product boundary](../architecture/product-boundary.md)
2. [Architecture overview](../architecture-diagram.md)
3. [Trusted identity boundary](../architecture/trusted-identity-boundary.md)
4. [KServe qualification](../architecture/kserve-qualification.md)
5. [Fleet eligibility and Router design](fleet-eligibility-router-design.md)
6. [Router release qualification](router-release-qualification.md)
7. [Threat model](../security/threat-model.md)
8. [Portable conformance report](multicluster-product-conformance-2026-08-31.md)
9. [Evidence limitations](evidence-and-limitations.md)
10. [Clean-room reproduction](clean-room-reproduction.md)

## Release decision

Do not create a new release solely to start upstream review. Tag the next
candidate only after CI and security workflows pass on the exact revision,
portable artifacts are regenerated, documentation names the same contracts,
and any accepted review findings are resolved. Production-specific proof must
remain separate from the portable OSS release.
