# CPU Inference Scaling — Two-Track Strategy

> **Historical downstream proposal.** The deployment state and measurements
> below predate `v0.3.0`; they are not current product certification or an OSS
> installation guide.

**Date**: 2026-07-08
**Author**: J. Kershaw (Red Hat AI Field Engineering)

---

## Track 1: Triforce Demo (Summit Connect Ready)

**Goal**: 50 concurrent users, peak burst 200, across 4 CPU models. Reliable, tested, deployed now.

### What's Deployed on prod-cluster-1 Today

| Component | Details | Status |
|-----------|---------|--------|
| **fleet-llm-d proxy** | Auth, rate limiting (10/s per tenant), load shedding (20 max-inflight per model) | Live |
| **granite-2b-cpu** | Multi-worker (2 concurrent), HPA 1→4 replicas | Live |
| **phi3-mini-cpu** | Multi-worker (2 concurrent), HPA 1→4 replicas | Live |
| **qwen25-3b-cpu** | Multi-worker (2 concurrent), HPA 1→4 replicas | Live |
| **qwen25-3b-int8** | INT8 quantized, multi-worker | Live |

### Capacity Math for Triforce

| Config | Per-pod slots | Max replicas (HPA) | Total slots per model |
|--------|-------------|--------------------|-----------------------|
| Multi-worker (2) | 2 | 4 | **8** |

Across 3 models: **24 concurrent inference slots**.

With fleet-llm-d load shedding at 20 per model:
- **50 sustained users** (1 request every 30s each ≈ 1.7 rps total): Easily handled. Each model sees ~0.6 rps, well within 8-slot capacity.
- **200 burst**: fleet-llm-d queues up to 20 per model (60 total), remaining 140 get immediate 503 + `Retry-After: 5`. Users retry in 5s. Worst case TTFT during burst: ~10s (5 queued × 2s each).

### Benchmarks (proven on prod-cluster-1)

| Model | Single request | 10 concurrent | 20 concurrent | 50 concurrent |
|-------|---------------|---------------|---------------|---------------|
| granite-2b-cpu | 731ms | 7.4s | 14.3s | 60% errors (1 replica) |
| phi3-mini-cpu | 491ms | 7.3s | 14.6s | 87% errors (1 replica) |
| qwen25-3b-cpu | 575ms | 7.4s | 14.3s | 60% errors (1 replica) |

With HPA at 4 replicas, the 50-concurrent breaking point shifts to ~200 concurrent.

### What to do for Triforce

1. **Pre-warm before demo**: Send a few requests to each model 10 min before the session so HPAs scale up
2. **Set rate limit per lab session**: `x-llm-d-inference-fairness-id` header maps to session ID
3. **Monitor**: `watch oc get hpa -n llm-hosting` to see replicas scaling
4. **If load spikes**: HPAs auto-scale. If beyond 4 replicas, fleet-llm-d sheds load gracefully

### Rollback (if anything goes wrong during demo)

```bash
# Quick: point fleet-llm-d back to original single-worker backends
oc patch deployment fleet-controller -n fleet-llm-d --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/args/3","value":"--backends=[{\"model\":\"granite-2b-cpu\",\"url\":\"http://granite-2b-cpu-predictor.llm-hosting.svc:80\",\"runtime\":\"openvino\"},{\"model\":\"phi3-mini-cpu\",\"url\":\"http://phi3-mini-cpu-predictor.llm-hosting.svc:80\",\"runtime\":\"openvino\"},{\"model\":\"qwen25-3b-cpu\",\"url\":\"http://qwen25-3b-cpu-predictor.llm-hosting.svc:80\",\"runtime\":\"openvino\"}]"}]'

# Nuclear: remove everything
oc delete namespace fleet-llm-d
oc delete deployment intel-granite-2b-multiworker intel-phi3-mini-multiworker intel-qwen25-3b-multiworker intel-qwen25-3b-int8 -n llm-hosting
oc delete svc intel-granite-2b-multiworker intel-phi3-mini-multiworker intel-qwen25-3b-multiworker intel-qwen25-3b-int8 -n llm-hosting
```

---

## Track 2: Event-Scale CPU Inference (Red Hat Summit Main Stage)

**Goal**: 500+ concurrent users, sustained over hours, CPU-only, multi-model, multi-cluster.

### The Problem at Main Summit

CPU inference failed because:
1. **Single-replica, single-worker**: 1 inference slot per model
2. **No autoscaling**: HPAs pinned at min==max
3. **No load management**: Requests piled up, 120s timeouts, users saw blank screens
4. **No multi-cluster routing**: Everything on one cluster

### What fleet-llm-d Adds (proven)

| Capability | Impact | Status |
|-----------|--------|--------|
| Connection pooling (50/host) | No TCP thrashing under burst | Deployed |
| Health polling | Dead backends detected in 30s | Deployed |
| Load shedding (503 + Retry-After) | Users get fast feedback, not 2-min hangs | Deployed |
| Per-tenant rate limiting | Fair sharing across lab sessions | Deployed |
| HPA integration | Auto-scale 1→4 under load | Deployed |
| Multi-worker backends | 2× concurrent slots per pod | Deployed |
| Inference benchmark suite | Measure before/after every change | Built |

### What's Still Needed for Event Scale

| Need | Solution | Effort | Impact |
|------|----------|--------|--------|
| **More replicas** | Increase HPA max from 4→8 | Config change | 2× capacity |
| **Faster cold start** | Pre-build container images with deps baked in (no pip install on startup) | 1 day | Startup 3min → 30s |
| **OVMS for throughput-critical models** | Bake OVMS GenAI models into container images (proven locally, exported) | 1 day per model | 4× concurrent per pod via continuous batching |
| **Multi-cluster routing** | fleet-llm-d routes across prod-cluster-1 + dev-cluster-1 (if on same network) | Architecture | 2× total capacity |
| **Predictive scaling** | Scale up based on event schedule, not reactive CPU metrics | 1 day | No cold-start delay during peak |
| **INT4 quantization** | More aggressive compression (vs INT8) for faster inference | 1 day per model | 2× per-request speedup |

### OVMS Findings (tested today)

| Metric | Python/FastAPI | OVMS C++ |
|--------|---------------|----------|
| Single request (20 tokens) | **0.8s** | 2.0s |
| Concurrent handling | 1 per worker (2 with multi-worker) | Up to 256 (`max_num_seqs`) |
| Streaming | Generate-then-stream (fake) | Token-by-token (real) |
| Memory per instance | 8GB | 8GB |
| Startup time | 3-5 min (pip install) | 30s (compiled binary) |

**Recommendation**: Use OVMS for models that need high concurrency per pod (>4 simultaneous). Use Python/FastAPI for models where per-request latency matters more than concurrency. The exported OVMS model for Qwen2.5-3B-Instruct is available at `quay.io/fleet-llm-d/fleet-controller:intel-qwen25-ovms`.

### Architecture for 500+ Concurrent

```
                    ┌─────────────────────────┐
                    │      Load Balancer       │
                    └──────────┬──────────────┘
                               │
                    ┌──────────┴──────────────┐
                    │   fleet-llm-d proxy      │
                    │   auth + rate limit      │
                    │   load shedding          │
                    │   health polling         │
                    └─────┬──────┬──────┬─────┘
                          │      │      │
              ┌───────────┘      │      └───────────┐
              │                  │                  │
    ┌─────────┴──────┐ ┌────────┴───────┐ ┌────────┴───────┐
    │  granite-2b    │ │  phi3-mini     │ │  qwen25-3b     │
    │  8 replicas    │ │  8 replicas    │ │  8 replicas    │
    │  2 workers ea  │ │  2 workers ea  │ │  2 workers ea  │
    │  = 16 slots    │ │  = 16 slots    │ │  = 16 slots    │
    └────────────────┘ └────────────────┘ └────────────────┘
                        Total: 48 concurrent slots
                        + load shedding for burst
```

With 8 replicas × 2 workers × 3 models = **48 concurrent inference slots**. At 0.8s per request, that's **60 rps sustained**. For 500 users at 1 request per 30s = 17 rps — well within capacity.

### Scaling Playbook for Events

| Audience Size | Replicas per model | Total slots | fleet-llm-d config |
|--------------|-------------------|-------------|---------------------|
| 20 users (lab) | 1-2 (HPA) | 4-12 | `--max-inflight=10 --rate-limit=5` |
| 50 users (demo) | 2-4 (HPA) | 12-24 | `--max-inflight=20 --rate-limit=10` |
| 200 users (workshop) | 4-8 | 24-48 | `--max-inflight=30 --rate-limit=20` |
| 500+ users (keynote) | 8+ per model | 48+ | `--max-inflight=50 --rate-limit=30` |

---

## What We Proved Today

| Claim | Evidence |
|-------|---------|
| fleet-llm-d can proxy CPU inference with <5ms overhead | prod-cluster-1 benchmarks: proxy adds <5ms to backend latency |
| HPA autoscaling works for CPU inference | qwen25-3b auto-scaled 1→4 replicas under load |
| Multi-worker uvicorn doubles concurrent capacity | Proper container image with `workers=2` deployed and tested |
| Load shedding prevents cascade failure | `--max-inflight=20` returns 503+Retry-After instead of 2-min hangs |
| OVMS C++ serves OpenAI-compatible API | Deployed, functional, but slower per-request than Python on this hardware |
| INT8 quantization exports cleanly | `export_model.py` with `--weight-format int8` produces 3GB model |
| NUMA pinning is counterproductive on multi-socket | O5 rolled back — hurt performance 10× on 256-core workers |
| Other models are completely unaffected | 11 non-target pods running throughout all changes |
