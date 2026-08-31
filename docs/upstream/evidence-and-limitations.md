# Upstream experience report: evidence and limitations

This appendix supports the RFC and proposal. It is not a certification of the
reference environment. The dated portable product result is in
`multicluster-product-conformance-2026-08-31.md`.

## Current-release behaviors exercised

- OpenAI-compatible chat and completion requests enter through the fleet
  inference gateway.
- Compatible CPU requests can be distributed between independent CPU failure
  domains.
- A GPU model alias and canonical physical model route exactly to the eligible
  GPU provider.
- A session can escalate from SIMPLE CPU placement to REASONING GPU placement
  using llm-d-sc as an optional semantic classification signal.
- Caller-provided internal routing headers do not override the gateway's
  decision.
- Unauthenticated requests are rejected.
- Removing compatible GPU health produces an explicit
  `503 no_compatible_capacity`; the request is not rewritten to the CPU model.
- Routing and execution metadata can be correlated with authenticated external
  evidence records.

Relevant local tests and operational definitions include:

- `pkg/server/handlers_inference_test.go`
- `test/e2e/e2e_test.go`
- `deploy/prometheus/production-alert-rules.yaml`

## Availability statement

The CPU model has providers in two cluster failure domains. A single-provider GPU model
must remain advertised as `degraded/non-HA`
until a second compatible GPU provider is added.

## Evidence that requires qualification

The repository contains longer soak and performance results from earlier
deployment revisions. Some used NodePort transport, a different hub cluster,
single-replica components, or in-memory dependencies. They are useful
implementation experience but must not be represented as certification of the
current Route-based HA release.

## Gates beyond portable product proof

- Reproduce the conformance suite in an independent, topology-neutral testbed.
- Complete formal product security review.
- Validate external HA PostgreSQL and immutable evidence only for a governed
  downstream profile; neither is a prerequisite for the portable OSS core.
- Add a second compatible GPU provider before advertising GPU model HA.
- Keep environment certification separate from product conformance claims.

## Completed release-integrity gate

Release `v0.3.0` publishes the exact source and conformance revision with
signed multi-architecture images, binary archives, a portable source archive,
CycloneDX SBOMs, provenance attestations, and SHA-256 checksums. CI, security,
and disposable three-cluster Kind E2E gates passed for the tagged commit. This
closes artifact provenance as a release gate; independent clean-room execution
and formal product security review remain open product-proof work.

## Router v0.10 compatibility work

- Render the fleet provider registry into the multi-cluster plugin's watched
  endpoint-file format.
- Verify Route hostnames, separate metrics endpoints, CA trust, and mTLS with
  `multicluster-metrics-data-source`.
- Map fleet health and draining transitions to endpoint removal and restoration
  while retaining the under-30-second convergence target.
- Compare Router queue/KV/prefix/session scoring with the current Praxis path.
- Confirm exact-model filtering occurs before EPP scoring and that sole GPU
  provider loss still returns `503 no_compatible_capacity`.
- Treat the integration as experimental because all v0.10 multi-cluster
  plugins are Alpha and require `--allow-experimental-plugins`.

## Submission hygiene

- Reproduce externally quoted measurements from retained result artifacts.
- Remove customer-identifying or confidential material.
- Use fresh, tightly scoped commits with Developer Certificate of Origin
  sign-off (`git commit -s`).
- Keep experimental behavior disabled by default and label it experimental.
- Submit tests, APIs, and implementation in separate reviewable changes after
  proposal approval.
