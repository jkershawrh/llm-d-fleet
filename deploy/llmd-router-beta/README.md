# llm-d Router beta qualification

These values deploy one active EPP and one warm standby per exact physical
model. Upstream leader election keeps routing state authoritative on one EPP;
preferred pod anti-affinity spreads the pair when the cluster has multiple
nodes. Apply `epp-pdb.yaml` after the Helm releases to retain one member during
voluntary disruption. The EPPs consume the controller-owned
`fleet-router-endpoints` ConfigMap and do not expose a client proxy, so the
validated Praxis path remains unchanged during discovery and configuration
qualification.

The shadow EPPs scrape each provider's optional Grid Signals endpoint over
verified mTLS. The current direct OVMS and vLLM providers publish health only,
so this profile retains random selection within the fleet-qualified provider
set. Queue and KV scorers must not be enabled until cluster-local EPPs publish
those aggregates; missing load signals are not replaced with synthetic data.

The beta uses an upstream `main` EPP build by immutable digest because the
released v0.10.0 `file-discovery` contract accepts only literal IPv4 addresses
and cannot preserve the DNS authority/SNI required by verified cross-cluster
TLS endpoints. The pinned digest remains retrievable, but has not completed a
new live qualification in this release. Update it only through a new
qualification run; do not promote this overlay to a released Router channel
until a released discovery API carries the required TLS identity. See
[`docs/upstream/router-release-qualification.md`](../../docs/upstream/router-release-qualification.md).

```sh
helm upgrade --install fleet-router-cpu \
  oci://ghcr.io/llm-d/charts/llm-d-router-standalone \
  --version v0 --namespace fleet-llm-d \
  -f deploy/llmd-router-beta/values-cpu.yaml

helm upgrade --install fleet-router-gpu \
  oci://ghcr.io/llm-d/charts/llm-d-router-standalone \
  --version v0 --namespace fleet-llm-d \
  -f deploy/llmd-router-beta/values-gpu.yaml

oc apply -f deploy/llmd-router-beta/epp-pdb.yaml
```

`router-proxies.yaml` adds isolated CPU and GPU Envoy Services for inference
qualification. Each proxy uses the EPP as a fail-closed external processor,
removes caller-supplied destination headers before EPP processing, rewrites
authority from the EPP-selected endpoint, and verifies the selected Route with
dynamic SNI against the mounted `fleet-route-ca` bundle. The Services have no
Route; only the qualification gateway NetworkPolicy identity may call them.

Deploy an isolated gateway with the `llm-d-router` inference provider using
the generic Kustomize base and environment-owned Route, trust, and Secret
overlays. Environment-specific gateway manifests are intentionally excluded
from this repository. The production gateway and Praxis resources must remain
unchanged during qualification.

The proxy permits one retry for connection failures, resets, or refused
streams. Envoy retries only before response headers are returned; an active
stream is never replayed. The stock standalone Envoy profile remains
unsuitable because it sends plaintext upstream.

## Environment certification profile

Keep the traffic generator, ingress Route, certificate bundle, endpoint
mirror, image registry, and credentials in an environment-owned private
overlay. Publish only anonymized conformance summaries after reviewing them
for hostnames, addresses, identities, and workload data.
