# llm-d Router release qualification

Status as of 2026-09-10: **beta adapter implemented; released cross-cluster
TLS qualification blocked by the upstream discovery contract**.

## Released baseline

The latest llm-d Router release reviewed for this contract is
[`v0.10.0`](https://github.com/llm-d/llm-d-router/releases/tag/v0.10.0). Its
released
[`file-discovery`](https://github.com/llm-d/llm-d-router/blob/v0.10.0/docs/discovery.md)
plugin supports atomic watched-file reload, but requires every endpoint
`address` to be a literal IPv4 address. It does not provide a released field
for a distinct TLS authority/SNI name.

That is insufficient for the portable cross-cluster contract used here:

- OpenShift Routes, Gateway API listeners, meshes, and external gateways are
  commonly published as DNS names;
- backend certificate verification requires preserving that DNS identity;
- resolving the name to an IP and rewriting authority to the IP would break
  certificate verification or require an insecure override; and
- llm-d-fleet will not weaken TLS to force release compatibility.

The DNS-aware `multicluster-file-discovery` and Grid Signals plugins used by
the isolated beta overlay are therefore still pinned to an immutable upstream
`main` image digest. They are not described as released Router capability.

## Local conformance completed

The fleet adapter independently enforces:

- one homogeneous endpoint file per exact physical model;
- exclusion of unauthorized, stale, unavailable, incompatible, and draining
  providers before EPP scoring;
- deterministic ordering and atomic writes;
- separate routing and metrics endpoint metadata;
- TLS server-name, trust-reference, and authentication-reference metadata;
- last-valid state retention on generation/publication failure;
- explicit empty endpoint tombstones when a model is withdrawn; and
- a 900 KiB ConfigMap payload guard below Kubernetes' 1 MiB object limit.

Replacing the entire ConfigMap `data` field uses one JSON Patch operation so
removed keys cannot survive map-merge semantics.

## Upstream contract needed

A released discovery contract needs either:

1. DNS endpoint addresses with a separate dial address where required; or
2. explicit `address`, `authority`, and `tlsServerName` fields.

It must also retain atomic reload and deletion semantics. Once released, the
qualification gate is: digest pin, schema contract test, TLS/SNI preflight,
exact-model routing, stream/cancellation tests, provider withdrawal, and an
independent cross-cluster soak.

No production or live cross-cluster qualification is claimed by this report.
