# llm-d-fleet community release

llm-d-fleet is an Apache-2.0 fleet-level inference control plane for multiple
llm-d deployments. This portable release contains the controller, agent,
OpenAI-compatible controller ingress, API definitions, Helm chart, generic
Kubernetes manifests, mock inference backend, and portable tests.

The source bundle also includes the optional `grid-signals-publisher`, a
platform-neutral mTLS service that converts local llm-d EPP metrics into an
allowlisted pool-level contract without publishing pod identities.

The community release is intentionally environment-neutral. It does not
contain cluster credentials, model weights, customer configuration, private
registry references, or the deployment overlays and evidence used by the
maintainer's physical test fleet.

## Quick start

Install with Helm after creating a unique cluster identity:

```sh
kubectl apply -k api/crds
kubectl create namespace fleet-llm-d
kubectl -n fleet-llm-d create configmap fleet-cluster-identity \
  --from-literal=cluster-id=community-cluster-01
helm upgrade --install fleet charts/fleet-llm-d \
  --namespace fleet-llm-d \
  --values charts/fleet-llm-d/values-standalone-dev.yaml
```

Alternatively, customize the generic Kustomize profile before applying it:

```sh
kubectl kustomize deploy/kustomize/overlays/community
```

See `docs/community/installation.md` for image and dependency configuration.
See `docs/community/release-boundary.md` for the exact distinction between the
portable artifact and environment material preserved in the full repository.
The chart path, Go module, binary names, and default Kubernetes namespace retain
`fleet-llm-d` as backward-compatible technical identifiers.
See `docs/README.md` for the authoritative documentation map and
`docs/upstream/reviewer-evidence-package.md` for the portable product claims,
limitations, release evidence, and clean-room reproduction protocol.

## Optional integrations

Praxis, llm-d Router, semantic classifiers, PostgreSQL, immutable evidence
ledgers, ModelPlane, and governance/event systems are integrations rather than
bundled services. Operators enable them explicitly and remain responsible for
their licenses, images, credentials, and lifecycle.

The included community PostgreSQL component is for evaluation only. Production
deployments should use an externally operated HA PostgreSQL service with TLS,
backup, and point-in-time recovery.
