# llm-d-fleet Helm chart

The chart directory and Kubernetes namespace retain the `fleet-llm-d` name for
backward compatibility. The public project and product name is `llm-d-fleet`.

This chart deploys the controller and per-cluster agent as separate workloads.
The controller hosts the OpenAI-compatible ingress and forwards accepted
requests to external Praxis. The default `standalone-dev` profile deploys both.
It does not
install PostgreSQL, Redis, Kafka, ARE, GCL, DeepField,
ModelPlane, or Prometheus. Stateful and actuation integrations are disabled by
default and must be enabled with explicit endpoints and operator-managed
credentials. The controller still retains its compatibility fallback URLs for
read-only semantic/platform metrics calls unless explicit URLs are supplied.

## Install

Create a stable identity that is unique across the fleet, then choose a profile:

```sh
kubectl apply -k api/crds
kubectl create namespace fleet-llm-d
kubectl -n fleet-llm-d create configmap fleet-cluster-identity \
  --from-literal=cluster-id=us-central1-prod-01
helm upgrade --install fleet charts/fleet-llm-d \
  --namespace fleet-llm-d \
  --values charts/fleet-llm-d/values-hub.yaml
```

The current chart does not yet bundle CRDs under `crds/`; install the pinned
CRDs from this repository first as shown above. Until those definitions are
generated into the release artifact and drift-checked, the chart is not a
self-contained fresh-install package.

The supported profiles are:

| Values file | Workloads | Intended use |
| --- | --- | --- |
| `values-standalone-dev.yaml` | controller, agent | Local/development packaging; external governance and durable state are disabled |
| `values-hub.yaml` | one controller | Central control-plane packaging; production dependencies and Praxis must be configured explicitly |
| `values-spoke.yaml` | agent only | Managed cluster connected to an external hub control plane |
| `values-federated-hub.yaml` | one controller peer | One peer per installation; federation does not imply controller HA |

The controller uses Kubernetes Lease election whenever `--kube-api` is set.
Profiles remain conservative at one replica because HA also requires shared
PostgreSQL and external event/ledger backends. Operators may scale the
controller after configuring those shared dependencies.

Profiles do not inject a shared placeholder cluster ID. You can set
`clusterIdentity.clusterId` directly instead of creating the ConfigMap. A spoke
also requires either `agent.controlPlaneURL` or this non-optional ConfigMap:

```sh
kubectl -n fleet-llm-d create configmap fleet-control-plane \
  --from-literal=url=https://fleet-controller.example.com
```

Set `agent.advertisedHealthURL` to the agent proxy `/readyz` endpoint and
`agent.advertisedInferenceURL` to its base URL as reachable from the hub
gateway. `agent.upstreamURL` identifies the local llm-d EPP that receives the
forwarded request.

## Ports

| Component | Port | Purpose |
| --- | ---: | --- |
| controller | 8080 | control-plane API and health probes |
| controller | 9091 | Prometheus metrics |
| controller | 9092 | optional JSON-RPC listener |
| agent | 8090 | local proxy, liveness, and fail-closed readiness probes |

The package exposes only listeners the current binaries bind. Agent ports 8080
and 9090 remain reserved CLI contracts and are not rendered as Services. Agent
readiness follows the configured upstream EPP.

The default controller Service is `ClusterIP`. Add an OpenShift Route or a
deliberate ingress/load-balancer policy to expose its inference endpoints.
Agent Services remain internal, and Praxis is an external dependency.

## External dependencies

- GCL DecisionPackage admission is disabled until `externalDependencies.gcl`
  names an existing Secret containing GCL's Ed25519 public verification key
  and its key ID. GCL retains the private key; fleet verifies producer
  authorship before creating a FleetIntent. The `secretKey` value name is a
  backward-compatible chart identifier and must never contain GCL's private
  key.
- The v2 production ingress accepts verified GCL DecisionPackage CloudEvents.
  Plain `application/json` v2 intents are disabled by default because their
  provenance is self-asserted. Set `controller.allowOperatorJSONIntents=true`
  only for explicit development/operator compatibility testing; the equivalent
  binary flag is `--allow-operator-json-intents` and the direct-process escape
  hatch is `FLEET_ALLOW_OPERATOR_JSON_INTENTS=true`.
- Standalone immutable-ledger mode defaults to `disabled`. The currently
  packaged `http` compatibility mode requires an explicit endpoint. Hub and
  federated-hub profiles additionally require HTTPS and an existing Secret
  containing the gateway bearer token. `memory` is development/test evidence
  only. The ledger-owned gRPC API remains canonical, but this binary does not
  advertise it until a generated Go client is shipped.
- PostgreSQL is disabled and, when enabled, reads the full connection URL from
  an existing Secret. No password appears in values or rendered arguments.
- The event publisher and ModelPlane adapter require explicit endpoints.
- Semantic-classifier and platform-metrics URLs are only added when supplied.
- Authentication never generates a Secret. `auth.provider=hmac` is the
  compatibility/development path and requires the named existing Secret.
  `auth.provider=trusted-proxy` consumes an operator-managed Secret containing
  only the trusted gateway's Ed25519 public-key JSON plus explicit issuer and
  audience values. OAuth/OIDC and API-key lifecycle remain gateway-owned. See
  `docs/architecture/trusted-identity-boundary.md` for the wire contract.

Run `helm lint charts/fleet-llm-d` and template each profile before promotion.
