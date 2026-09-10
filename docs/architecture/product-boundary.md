# Product and upstream boundary

fleet-llm-d is the fleet eligibility and operations control plane. It owns
authenticated cluster registration, capability inventory, exact logical-to-
physical model resolution, tenant admission, placement constraints, health
freshness, draining, failure-domain status, and the eligible provider set.

It does not replace KServe model lifecycle, llm-d Router/EPP scoring, KEDA or
HPA actuation, WVA heterogeneous-variant optimization, model servers, or a
cross-cluster network product.

An external Model/API Gateway owns customer identity, OAuth/OIDC, API keys,
subscriptions, external RBAC, and commercial rate plans. llm-d-fleet consumes
a verified normalized identity and owns only fleet admission, placement
entitlements, internal authorization, and fleet quota enforcement. The
portable contract is documented in
[trusted-identity-boundary.md](trusted-identity-boundary.md).

## Routing adapters

One adapter is authoritative per deployment:

- `praxis` is the default and validated reference adapter. It preserves the
  existing `GridSite` and `InferenceProvider` contract.
- `llm-d-router` is the upstream-native beta. It writes one atomic watched
  endpoint file per exact model and an index that maps models to files. Each
  file is a homogeneous candidate set for one cluster-scoped EPP.
- `disabled` runs the control plane without publishing routing state.

The Router adapter excludes stale, draining, unavailable, unauthorized, and
model-incompatible providers before EPP scoring. Router retains queue, KV,
prefix, session, flow-control, and endpoint-selection ownership. HTTPS is
required unless `LLMD_ROUTER_ALLOW_INSECURE=true` is explicitly selected for
development.

## Endpoint transport adapters

The fleet core does not require OpenShift Routes. It qualifies a provider and
passes a normalized transport contract to the selected routing adapter:

- routing and pool-metrics URLs;
- TLS server identity and a reference to trust material;
- a reference to authentication material; and
- cluster, failure-domain, health, freshness, draining, and exact-model data.

An endpoint publisher may satisfy that contract with an OpenShift re-encrypt
Route, Kubernetes Gateway API, Multi-Cluster Services, a service mesh, or an
externally managed gateway. These are deployment choices: an endpoint
publisher cannot add a provider rejected by fleet policy, weaken TLS or
authentication requirements, or substitute a physical model.

The three-cluster reference deployment uses re-encrypt OpenShift Routes
because they are the validated Red Hat transport. Route objects, service-CA
annotations, and router-specific certificate wiring belong in OpenShift
overlays. The portable OSS core and community manifests remain usable without
the OpenShift API. Gateway API is the preferred Kubernetes-native portability
target; MCS and mesh integrations can be added when their operational contract
is required rather than embedded in the controller preemptively.

## Serving targets

`FleetInferencePool.spec.serving.target` defaults to `inferencePool`. The
additive `kserveLLMInferenceService` target renders a KServe
`LLMInferenceService`; KServe then owns the workload, revisions, readiness,
draining, Gateway, and local Router resources. If the selected cluster client
cannot apply KServe resources, reconciliation reports degraded state rather
than pretending the desired placement exists.

## Scaling ownership

KEDA over EPP metrics is the default local path for homogeneous pools. WVA is
optional for heterogeneous variants and emits desired replicas for KEDA/HPA.
`FleetScalingPolicy` supplies fleet budgets, placement bounds, and migration
constraints; it does not compete with those local autoscalers.

## Optional ecosystem

DeepField observations, GCL DecisionPackages, immutable-ledger evidence,
llm-d-sc classification, ModelPack, and ModelPlane are optional capabilities.
The community chart disables them by default. The existing `--production`
switch denotes the governed-evidence production contract and intentionally
requires GCL verification and an authenticated external ledger.

## Grid Signals

Pool-level Prometheus signals are optional. Membership and capabilities remain
in SWIM; dynamic load is polled over HTTPS. The implementation requires mTLS,
peer fingerprints, response bounds, Date-relative freshness, publisher-side
metric allowlists, and rejection of pod/container/instance/rank labels. Missing
signals reduce scoring quality but do not make a qualified provider unusable.
