# KServe serving-target qualification

The optional `kserveLLMInferenceService` target delegates cluster-local model,
workload, Router, revision, and readiness lifecycle to KServe. Fleet chooses
eligible clusters and reconciles one KServe resource per selected cluster; it
does not create KServe's child Deployments, LeaderWorkerSets, Services,
InferencePools, Gateways, HTTPRoutes, or EPP workloads.

## Implemented contract

The current adapter renders `serving.kserve.io/v1alpha2`
`LLMInferenceService` resources and reconciles them with Kubernetes
server-side apply using the fixed `llm-d-fleet` field manager. This follows the
current [upstream KServe API](https://github.com/kserve/kserve/blob/master/pkg/apis/serving/v1alpha2/llm_inference_service_types.go).

Fleet reads these KServe-owned status fields:

- `metadata.generation`;
- `status.observedGeneration`;
- `status.conditions[type=Ready]`;
- `status.url`; and
- the first usable `status.addresses[].url` fallback.

A selected cluster becomes observed fleet capacity only when KServe reports
`Ready=True`, status matches the current generation, and KServe publishes an
endpoint. Missing CRDs, API errors, malformed status, stale generation,
unknown readiness, and missing addresses remain degraded/unavailable. Desired
placement is never reported as actual capacity.

`FleetInferencePool.spec.serving.kserve.criticality` remains fleet policy
metadata and is emitted as an annotation; it is not injected into KServe's
model schema.

## Compatibility and evidence

The existing `inferencePool` serving target remains the default and its
resource behavior is unchanged. KServe support is additive and beta because
the upstream API is still alpha.

Unit and contract tests validate deterministic rendering, create/update
semantics, status parsing, stale-generation rejection, endpoint fallback, and
missing-API failure. No live KServe cluster conformance is claimed by the OSS
test suite. That proof requires an independent environment running a declared
KServe release and is tracked as a release qualification gate.
