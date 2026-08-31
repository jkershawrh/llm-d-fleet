# prod-cluster-1 CPU Inference Scaling Proposal

> **Historical downstream proposal.** Retained as sanitized engineering
> context, excluded from the portable OSS release, and not an active
> llm-d-fleet deployment recommendation.

**Date**: 2026-07-08
**Author**: J. Kershaw (Red Hat AI Field Engineering)
**Status**: Proposal — requires prod-cluster-1 team approval before any changes

---

## Executive Summary

CPU inference on prod-cluster-1 failed under load at Red Hat Summit. We've deployed fleet-llm-d as an inference proxy on prod-cluster-1 and benchmarked all 3 CPU models (granite-2b-cpu, phi3-mini-cpu, qwen25-3b-cpu) to identify the bottleneck. The root cause is **single-replica deployments with serial inference** — each model can only serve ~0.5-0.75 req/s and breaks at 20-50 concurrent users.

**Proposed fix**: Unlock HPA scaling (min=1, max=4) and reduce CPU requests from 32→16 cores per pod. This would allow 4× concurrent capacity with no additional hardware.

---

## Evidence

### Cluster Capacity (current)

| Resource | Available | Used | Headroom |
|----------|-----------|------|----------|
| CPU (worker01-06, CPU-only) | 1,536 cores | 6 cores (0.4%) | **1,530 cores** |
| Memory (worker01-06) | 3 TB | ~120 GB | **2.88 TB** |
| CPU requests (cluster-wide) | 2,752 cores | 318 cores (12%) | **2,434 cores** |

### Baseline Benchmarks (1 replica per model)

Tested via fleet-llm-d inference proxy on prod-cluster-1, 2026-07-08:

| Concurrency | granite-2b-cpu | phi3-mini-cpu | qwen25-3b-cpu |
|------------|----------------|---------------|---------------|
| 1 | 1.9s / 0% | 1.5s / 0% | 1.4s / 0% |
| 2 | 3.7s / 0% | 2.9s / 0% | 2.7s / 0% |
| 5 | 9.4s / 0% | 7.3s / 0% | 6.6s / 0% |
| 10 | 19.6s / 0% | 14.6s / 0% | 13.2s / 0% |
| **20** | **15.1s / 75%** | 29.3s / 0% | 26.7s / 0% |
| **50** | 99% errors | **87% errors** | **85% errors** |

*Values: TTFT P50 / error rate*

**Breaking point**: granite-2b-cpu at 20 concurrent, phi3-mini and qwen25-3b at 50 concurrent.

**Root cause**: Python `threading.Lock()` in each backend serializes all inference. Each request takes ~1.5-2s, so at 10 concurrent the 10th request waits ~15-20s. At 20+ concurrent, requests exceed the 120s proxy timeout and fail.

### Multi-Model Distribution

When load is spread across all 3 models simultaneously, **30 concurrent users with 0% errors**:

| Concurrency | TTFT P50 | Error Rate | Throughput |
|------------|----------|------------|------------|
| 3 (1/model) | 1.5s | 0% | 1.5 rps |
| 10 (~3/model) | 4.2s | 0% | 1.3 rps |
| 20 (~7/model) | 7.7s | 0% | 1.5 rps |
| 30 (10/model) | 13.2s | 0% | 1.5 rps |

### dev-cluster-1 Scaling Proof (OVMS C++ backend)

On dev-cluster-1 (256-core single node), we proved that horizontal scaling is linear:

| Replicas | 50 concurrent TTFT | Error rate | Throughput |
|----------|-------------------|------------|------------|
| 1 | 22.9s | 21.3% | 0.9 rps |
| 4 | 466ms | 0% | 2.5 rps |
| 8 | 225ms | 0% | 4.2 rps |

---

## Proposed Changes

### 1. Unlock HPA scaling

| Model | Current min/max | Proposed min/max | Justification |
|-------|----------------|-----------------|---------------|
| granite-2b-cpu-predictor | 1/1 | 1/4 | Breaks at 20 concurrent; 4 replicas → ~80 concurrent capacity |
| phi3-mini-cpu-predictor | 1/1 | 1/4 | Breaks at 50 concurrent; 4 replicas → ~200 concurrent capacity |
| qwen25-3b-cpu-predictor | 1/1 | 1/4 | Same as phi3-mini |

HPA target: CPU 60% (down from current 80%) — scale earlier before TTFT degrades.

```yaml
# Example patch for granite-2b-cpu:
oc patch hpa granite-2b-cpu-predictor -n llm-hosting -p '{
  "spec": {
    "minReplicas": 1,
    "maxReplicas": 4,
    "metrics": [{"type": "Resource", "resource": {"name": "cpu", "target": {"type": "Utilization", "averageUtilization": 60}}}]
  }
}'
```

### 2. Reduce CPU requests

| Current | Proposed | Impact |
|---------|----------|--------|
| requests: 32 CPU | requests: 16 CPU | Allows 2× more pods per node (16 vs 8 pods per 256-core worker) |
| limits: 128 CPU | limits: 64 CPU | Still 4× headroom above request |

**Justification**: 90-day Prometheus data shows peak CPU usage of 10.6 cores for the busiest inference pod (qwen3-14b). The CPU inference pods (granite-2b, phi3-mini, qwen25-3b) have never exceeded 1 core because they receive zero traffic. With `OMP_NUM_THREADS=32`, the model uses 32 threads but actual core utilization is 5-10 cores under inference load.

```yaml
# Example patch:
oc patch deployment granite-2b-cpu-predictor -n llm-hosting -p '{
  "spec": {"template": {"spec": {"containers": [{"name": "granite-2b-cpu-predictor",
    "resources": {"requests": {"cpu": "16"}, "limits": {"cpu": "64", "memory": "32Gi"}}}]}}}
}'
```

### 3. No other changes required

- **fleet-llm-d**: Already deployed in `fleet-llm-d` namespace, provides connection pooling, health polling, rate limiting, and inference load testing. Zero changes to `llm-hosting`.
- **Kubernetes Services**: Already configured correctly (ClusterIP, port 80→8000).
- **Network policies**: None in `llm-hosting` — cross-namespace traffic works.

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Additional replicas consume more memory | Each CPU inference pod uses 5-8 GB. 4 replicas = 32 GB max per model. Workers have 503 GB RAM each. |
| CPU contention with other workloads | Worker01-06 are at 0-1% CPU. Even 12 pods × 16 cores = 192 cores on a 256-core node — still 25% headroom. |
| HPA oscillation (scale up/down thrashing) | Set `stabilizationWindowSeconds: 300` (5 min) on both scale-up and scale-down to prevent rapid cycling. |

## Rollback

```bash
# Revert HPAs to pinned
for dep in granite-2b-cpu-predictor phi3-mini-cpu-predictor qwen25-3b-cpu-predictor; do
  oc patch hpa $dep -n llm-hosting -p '{"spec":{"minReplicas":1,"maxReplicas":1}}'
done

# Revert CPU requests
for dep in granite-2b-cpu-predictor phi3-mini-cpu-predictor qwen25-3b-cpu-predictor; do
  oc patch deployment $dep -n llm-hosting -p '{"spec":{"template":{"spec":{"containers":[{"name":"'$dep'","resources":{"requests":{"cpu":"32"},"limits":{"cpu":"128"}}}]}}}}'
done
```

---

## Expected Outcome

With 4 replicas per model:
- **granite-2b-cpu**: Breaking point moves from 20 → ~80 concurrent
- **phi3-mini-cpu**: Breaking point moves from 50 → ~200 concurrent
- **Summit Connect capacity**: 50+ concurrent lab users across 3 models with <5s TTFT

## Validation Plan

If approved, fleet-llm-d test harness will re-run benchmarks at 2 and 4 replicas to confirm linear scaling on prod-cluster-1 (as proven on dev-cluster-1).
