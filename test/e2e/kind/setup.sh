#!/usr/bin/env bash
# Three-cluster Kind environment for fleet-llm-d e2e testing.
# Creates: fleet-hub (controller), fleet-spoke-1, fleet-spoke-2
# Requires: kind, kubectl, docker/podman
set -euo pipefail

KIND_IMAGE="${KIND_IMAGE:-kindest/node:v1.31.0}"
HUB="fleet-hub"
SPOKE1="fleet-spoke-1"
SPOKE2="fleet-spoke-2"

container_ip() {
  local container="$1"
  docker inspect "$container" --format '{{.NetworkSettings.Networks.kind.IPAddress}}' 2>/dev/null || \
    podman inspect "$container" --format '{{.NetworkSettings.Networks.kind.IPAddress}}' 2>/dev/null
}

build_image() {
  local tag="$1"
  local dockerfile="$2"
  docker build -t "$tag" -f "$dockerfile" . 2>/dev/null || \
    podman build -t "$tag" -f "$dockerfile" .
}

echo "=== Creating Kind clusters ==="
for cluster in "$HUB" "$SPOKE1" "$SPOKE2"; do
  if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
    echo "$cluster already exists, skipping"
  else
    kind create cluster --name "$cluster" --image "$KIND_IMAGE" --wait 60s
    echo "$cluster created"
  fi
done

echo ""
echo "=== Applying CRDs to all clusters ==="
for cluster in "$HUB" "$SPOKE1" "$SPOKE2"; do
  kubectl --context "kind-${cluster}" apply -f api/crds/ 2>/dev/null || true
  echo "CRDs applied to $cluster"
done

echo ""
echo "=== Creating RBAC for fleet-agent ==="
for spoke in "$SPOKE1" "$SPOKE2"; do
  kubectl --context "kind-${spoke}" create namespace fleet-llm-d 2>/dev/null || true
  cat <<EOF | kubectl --context "kind-${spoke}" apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: fleet-agent
  namespace: fleet-llm-d
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fleet-agent
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["fleet.llm-d.ai"]
    resources: ["fleetinferencepools"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fleet-agent
subjects:
  - kind: ServiceAccount
    name: fleet-agent
    namespace: fleet-llm-d
roleRef:
  kind: ClusterRole
  name: fleet-agent
  apiGroup: rbac.authorization.k8s.io
EOF
  echo "RBAC created on $spoke"
done

echo ""
echo "=== Building e2e component images ==="
build_image fleet-controller:e2e deploy/docker/Dockerfile.controller
build_image fleet-agent:e2e deploy/docker/Dockerfile.agent
build_image inference-mock:e2e deploy/docker/Dockerfile.inference-mock

echo ""
echo "=== Loading component images into Kind clusters ==="
kind load docker-image fleet-controller:e2e --name "$HUB"
kind load docker-image fleet-agent:e2e --name "$SPOKE1"
kind load docker-image fleet-agent:e2e --name "$SPOKE2"
kind load docker-image inference-mock:e2e --name "$SPOKE1"
kind load docker-image inference-mock:e2e --name "$SPOKE2"

echo ""
echo "=== Deploying controller to hub ==="
kubectl --context "kind-${HUB}" create namespace fleet-llm-d 2>/dev/null || true
cat <<EOF | kubectl --context "kind-${HUB}" apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fleet-controller
  namespace: fleet-llm-d
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fleet-controller
  template:
    metadata:
      labels:
        app: fleet-controller
    spec:
      containers:
        - name: controller
          image: fleet-controller:e2e
          imagePullPolicy: Never
          args: ["--port=8080", "--ledger-mode=memory", "--rate-limit=0"]
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: fleet-controller
  namespace: fleet-llm-d
spec:
  selector:
    app: fleet-controller
  ports:
    - port: 8080
      targetPort: 8080
  type: NodePort
EOF

echo ""
echo "=== Waiting for controller ==="
kubectl --context "kind-${HUB}" -n fleet-llm-d rollout status deploy/fleet-controller --timeout=120s

# Get the NodePort for cross-cluster access
NODEPORT=$(kubectl --context "kind-${HUB}" -n fleet-llm-d get svc fleet-controller -o jsonpath='{.spec.ports[0].nodePort}')
HUB_IP=$(container_ip "${HUB}-control-plane")
CONTROLLER_URL="http://${HUB_IP}:${NODEPORT}"

echo ""
echo "=== Controller accessible at $CONTROLLER_URL ==="
controller_ready=false
for attempt in $(seq 1 60); do
  if curl -fsS --connect-timeout 1 --max-time 2 "$CONTROLLER_URL/healthz" >/dev/null; then
    controller_ready=true
    break
  fi
  sleep 1
done
if [[ "$controller_ready" != "true" ]]; then
  echo "controller NodePort did not become ready within 60 seconds" >&2
  kubectl --context "kind-${HUB}" -n fleet-llm-d get pods,svc,endpoints >&2 || true
  exit 1
fi
echo "Controller NodePort is ready"

echo ""
echo "=== Pre-registering trusted spoke identities ==="
for spoke in "$SPOKE1" "$SPOKE2"; do
  SPOKE_ID=$(echo "$spoke" | sed 's/fleet-//')
  curl -fsS -X POST "$CONTROLLER_URL/api/v1/clusters" \
    -H 'Content-Type: application/json' \
    -d "{\"id\":\"$SPOKE_ID\",\"name\":\"$SPOKE_ID\",\"region\":\"kind\"}" >/dev/null
  echo "Registered $SPOKE_ID"
done


echo ""
echo "=== Deploying spoke agents ==="
for spoke in "$SPOKE1" "$SPOKE2"; do
  SPOKE_ID=$(echo "$spoke" | sed 's/fleet-//')
  kubectl --context "kind-${spoke}" create namespace fleet-llm-d 2>/dev/null || true
  cat <<EOF | kubectl --context "kind-${spoke}" apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inference-mock
  namespace: fleet-llm-d
spec:
  replicas: 1
  selector:
    matchLabels:
      app: inference-mock
  template:
    metadata:
      labels:
        app: inference-mock
    spec:
      containers:
        - name: inference-mock
          image: inference-mock:e2e
          imagePullPolicy: Never
          env:
            - name: CLUSTER_ID
              value: "$SPOKE_ID"
          ports:
            - name: http
              containerPort: 8000
---
apiVersion: v1
kind: Service
metadata:
  name: inference-mock
  namespace: fleet-llm-d
spec:
  selector:
    app: inference-mock
  ports:
    - name: http
      port: 8000
      targetPort: http
EOF
  kubectl --context "kind-${spoke}" -n fleet-llm-d rollout status deploy/inference-mock --timeout=120s

  cat <<EOF | kubectl --context "kind-${spoke}" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: fleet-agent
  namespace: fleet-llm-d
spec:
  selector:
    app: fleet-agent
  ports:
    - name: proxy
      port: 8090
      targetPort: proxy
  type: NodePort
EOF
  AGENT_NODEPORT=$(kubectl --context "kind-${spoke}" -n fleet-llm-d get svc fleet-agent -o jsonpath='{.spec.ports[0].nodePort}')
  SPOKE_IP=$(container_ip "${spoke}-control-plane")
  INFERENCE_URL="http://${SPOKE_IP}:${AGENT_NODEPORT}"
  HEALTH_URL="${INFERENCE_URL}/readyz"
  cat <<EOF | kubectl --context "kind-${spoke}" apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fleet-agent
  namespace: fleet-llm-d
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fleet-agent
  template:
    metadata:
      labels:
        app: fleet-agent
    spec:
      serviceAccountName: fleet-agent
      enableServiceLinks: false
      containers:
        - name: agent
          image: fleet-agent:e2e
          imagePullPolicy: Never
          env:
            - name: FLEET_CONTROL_PLANE_URL
              value: "$CONTROLLER_URL"
            - name: FLEET_CLUSTER_ID
              value: "$SPOKE_ID"
            - name: FLEET_CLUSTER_HEALTH_URL
              value: "$HEALTH_URL"
            - name: FLEET_CLUSTER_INFERENCE_URL
              value: "$INFERENCE_URL"
            - name: FLEET_UPSTREAM_URL
              value: "http://inference-mock.fleet-llm-d.svc.cluster.local:8000"
            - name: FLEET_LOCAL_PROMETHEUS_URL
              value: "http://inference-mock.fleet-llm-d.svc.cluster.local:8000/metrics"
          ports:
            - name: proxy
              containerPort: 8090
EOF
  kubectl --context "kind-${spoke}" -n fleet-llm-d rollout status deploy/fleet-agent --timeout=120s
  echo "Agent deployed on $spoke (id=$SPOKE_ID, health=$HEALTH_URL)"
done

echo ""
echo "=== Environment ready ==="
echo "Hub:     kind-${HUB} (controller at $CONTROLLER_URL)"
echo "Spoke 1: kind-${SPOKE1}"
echo "Spoke 2: kind-${SPOKE2}"
echo ""
echo "Gateway: fleet-gateway in kind-${HUB}"
echo "Agents register automatically from both spokes."
