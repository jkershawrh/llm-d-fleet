# llm-d-fleet reviewer evidence package

## Purpose

This index is the shortest path for an upstream or product reviewer to assess
the portable `llm-d-fleet` proposition. It covers reusable product behavior,
not the certification of any named deployment environment.

## Review sequence

1. Read the [product boundary](../architecture/product-boundary.md) to see what
   fleet owns and what remains with llm-d Router, EPP, KServe, and autoscaling.
2. Read the [fleet eligibility and Router design](fleet-eligibility-router-design.md)
   for the normalized provider contract and policy/adapter separation.
3. Read the [focused upstream proposal](proposal-fleet-level-multicluster-orchestration.md)
   and [RFC discussion text](rfc-discussion.md).
4. Review the [portable multi-cluster conformance result](multicluster-product-conformance-2026-08-31.md).
5. Review [evidence limitations](evidence-and-limitations.md), especially the
   single-provider GPU HA limitation and the absence of production claims for
   the disposable reference testbed.
6. Reproduce the result using the [clean-room protocol](clean-room-reproduction.md).

## Release under review

- Repository: <https://github.com/jkershawrh/llm-d-fleet>
- Release: <https://github.com/jkershawrh/llm-d-fleet/releases/tag/v0.3.0>
- Source commit: `84f07898e82d3b9a37aa8cdfd61e9aa889bdb43b`
- License: Apache License 2.0

The release includes Linux AMD64 and ARM64 binaries, a portable OSS source
archive, signed multi-architecture controller and agent images, CycloneDX
SBOMs, provenance attestations, and SHA-256 checksums. The tagged revision
passed CI, security, public-boundary checks, portable conformance, and a
disposable three-cluster Kind E2E workflow.

## Claims supported by evidence

- Fleet policy qualifies providers before data-plane endpoint selection.
- Exact physical-model compatibility is authoritative and cannot be weakened
  by an adapter or caller-controlled routing header.
- CPU providers in separate failure domains can distribute traffic and fail
  over within the defined convergence objective.
- Loss of the sole compatible GPU provider returns structured
  `503 no_compatible_capacity` without CPU-model substitution.
- Praxis and llm-d Router fit behind a single provider-publication boundary;
  adapters translate qualified state but do not own fleet policy.
- Optional governance, evidence, semantic-classification, and observation
  integrations can be absent without opening an authorization bypass.

## Claims deliberately not made

- The reference infrastructure is not certified for production.
- A model with only one compatible GPU provider is not highly available.
- Router queue, KV, prefix, cost, carbon, or latency optimization is not
  claimed without genuine fresh signals.
- The OSS core does not include or require a private deployment, external
  ledger, observation system, classifier model, or customer configuration.
- Release provenance is not a substitute for independent reproduction or a
  formal product security review.

## Remaining product-proof gates

1. Independent clean-room execution by a reviewer who did not build the
   reference environment.
2. Formal product security review and threat-model sign-off.
3. A second compatible GPU failure domain before making a GPU-HA claim.
4. Governed-profile dependency qualification only if that optional downstream
   profile is proposed as part of a product offering.

Environment-specific capacity, soak, credential, hostname, and operational
evidence belongs in a separate private deployment repository and is not needed
to evaluate this OSS product boundary.
