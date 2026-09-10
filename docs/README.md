# llm-d-fleet documentation

This index defines which documents describe the current OSS contract and which
are retained only as design history or sanitized downstream experience. When
documents disagree, the current-contract set below is authoritative.

## Current OSS contract

- [Repository README](../README.md) — project purpose, architecture, APIs, and
  release maturity.
- [Product boundary](architecture/product-boundary.md) — responsibilities of
  fleet, routing providers, KServe, EPP, KEDA, and optional integrations.
- [Trusted identity boundary](architecture/trusted-identity-boundary.md) —
  portable Model/API Gateway identity and header-authority contract.
- [KServe qualification](architecture/kserve-qualification.md) — current
  serving-target API, status ingestion, and evidence limitations.
- [Architecture overview](architecture-diagram.md) — current component and
  request-flow overview.
- [Deployment profiles](community/deployment-profiles.md) — portable core,
  scale/HA, and optional governed-evidence profiles.
- [Community installation](community/installation.md) — installation and
  dependency boundary.
- [Repository boundary](community/repository-boundary.md) and
  [release boundary](community/release-boundary.md) — public/private content
  and portable source-package rules.
- [OpenAPI contract](../api/openapi/fleet-api.yaml) and CRDs under
  [`api/crds`](../api/crds) — executable public interfaces.

## Upstream and product review

- [Reviewer evidence package](upstream/reviewer-evidence-package.md)
- [Fleet eligibility and Router design](upstream/fleet-eligibility-router-design.md)
- [Router release qualification](upstream/router-release-qualification.md) —
  released discovery compatibility, local evidence, and upstream blocker.
- [Multi-cluster conformance report](upstream/multicluster-product-conformance-2026-08-31.md)
- [Independent clean-room protocol](upstream/clean-room-reproduction.md)
- [Focused RFC discussion](upstream/rfc-discussion.md)

## Current technical references

- [OSS whitepaper](whitepaper/llm-d-fleet-whitepaper.md) — normalized
  topology-neutral architecture, evidence, and limitation statement.
- [Production test model](production-test-plan.md) — reusable test stages and
  acceptance criteria, not certification of a particular installation.
- [Security documents](security/README.md) — control mappings and review procedures;
  mappings are implementation aids, not third-party certifications.
- [Praxis alignment](architecture/praxis-alignment.md) — historical Red Hat
  architecture review with a current responsibility-boundary note.
- [Praxis migration record](architecture/praxis-integration.md) — retained
  migration history; not the current installation guide.

## Historical and downstream reference material

Documents under `benchmarks/`, `customer-patterns/`, `demo/`, `presentation/`,
selected files under `proposals/`, and generated whitepaper exports preserve
sanitized experience from earlier revisions. Their status banners define
their scope. They are excluded from the portable OSS source archive and must
not be cited as certification of `v0.3.0`.

Environment-specific manifests, credentials, raw telemetry, capacity results,
and operational certification belong in a separate private deployment
repository. Do not add them here.
