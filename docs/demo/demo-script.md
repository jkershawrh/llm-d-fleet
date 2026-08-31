# Historical llm-d-fleet demo script

> **Historical demo script.** Commands, topology, and component descriptions
> may reflect an earlier revision. Do not use this as a `v0.3.0` installation
> or conformance procedure.

**Duration:** 15 minutes
**Audience:** Field engineers, customer stakeholders, leadership

---

## Prerequisites

- OpenShift cluster with RHOAI 3.4+ (minimum 3 worker nodes, each with at least 1 NVIDIA A100 or H100 GPU)
- `oc` CLI logged in as cluster-admin
- `fleetctl` binary installed (from `bin/` in this repo)
- `grpcurl` installed (for compliance ledger queries)
- ARE Immutable Ledger deployed (shared infrastructure, accessible on gRPC port 9092)
- At least 2 managed clusters registered in ACM or accessible via kubeconfig

---

## Demo Flow

### 1. Deploy fleet-llm-d (2 min)

**Talking points:**

- fleet-llm-d is a fleet-level inference orchestration platform built on llm-d.
- It extends single-cluster llm-d to manage model serving across an entire fleet of OpenShift clusters.
- Deployment is a single command that installs the CRDs, controller, and agent components. Praxis AI handles inference routing as the data plane.

**Commands:**

```bash
# Deploy fleet-llm-d to the hub cluster
./hack/deploy-demo.sh \
  --cluster-url https://api.hub.demo.openshiftapps.com:6443 \
  --token sha256~xK9mR2vLpQwN7tBjYcHfZsA3dE6gU8iO1kM4nP5rT
```

**Expected output:**

```
[INFO]  Deploying fleet-llm-d v0.1.0 to hub cluster...
[INFO]  Creating namespace fleet-llm-d
namespace/fleet-llm-d created
[INFO]  Installing CRDs...
customresourcedefinition.apiextensions.k8s.io/fleetinferencepools.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/placementpolicies.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/fleetroutingpolicies.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/tenantprofiles.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/fleetscalingpolicies.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/modellifecycles.fleet.llm-d.ai created
customresourcedefinition.apiextensions.k8s.io/kvcachetransferpolicies.fleet.llm-d.ai created
[INFO]  Deploying fleet-controller...
deployment.apps/fleet-controller created
service/fleet-controller created
[INFO]  Deploying Praxis AI gateway...
deployment.apps/praxis-gateway created
service/praxis-gateway created
route.route.openshift.io/praxis-gateway created
[INFO]  Waiting for controller to become ready...
deployment.apps/fleet-controller condition met
[INFO]  fleet-llm-d deployed successfully.
[INFO]  Gateway endpoint: https://praxis-gateway-fleet-llm-d.apps.hub.demo.openshiftapps.com
```

```bash
# Verify the deployment
oc get pods -n fleet-llm-d
```

**Expected output:**

```
NAME                                READY   STATUS    RESTARTS   AGE
fleet-controller-6b8f9d4c57-xr2kl   1/1     Running   0          42s
praxis-gateway-7c4a1e5d38-mv9pn     1/1     Running   0          38s
```

```bash
# Check controller health
curl -s http://localhost:8080/healthz && echo
curl -s http://localhost:8080/readyz && echo
```

**Expected output:**

```
ok
ok
```

> **What to highlight:** One command deploys the entire platform. The controller and gateway are running and healthy in under a minute. This is production-grade infrastructure, not a toy.

**Transition:** "Now that the platform is running, let's register the clusters we want to orchestrate across."

---

### 2. Register Clusters (2 min)

**Talking points:**

- fleet-llm-d manages inference workloads across multiple clusters.
- Each cluster is registered with its API endpoint and credentials.
- The controller discovers available GPU capacity on each cluster automatically.
- This is where fleet-level orchestration begins -- you are no longer managing one cluster at a time.

**Commands:**

```bash
# Register the first spoke cluster (US-East, 8x A100 nodes)
fleetctl cluster register \
  --name us-east-gpu-01 \
  --api-url https://api.us-east-01.demo.openshiftapps.com:6443 \
  --token sha256~aB3cD4eF5gH6iJ7kL8mN9oP0qR1sT2uV \
  --region us-east-1 \
  --labels gpu-type=a100,tier=production
```

**Expected output:**

```
Cluster "us-east-gpu-01" registered successfully.
  Region:     us-east-1
  GPU Type:   NVIDIA A100 (8 available)
  Status:     Ready
  Endpoint:   https://api.us-east-01.demo.openshiftapps.com:6443
```

```bash
# Register the second spoke cluster (EU-West, 4x H100 nodes)
fleetctl cluster register \
  --name eu-west-gpu-01 \
  --api-url https://api.eu-west-01.demo.openshiftapps.com:6443 \
  --token sha256~wX3yZ4aB5cD6eF7gH8iJ9kL0mN1oP2qR \
  --region eu-west-1 \
  --labels gpu-type=h100,tier=production
```

**Expected output:**

```
Cluster "eu-west-gpu-01" registered successfully.
  Region:     eu-west-1
  GPU Type:   NVIDIA H100 (4 available)
  Status:     Ready
  Endpoint:   https://api.eu-west-01.demo.openshiftapps.com:6443
```

```bash
# Register a third cluster (US-West, 4x A100 nodes)
fleetctl cluster register \
  --name us-west-gpu-01 \
  --api-url https://api.us-west-01.demo.openshiftapps.com:6443 \
  --token sha256~sT3uV4wX5yZ6aB7cD8eF9gH0iJ1kL2mN \
  --region us-west-2 \
  --labels gpu-type=a100,tier=production
```

**Expected output:**

```
Cluster "us-west-gpu-01" registered successfully.
  Region:     us-west-2
  GPU Type:   NVIDIA A100 (4 available)
  Status:     Ready
  Endpoint:   https://api.us-west-01.demo.openshiftapps.com:6443
```

```bash
# List all registered clusters
fleetctl cluster list
```

**Expected output:**

```
NAME              REGION      GPU TYPE   GPU AVAIL   STATUS   AGE
us-east-gpu-01    us-east-1   A100       8           Ready    1m
eu-west-gpu-01    eu-west-1   H100       4           Ready    45s
us-west-gpu-01    us-west-2   A100       4           Ready    20s
```

> **What to highlight:** Three clusters, two regions, two GPU types -- all managed from a single control plane. The controller already knows the GPU capacity of each cluster. This is fleet-level visibility that does not exist in vanilla Kubernetes.

**Transition:** "With our clusters registered, let's deploy a model and see how fleet-llm-d decides where to place it."

---

### 3. Deploy Model (3 min)

**Talking points:**

- A FleetInferencePool defines the model you want to serve and the constraints for placement.
- The controller evaluates available capacity, GPU type requirements, and placement policies to decide where the model runs.
- We use ModelPack (from the CNCF model-spec) for OCI model metadata, so the controller knows the model's resource requirements before placing it.
- The placement decision is automatic -- you declare intent, the platform handles the rest.

**Commands:**

```bash
# Apply the FleetInferencePool for Granite 3.2
cat <<'EOF' | oc apply -f -
apiVersion: fleet.llm-d.ai/v1alpha1
kind: FleetInferencePool
metadata:
  name: granite-3-2-8b
  namespace: fleet-llm-d
spec:
  model:
    name: ibm-granite/granite-3.2-8b-instruct
    source:
      modelPack:
        registry: quay.io/modh
        tag: latest
  replicas: 2
  placement:
    strategy: BestFit
    constraints:
      gpuType: ["a100", "h100"]
      minGPUs: 1
      regions: ["us-east-1", "us-west-2"]
  serving:
    engine: vllm
    maxModelLen: 8192
    tensorParallelSize: 1
EOF
```

**Expected output:**

```
fleetinferencepool.fleet.llm-d.ai/granite-3-2-8b created
```

```bash
# Watch the placement decision
oc get fleetinferencepool granite-3-2-8b -n fleet-llm-d -o yaml | grep -A 20 "status:"
```

**Expected output:**

```yaml
status:
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2026-07-06T14:23:18Z"
    reason: ReplicasAvailable
    message: "2/2 replicas are serving"
  placements:
  - cluster: us-east-gpu-01
    region: us-east-1
    gpuType: a100
    gpusAllocated: 1
    replica: granite-3-2-8b-0
    status: Serving
    endpoint: http://granite-3-2-8b-0.us-east-gpu-01.svc:8000
  - cluster: us-west-gpu-01
    region: us-west-2
    gpuType: a100
    gpusAllocated: 1
    replica: granite-3-2-8b-1
    status: Serving
    endpoint: http://granite-3-2-8b-1.us-west-gpu-01.svc:8000
  totalGPUsAllocated: 2
  availableReplicas: 2
```

```bash
# Check the placement in a compact view
fleetctl pool status granite-3-2-8b
```

**Expected output:**

```
FleetInferencePool: granite-3-2-8b
  Model:    ibm-granite/granite-3.2-8b-instruct
  Engine:   vllm
  Status:   Ready (2/2 replicas serving)

  REPLICA              CLUSTER            REGION      GPU     STATUS
  granite-3-2-8b-0     us-east-gpu-01     us-east-1   A100    Serving
  granite-3-2-8b-1     us-west-gpu-01     us-west-2   A100    Serving
```

> **What to highlight:** The platform chose to spread replicas across two regions automatically using BestFit placement. It respected our constraints (A100 or H100, US regions only). The model is serving on both clusters within seconds. No one had to SSH into a node or write a HelmRelease per cluster.

**Transition:** "The model is live. Let's send some inference requests through the fleet inference path."

---

### 4. Send Inference (3 min)

**Talking points:**

- The Praxis AI gateway is the programmable inference data plane that handles cross-cluster inference routing.
- Clients send requests to a single endpoint. The gateway decides which backend serves the request.
- The EPP (Endpoint Picker Protocol) headers control routing behavior: `x-llm-d-inference-objective` selects realtime vs. batch, and `x-llm-d-inference-fairness-id` enables fair queuing across tenants.
- All of this is transparent to the caller -- they just see an OpenAI-compatible API.

**Commands:**

```bash
# Set the gateway endpoint
export FLEET_GATEWAY="https://praxis-gateway-fleet-llm-d.apps.hub.demo.openshiftapps.com"

# Send a realtime inference request
curl -s -w "\n\n--- Response Headers ---\n" \
  -D /dev/stderr \
  "${FLEET_GATEWAY}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "x-llm-d-inference-objective: realtime" \
  -H "x-llm-d-inference-fairness-id: tenant-acme" \
  -d '{
    "model": "ibm-granite/granite-3.2-8b-instruct",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Explain Kubernetes operators in two sentences."}
    ],
    "max_tokens": 128,
    "temperature": 0.7
  }' 2>&1
```

**Expected output:**

```
HTTP/2 200
content-type: application/json
x-llm-d-served-by: granite-3-2-8b-0
x-llm-d-served-cluster: us-east-gpu-01
x-llm-d-served-region: us-east-1
x-llm-d-routing-decision: latency-optimized
x-llm-d-queue-time-ms: 2
x-llm-d-inference-time-ms: 847

{
  "id": "chatcmpl-9f2a1b3c4d5e6f7a8b9c0d1e",
  "object": "chat.completion",
  "created": 1751808198,
  "model": "ibm-granite/granite-3.2-8b-instruct",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Kubernetes operators are software extensions that use custom resources to manage applications and their components, encoding operational knowledge into automated controllers. They follow the operator pattern to watch for changes to custom resources and reconcile the actual state of the cluster with the desired state defined by the user."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 28,
    "completion_tokens": 52,
    "total_tokens": 80
  }
}
```

```bash
# Send a batch inference request (lower priority, cost-optimized)
curl -s "${FLEET_GATEWAY}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "x-llm-d-inference-objective: batch" \
  -H "x-llm-d-inference-fairness-id: tenant-acme" \
  -d '{
    "model": "ibm-granite/granite-3.2-8b-instruct",
    "messages": [
      {"role": "user", "content": "Summarize the benefits of multi-cluster Kubernetes."}
    ],
    "max_tokens": 256
  }' | jq .
```

**Expected output:**

```json
{
  "id": "chatcmpl-a1b2c3d4e5f6a7b8c9d0e1f2",
  "object": "chat.completion",
  "created": 1751808205,
  "model": "ibm-granite/granite-3.2-8b-instruct",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Multi-cluster Kubernetes provides several key benefits: improved resilience through geographic distribution, better resource utilization by scheduling workloads where capacity exists, regulatory compliance through data locality controls, and reduced blast radius during failures. It also enables teams to scale beyond the limits of a single cluster while maintaining a unified operational model."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 16,
    "completion_tokens": 67,
    "total_tokens": 83
  }
}
```

```bash
# Show the routing metrics
curl -s http://localhost:9090/metrics | grep fleet_routing
```

**Expected output:**

```
# HELP fleet_routing_requests_total Total number of routed inference requests
# TYPE fleet_routing_requests_total counter
fleet_routing_requests_total{cluster="us-east-gpu-01",model="granite-3-2-8b",objective="realtime"} 1
fleet_routing_requests_total{cluster="us-west-gpu-01",model="granite-3-2-8b",objective="batch"} 1
# HELP fleet_routing_latency_seconds Routing decision latency in seconds
# TYPE fleet_routing_latency_seconds histogram
fleet_routing_latency_seconds_bucket{le="0.001"} 2
fleet_routing_latency_seconds_bucket{le="0.005"} 2
fleet_routing_latency_seconds_bucket{le="+Inf"} 2
fleet_routing_latency_seconds_sum 0.00034
fleet_routing_latency_seconds_count 2
```

> **What to highlight:** The response headers tell the full story. `x-llm-d-served-cluster` shows which cluster handled the request. `x-llm-d-routing-decision` shows the algorithm used. The batch request may land on a different cluster than the realtime one -- the gateway optimizes for different objectives. Routing decisions take sub-millisecond. The API is fully OpenAI-compatible; any existing client library works out of the box.

**Transition:** "Now let's look at how we govern access across tenants."

---

### 5. Tenant Governance (2 min)

**Talking points:**

- TenantProfiles define per-tenant quotas, rate limits, and priority classes.
- This is critical for shared infrastructure -- you need to prevent one team from monopolizing GPU resources.
- The fairness ID header we saw earlier connects each request to its tenant profile.
- Enforcement happens at the Praxis AI gateway level, so it works across all clusters in the fleet.

**Commands:**

```bash
# Create a TenantProfile for ACME Corp
cat <<'EOF' | oc apply -f -
apiVersion: fleet.llm-d.ai/v1alpha1
kind: TenantProfile
metadata:
  name: tenant-acme
  namespace: fleet-llm-d
spec:
  displayName: "ACME Corp - AI Platform Team"
  fairnessId: tenant-acme
  priority: 100
  quotas:
    requestsPerMinute: 60
    tokensPerMinute: 100000
    maxConcurrentRequests: 10
    gpuHoursPerDay: 48
  allowedModels:
    - ibm-granite/granite-3.2-8b-instruct
    - meta-llama/Llama-3.1-70B-Instruct
  allowedRegions:
    - us-east-1
    - us-west-2
EOF
```

**Expected output:**

```
tenantprofile.fleet.llm-d.ai/tenant-acme created
```

```bash
# Verify the tenant profile
oc get tenantprofile tenant-acme -n fleet-llm-d -o jsonpath='{.spec}' | jq .
```

**Expected output:**

```json
{
  "displayName": "ACME Corp - AI Platform Team",
  "fairnessId": "tenant-acme",
  "priority": 100,
  "quotas": {
    "requestsPerMinute": 60,
    "tokensPerMinute": 100000,
    "maxConcurrentRequests": 10,
    "gpuHoursPerDay": 48
  },
  "allowedModels": [
    "ibm-granite/granite-3.2-8b-instruct",
    "meta-llama/Llama-3.1-70B-Instruct"
  ],
  "allowedRegions": [
    "us-east-1",
    "us-west-2"
  ]
}
```

```bash
# Demonstrate rate limiting -- exceed the quota
for i in $(seq 1 65); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    "${FLEET_GATEWAY}/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "x-llm-d-inference-objective: realtime" \
    -H "x-llm-d-inference-fairness-id: tenant-acme" \
    -d '{"model":"ibm-granite/granite-3.2-8b-instruct","messages":[{"role":"user","content":"hi"}],"max_tokens":1}')
  echo "Request $i: HTTP $STATUS"
done
```

**Expected output (last few lines):**

```
Request 58: HTTP 200
Request 59: HTTP 200
Request 60: HTTP 200
Request 61: HTTP 429
Request 62: HTTP 429
Request 63: HTTP 429
Request 64: HTTP 429
Request 65: HTTP 429
```

```bash
# Show the 429 response body
curl -s "${FLEET_GATEWAY}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "x-llm-d-inference-objective: realtime" \
  -H "x-llm-d-inference-fairness-id: tenant-acme" \
  -d '{"model":"ibm-granite/granite-3.2-8b-instruct","messages":[{"role":"user","content":"hi"}],"max_tokens":1}' | jq .
```

**Expected output:**

```json
{
  "error": {
    "message": "Rate limit exceeded for tenant 'tenant-acme': 60 requests per minute",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded",
    "tenant": "tenant-acme",
    "limit": 60,
    "reset_at": "2026-07-06T14:25:00Z"
  }
}
```

> **What to highlight:** Tenant governance is declarative. You define a TenantProfile once and the gateway enforces it across the entire fleet. Rate limiting kicks in at exactly the configured threshold. This prevents noisy-neighbor problems on shared GPU infrastructure. The 429 response includes the reset time so clients can implement proper backoff.

**Transition:** "Every request we've sent has been recorded in the compliance ledger. Let's take a look."

---

### 6. Compliance Trail (2 min)

**Talking points:**

- The ARE Immutable Ledger records every inference event as a tamper-evident audit trail.
- Each entry is hash-chained to the previous one, so you can prove no records have been altered or deleted.
- This is critical for regulated industries -- financial services, healthcare, government.
- The ledger is external infrastructure, not part of fleet-llm-d itself, but the integration is seamless.

**Commands:**

```bash
# Query recent audit entries from the ARE Immutable Ledger
grpcurl -plaintext \
  -d '{"entry_type":"fleet.inference.completed","page_size":3}' \
  localhost:9092 are.ledger.v1.ImmutableLedgerService/QueryEntries
```

**Illustrative explorer projection:** The raw protobuf response uses
`LedgerEntry` fields (`entryId`, `entryType`, base64 `content`, `entryHash`,
`previousHash`, `chainPosition`, and `writtenTs`). The UI may project decoded
fleet content into the friendlier shape below.

```json
{
  "entries": [
    {
      "entryId": "ae-1751808198-0001",
      "timestamp": "2026-07-06T14:23:18.847Z",
      "eventType": "INFERENCE_COMPLETED",
      "model": "ibm-granite/granite-3.2-8b-instruct",
      "tenant": "tenant-acme",
      "cluster": "us-east-gpu-01",
      "region": "us-east-1",
      "tokensIn": 28,
      "tokensOut": 52,
      "latencyMs": 847,
      "objective": "realtime",
      "hash": "sha256:3a7f2b9c1d4e5f6a8b0c9d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a",
      "previousHash": "sha256:0000000000000000000000000000000000000000000000000000000000000000"
    },
    {
      "entryId": "ae-1751808205-0002",
      "timestamp": "2026-07-06T14:23:25.312Z",
      "eventType": "INFERENCE_COMPLETED",
      "model": "ibm-granite/granite-3.2-8b-instruct",
      "tenant": "tenant-acme",
      "cluster": "us-west-gpu-01",
      "region": "us-west-2",
      "tokensIn": 16,
      "tokensOut": 67,
      "latencyMs": 1203,
      "objective": "batch",
      "hash": "sha256:7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c",
      "previousHash": "sha256:3a7f2b9c1d4e5f6a8b0c9d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a"
    },
    {
      "entryId": "ae-1751808261-0003",
      "timestamp": "2026-07-06T14:24:21.556Z",
      "eventType": "RATE_LIMIT_ENFORCED",
      "model": "ibm-granite/granite-3.2-8b-instruct",
      "tenant": "tenant-acme",
      "cluster": "",
      "region": "",
      "tokensIn": 0,
      "tokensOut": 0,
      "latencyMs": 0,
      "objective": "realtime",
      "hash": "sha256:c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0",
      "previousHash": "sha256:7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c"
    }
  ],
  "totalCount": 63
}
```

```bash
# Verify the hash chain integrity
grpcurl -plaintext \
  -d '{"entry_type":"fleet.inference.completed"}' \
  localhost:9092 are.ledger.v1.ImmutableLedgerService/VerifyChain
```

**Expected output:**

```json
{
  "chainValid": true,
  "entriesChecked": "63"
}
```

> **What to highlight:** Every inference event, every rate limit enforcement, every routing decision is recorded with a cryptographic hash chain. You can prove to auditors that no records were tampered with. Notice how `previousHash` in each entry matches the `hash` of the prior entry -- this is how the chain works. The ledger even captured the rate-limit events from our earlier demo.

**Transition:** "Finally, let's see what happens when a cluster goes down."

---

### 7. Failover (1 min)

**Talking points:**

- In production, clusters fail. Nodes go down. GPUs overheat.
- fleet-llm-d continuously monitors backend health and reroutes traffic automatically.
- We will simulate a failure by marking one backend as unhealthy and watch the gateway reroute.
- This happens in seconds, not minutes. No human intervention required.

**Commands:**

```bash
# Simulate a backend failure on us-east-gpu-01
fleetctl backend drain granite-3-2-8b-0 \
  --cluster us-east-gpu-01 \
  --reason "simulated-failure"
```

**Expected output:**

```
Backend "granite-3-2-8b-0" on cluster "us-east-gpu-01" marked as draining.
  Active connections will complete, new requests will be rerouted.
  Reason: simulated-failure
```

```bash
# Send a request and observe it routes to the healthy backend
curl -s -D - "${FLEET_GATEWAY}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "x-llm-d-inference-objective: realtime" \
  -H "x-llm-d-inference-fairness-id: tenant-acme" \
  -d '{
    "model": "ibm-granite/granite-3.2-8b-instruct",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 16
  }' 2>&1 | grep "x-llm-d-"
```

**Expected output:**

```
x-llm-d-served-by: granite-3-2-8b-1
x-llm-d-served-cluster: us-west-gpu-01
x-llm-d-served-region: us-west-2
x-llm-d-routing-decision: failover
x-llm-d-failover-from: us-east-gpu-01
```

```bash
# Check the pool status
fleetctl pool status granite-3-2-8b
```

**Expected output:**

```
FleetInferencePool: granite-3-2-8b
  Model:    ibm-granite/granite-3.2-8b-instruct
  Engine:   vllm
  Status:   Degraded (1/2 replicas serving)

  REPLICA              CLUSTER            REGION      GPU     STATUS
  granite-3-2-8b-0     us-east-gpu-01     us-east-1   A100    Draining
  granite-3-2-8b-1     us-west-gpu-01     us-west-2   A100    Serving
```

```bash
# Restore the backend
fleetctl backend restore granite-3-2-8b-0 --cluster us-east-gpu-01
```

**Expected output:**

```
Backend "granite-3-2-8b-0" on cluster "us-east-gpu-01" restored.
  Status: Serving
  Pool "granite-3-2-8b" is now fully healthy (2/2 replicas).
```

> **What to highlight:** Failover is automatic and instant. The `x-llm-d-failover-from` header proves the gateway detected the drained backend and rerouted. When the backend comes back, the pool self-heals. In a real outage, this happens without any human intervention -- the controller detects the failure and the gateway adapts in real time.

**Transition:** "That wraps up the core demo. Let me summarize what we've seen."

---

## Summary Slide Talking Points

At the end of the demo, summarize the key capabilities shown:

1. **Single-command deployment** -- fleet-llm-d installs in under a minute.
2. **Fleet-level orchestration** -- manage models across multiple clusters from one control plane.
3. **Intelligent placement** -- the platform decides where to run models based on capacity, GPU type, and region.
4. **Cross-cluster routing** -- the Praxis AI gateway routes inference requests with sub-millisecond overhead.
5. **Tenant governance** -- declarative quotas and rate limits prevent noisy-neighbor problems.
6. **Compliance-grade audit** -- every event is recorded in a tamper-evident hash chain via the ARE Immutable Ledger.
7. **Automatic failover** -- backend failures are detected and traffic is rerouted without human intervention.

---

## Troubleshooting

### Controller pod in CrashLoopBackOff

**Symptom:** `fleet-controller` pod keeps restarting.

**Fix:**

```bash
# Check the logs for the root cause
oc logs -n fleet-llm-d deployment/fleet-controller --previous

# Common cause: missing RBAC. Re-run the deploy script:
./hack/deploy-demo.sh \
  --cluster-url https://api.hub.demo.openshiftapps.com:6443 \
  --token sha256~xK9mR2vLpQwN7tBjYcHfZsA3dE6gU8iO1kM4nP5rT \
  --force
```

### Gateway returns 502 Bad Gateway

**Symptom:** All inference requests return HTTP 502.

**Fix:**

```bash
# Check if backends are registered
fleetctl backend list --pool granite-3-2-8b

# If empty, the model pods may not be running on spoke clusters
oc get pods -n fleet-llm-d --context us-east-gpu-01

# Restart the Praxis AI gateway to force re-discovery
oc rollout restart deployment/praxis-gateway -n fleet-llm-d
```

### Cluster registration fails with "connection refused"

**Symptom:** `fleetctl cluster register` returns a connection error.

**Fix:**

```bash
# Verify the cluster API endpoint is reachable
curl -k https://api.us-east-01.demo.openshiftapps.com:6443/healthz

# Check if the token is still valid
oc login https://api.us-east-01.demo.openshiftapps.com:6443 --token=sha256~aB3cD4eF5gH6iJ7kL8mN9oP0qR1sT2uV

# If the token expired, generate a new one
oc create token fleet-controller-sa -n fleet-llm-d --duration=24h
```

### ARE Ledger unreachable

**Symptom:** Compliance queries fail with "connection refused" on port 9092.

**Fix:**

```bash
# Verify the ledger pod is running
oc get pods -n are-system | grep ledger

# Check the service endpoint
oc get svc -n are-system are-immutable-ledger

# Port-forward if running locally
oc port-forward -n are-system svc/are-immutable-ledger 9092:9092
```

### Rate limiting not enforcing

**Symptom:** Requests are not being rate-limited even after exceeding quota.

**Fix:**

```bash
# Check if the TenantProfile is applied correctly
oc get tenantprofile tenant-acme -n fleet-llm-d -o yaml

# Verify the fairness ID header matches
# The header value must exactly match spec.fairnessId in the TenantProfile
curl -v "${FLEET_GATEWAY}/v1/chat/completions" \
  -H "x-llm-d-inference-fairness-id: tenant-acme" \
  ... 2>&1 | grep "x-llm-d-inference-fairness-id"

# Restart the Praxis AI gateway to reload tenant configs
oc rollout restart deployment/praxis-gateway -n fleet-llm-d
```

---

## Appendix: Quick Reset

Use these commands to tear down and re-deploy for repeat demos.

### Full Teardown

```bash
# Remove all fleet-llm-d resources and the namespace
fleetctl cluster unregister us-east-gpu-01 --force
fleetctl cluster unregister eu-west-gpu-01 --force
fleetctl cluster unregister us-west-gpu-01 --force

oc delete fleetinferencepool --all -n fleet-llm-d
oc delete tenantprofile --all -n fleet-llm-d
oc delete fleetroutingpolicy --all -n fleet-llm-d
oc delete placementpolicy --all -n fleet-llm-d
oc delete fleetscalingpolicy --all -n fleet-llm-d
oc delete modellifecycle --all -n fleet-llm-d
oc delete kvcachetransferpolicy --all -n fleet-llm-d

oc delete namespace fleet-llm-d

# Remove CRDs
oc delete crd fleetinferencepools.fleet.llm-d.ai
oc delete crd placementpolicies.fleet.llm-d.ai
oc delete crd fleetroutingpolicies.fleet.llm-d.ai
oc delete crd tenantprofiles.fleet.llm-d.ai
oc delete crd fleetscalingpolicies.fleet.llm-d.ai
oc delete crd modellifecycles.fleet.llm-d.ai
oc delete crd kvcachetransferpolicies.fleet.llm-d.ai
```

### Quick Re-deploy

```bash
# Re-deploy everything from scratch (takes ~60 seconds)
./hack/deploy-demo.sh \
  --cluster-url https://api.hub.demo.openshiftapps.com:6443 \
  --token sha256~xK9mR2vLpQwN7tBjYcHfZsA3dE6gU8iO1kM4nP5rT

# Re-register clusters
fleetctl cluster register \
  --name us-east-gpu-01 \
  --api-url https://api.us-east-01.demo.openshiftapps.com:6443 \
  --token sha256~aB3cD4eF5gH6iJ7kL8mN9oP0qR1sT2uV \
  --region us-east-1 \
  --labels gpu-type=a100,tier=production

fleetctl cluster register \
  --name eu-west-gpu-01 \
  --api-url https://api.eu-west-01.demo.openshiftapps.com:6443 \
  --token sha256~wX3yZ4aB5cD6eF7gH8iJ9kL0mN1oP2qR \
  --region eu-west-1 \
  --labels gpu-type=h100,tier=production

fleetctl cluster register \
  --name us-west-gpu-01 \
  --api-url https://api.us-west-01.demo.openshiftapps.com:6443 \
  --token sha256~sT3uV4wX5yZ6aB7cD8eF9gH0iJ1kL2mN \
  --region us-west-2 \
  --labels gpu-type=a100,tier=production

echo "Ready for demo. Run 'fleetctl cluster list' to verify."
```

### One-Liner Reset and Redeploy

```bash
# Nuke everything and start fresh
oc delete namespace fleet-llm-d --wait=true && \
  ./hack/deploy-demo.sh \
    --cluster-url https://api.hub.demo.openshiftapps.com:6443 \
    --token sha256~xK9mR2vLpQwN7tBjYcHfZsA3dE6gU8iO1kM4nP5rT && \
  echo "Reset complete. Re-register clusters to continue."
```
