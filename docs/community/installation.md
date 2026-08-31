# Community installation

The community profile is a portable starting point for Kubernetes. External
systems are disabled by default, and the package does not install model weights,
an inference engine, a semantic classifier, or an immutable ledger.

## Images

Release automation publishes signed controller and agent images to the GitHub
Container Registry namespace belonging to the repository that creates the
release. For the v0.3.0 public release, use:

```sh
helm upgrade --install fleet charts/fleet-llm-d \
  --namespace fleet-llm-d \
  --set clusterIdentity.clusterId=community-cluster-01 \
  --set controller.image.repository=ghcr.io/jkershawrh/llm-d-fleet/fleet-controller \
  --set controller.image.tag=0.3.0 \
  --set agent.image.repository=ghcr.io/jkershawrh/llm-d-fleet/fleet-agent \
  --set agent.image.tag=0.3.0
```

For Kustomize, replace the two image names in
`deploy/kustomize/overlays/community/kustomization.yaml` with the release
repository and tag before applying the overlay.

The community overlay omits Prometheus Operator `ServiceMonitor` objects so it
can be applied to vanilla Kubernetes. The Prometheus rules and dashboards are
included as optional observability assets; install their operator-specific
resources only when the corresponding CRDs are available.

## Durable state

The default chart keeps PostgreSQL disabled. Set
`externalDependencies.postgres.enabled=true` and supply an existing Secret
containing the full TLS connection URL for a durable deployment.

The optional `community-postgres` Kustomize component uses the upstream
PostgreSQL image and is intended only for evaluation. It deliberately requires
the operator to create the password Secret instead of embedding credentials.

## Inference

Configure each agent's `upstreamURL` to a local llm-d-compatible inference
entry point. The included mock backend is for contract testing only. Clients
call the controller's `/v1/chat/completions` or `/v1/completions` endpoint; the
controller performs admission and forwards the request to the configured
Praxis endpoint. Praxis and llm-d Router adapters are optional; neither is
embedded in the core images.

Praxis is the default and currently validated adapter. Select the upstream-
native beta with `--set controller.routingProvider=llm-d-router`; mount the
generated endpoint directory into one model-specific hub EPP and configure its
`multicluster-file-discovery` plugin with `watchFile: true`. Use one hub EPP per
exact model until upstream defines a multi-model discovery/filter contract.

The inference gateway never assumes physical cluster names. Configure the
authoritative exact-model provider set with `FLEET_MODEL_PROVIDERS_JSON`, for
example `{"model-a":["cluster-a","cluster-b"],"model-b":["cluster-c"]}`.
Every provider ID must correspond to registered fleet state and configured
health/routing evidence. Missing mappings fail closed with
`503 no_compatible_capacity`; a provider mapped to one physical model cannot
execute another model.

Semantic classification is also optional. The release contains the classifier
client contract but does not distribute classifier model weights or an
unpublished classifier server.

## Optional Grid Signals publisher

`grid-signals-publisher` is a portable sidecar or standalone service for a
cluster-local llm-d EPP. It scrapes a local Prometheus endpoint and exposes
only the pool-level queue, KV-cache utilization, ready-endpoint, and saturation
signals used by fleet and the multi-cluster Router plugins. All source labels
are discarded before publication.

When a serving implementation does not expose compatible EPP metrics, an
operator may configure `GRID_SIGNALS_HEALTH_URL`. A successful active health
probe then publishes exactly one ready provider. This fallback does not invent
queue, KV-cache, or saturation values; those signals remain absent until a
compatible source is installed.

The publisher requires a server certificate, a client CA, and an explicit
SHA-256 allowlist of client certificate fingerprints. It refuses to start if
any of those identity controls are absent. Its `/metrics` listener uses TLS
1.3 and requires a verified client certificate; a separate loopback health
listener is available for container probes.

The binary has no OpenShift dependency. On OpenShift, expose it with a
passthrough Route so the publisher—not the ingress router—performs client
certificate authentication. On portable Kubernetes, use Gateway API, a
LoadBalancer, a service mesh, or another transport that preserves end-to-end
TLS. Do not use an edge- or re-encrypt-terminated ingress for this mTLS
endpoint unless an authenticated client identity is securely propagated and
verified by an approved intermediary.

## Production boundary

Production deployments require operator-managed authentication, TLS, durable
state, topology
spread, disruption budgets, capacity testing, and failure certification. The
community profile is not a claim that those dependencies have been supplied.
The binary's existing `--production` switch is specifically the governed-
evidence production contract and additionally requires verified GCL
DecisionPackages and an authenticated external immutable ledger.
