# llm-d-fleet Kustomize deployment profiles

Resource names and the default `fleet-llm-d` namespace remain compatibility
identifiers; they do not represent a separate product edition.

The base deploys the fleet controller and per-cluster agent in the
`fleet-llm-d` namespace. The controller serves the OpenAI-compatible ingress
and forwards admitted requests to an explicitly configured external Praxis
endpoint. The overlays retain the existing standalone, hub, and federated
topologies; they do not install Praxis.

Kubernetes API mode enables Lease-based active/passive leader election. The
reference overlays retain a conservative single controller by default because
replica HA also requires shared PostgreSQL and external event/ledger services.

## Required cluster identity

`fleet-agent` refuses to start without a stable cluster identity. Create the
required ConfigMap before applying any profile; use an ID that is unique across
every cluster registered with the same control plane:

```sh
kubectl -n fleet-llm-d create configmap fleet-cluster-identity \
  --from-literal=cluster-id=us-central1-prod-01
kubectl apply -k deploy/kustomize/overlays/hub
```

The reference is deliberately non-optional. Packaging must not silently invent
a shared cluster ID for production clusters.

## Runtime ports

| Component | Port | Purpose |
| --- | ---: | --- |
| controller | 8080 | control-plane API and health probes |
| controller | 9091 | Prometheus metrics |
| agent | 8090 | local proxy, liveness, and fail-closed readiness probes |

The manifests expose only listeners the current binaries bind. Agent ports 8080
and 9090 remain reserved CLI contracts and are not rendered as Services. Agent
readiness stays false until synchronization and upstream forwarding exist.
This prevents scaffold processes from being counted as live provider evidence.

The base keeps the controller `ClusterIP`; expose its inference endpoints only
through an explicitly managed ingress, OpenShift Route, Gateway API object, or
LoadBalancer policy. The controller starts with the external ARE ledger disabled and without a
PostgreSQL or event-publisher endpoint. Production overlays should opt into
those services with operator-managed credentials and TLS endpoints rather than
guessed service addresses. The standalone overlay's PostgreSQL and Redis
resources are development conveniences, not production dependency defaults.

Validate all profiles with:

```sh
kubectl kustomize deploy/kustomize/base >/dev/null
kubectl kustomize deploy/kustomize/overlays/standalone >/dev/null
kubectl kustomize deploy/kustomize/overlays/hub >/dev/null
kubectl kustomize deploy/kustomize/overlays/federated >/dev/null
```

The `llmd-router-endpoints` component switches the authoritative routing
adapter and publishes the qualified endpoint files into the
`fleet-router-endpoints` ConfigMap. Router EPP replicas mount that ConfigMap
with `watchFile: true`; Kubernetes projected-volume swaps preserve the last
valid data while the controller publishes a complete replacement. The
`hubcluster-router-beta` overlay is intentionally separate from the validated
Praxis overlay so qualification can roll back by reapplying `hubcluster`.
