# Independent clean-room reproduction protocol

## Objective

Allow a reviewer who has no access to the original deployment environment to
confirm the portable multi-cluster product claims from release `v0.3.0`.
Successful execution is independent product evidence; it is not production
certification of the reviewer's clusters.

## Required topology

- Three disposable Kubernetes or OpenShift clusters with distinct logical
  cluster and failure-domain identities.
- Two providers serving the same exact CPU model.
- One provider serving a different exact GPU model. A GPU-backed mock may be
  used for contract reproduction, but real accelerator execution must be
  stated explicitly before making performance claims.
- The fleet inference gateway and exactly one routing provider: `praxis` or
  `llm-d-router`.
- TLS trust and test credentials created by the reviewer. Do not copy
  credentials, hostnames, certificates, or overlays from another environment.

## Artifact verification

1. Download all assets from the [`v0.3.0` release](https://github.com/jkershawrh/llm-d-fleet/releases/tag/v0.3.0).
2. Verify the three archives against `checksums-sha256.txt`.
3. Confirm the source archive contains no deployment-specific overlay or
   credential material.
4. Verify image signatures, provenance, and SBOM attestations against the
   release repository identity before deployment.
5. Record the exact image digests, cluster versions, routing-provider version,
   and test-harness commit in the result.

## Local contract gate

From a clean checkout of the tagged source, run:

```sh
make test-portable-conformance
make test-contracts
make test-bdd
```

All optional ecosystem integrations must remain disabled for this gate. A
failure must be retained as evidence and investigated rather than omitted from
the final result.

## Multi-cluster execution

1. Register all three clusters with topology-neutral identities.
2. Publish the two CPU providers under one exact physical model and the GPU
   provider under a different exact physical model.
3. Confirm unauthenticated requests and caller-supplied internal routing
   headers cannot bypass gateway admission or fleet policy.
4. Send at least 50 bounded requests with a 90% CPU and 10% exact-GPU mix.
5. Confirm successful CPU execution reaches both CPU failure domains and every
   GPU request reaches only the exact GPU provider.
6. Remove each CPU provider independently. Confirm new requests converge to
   the surviving compatible provider within 30 seconds.
7. Remove the sole GPU provider. Confirm exact-GPU requests return structured
   `503 no_compatible_capacity` and never execute a CPU model.
8. Restore every provider and require consecutive successful health checks
   before counting it as eligible again.

Keep request volume below 50% of the lowest known safe backend capacity. This
protocol evaluates conformance and failover, not saturation performance.

## Required result fields

- Release tag, commit, artifact digests, and routing-provider version.
- Kubernetes/OpenShift versions and anonymized failure-domain topology.
- Request totals and status-code counts.
- Requested logical model, exact physical model, selected cluster, executing
  cluster, data plane, and request ID.
- Distribution by eligible provider.
- Provider removal/restoration timestamps and convergence duration.
- Wrong-model, wrong-cluster, malformed-response, authentication-bypass, and
  routing-header-bypass counts.
- Pod restarts and peak CPU/memory observations for components in scope.
- Every deviation, infrastructure interruption, and untested claim.

## Acceptance

- 100% of retained compatible requests execute the requested exact model.
- At least 99.9% success while compatible healthy capacity exists.
- Zero authorization, tenant, or internal-routing-header bypasses.
- CPU traffic reaches both eligible failure domains before disruption.
- CPU provider-loss convergence is at most 30 seconds.
- Sole GPU-provider loss produces only `503 no_compatible_capacity` for that
  exact model, with zero substitutions.
- Fleet decision, adapter output, backend execution identity, and request ID
  agree in the retained evidence.

Publish the result with topology-neutral provider names. Keep credentials,
private endpoints, customer identifiers, raw prompts, and environment-specific
deployment manifests outside the public evidence package.
