# Air-Gap Image Mirror for fleet-llm-d

This overlay replaces all external container image references with a local
mirror registry, enabling deployment in disconnected (air-gapped) environments
such as sovereign cloud or classified networks.

## External Images

The following 6 images must be mirrored before deploying in an air-gapped
environment:

| Source Image | Component |
|---|---|
| `ghcr.io/jkershawrh/llm-d-fleet/fleet-controller:0.3.0` | Fleet control plane |
| `ghcr.io/jkershawrh/llm-d-fleet/fleet-agent:0.3.0` | Per-cluster data plane agent |
| `docker.io/library/postgres:16` | PostgreSQL state store |
| `docker.io/library/redis:7-alpine` | Redis cache (standalone overlay) |
| `registry.redhat.io/rhel9/postgresql-16:latest` | RHEL PostgreSQL (production) |
| `docker.io/vllm/vllm-openai:latest` | vLLM inference backend |

## Mirror Procedure

Replace `MIRROR` with your local registry (e.g., `registry.local:5000/fleet-llm-d`).

### Using `oc image mirror` (OpenShift)

```bash
MIRROR=registry.local:5000/fleet-llm-d

oc image mirror ghcr.io/jkershawrh/llm-d-fleet/fleet-controller:0.3.0 ${MIRROR}/fleet-controller:0.3.0
oc image mirror ghcr.io/jkershawrh/llm-d-fleet/fleet-agent:0.3.0 ${MIRROR}/fleet-agent:0.3.0
oc image mirror docker.io/library/postgres:16 ${MIRROR}/postgres:16
oc image mirror docker.io/library/redis:7-alpine ${MIRROR}/redis:7-alpine
oc image mirror registry.redhat.io/rhel9/postgresql-16:latest ${MIRROR}/postgresql-16:latest
oc image mirror docker.io/vllm/vllm-openai:latest ${MIRROR}/vllm-openai:latest
```

### Using `skopeo copy` (alternative)

```bash
MIRROR=registry.local:5000/fleet-llm-d

skopeo copy docker://ghcr.io/jkershawrh/llm-d-fleet/fleet-controller:0.3.0 docker://${MIRROR}/fleet-controller:0.3.0
skopeo copy docker://ghcr.io/jkershawrh/llm-d-fleet/fleet-agent:0.3.0 docker://${MIRROR}/fleet-agent:0.3.0
skopeo copy docker://docker.io/library/postgres:16 docker://${MIRROR}/postgres:16
skopeo copy docker://docker.io/library/redis:7-alpine docker://${MIRROR}/redis:7-alpine
skopeo copy docker://registry.redhat.io/rhel9/postgresql-16:latest docker://${MIRROR}/postgresql-16:latest
skopeo copy docker://docker.io/vllm/vllm-openai:latest docker://${MIRROR}/vllm-openai:latest
```

## Deployment

1. Mirror all images using the commands above.
2. Edit `kustomization.yaml` to replace `registry.local:5000/fleet-llm-d` with
   your actual mirror registry.
3. Build and apply:

```bash
kustomize build deploy/kustomize/overlays/airgap | kubectl apply -f -
```

## OpenShift ImageContentSourcePolicy

For OpenShift clusters, you can alternatively configure an `ImageContentSourcePolicy`
to transparently redirect image pulls:

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: ImageContentSourcePolicy
metadata:
  name: fleet-llm-d-mirror
spec:
  repositoryDigestMirrors:
    - source: ghcr.io/llm-d
      mirrors:
        - registry.local:5000/fleet-llm-d
```
