# llm-d-fleet production test model

> This document defines reusable test stages and acceptance criteria. Passing
> the test implementation does not certify an operator's infrastructure; each
> deployment must retain its own capacity, failure, security, and soak evidence.

**Project:** fleet-llm-d -- Fleet-level inference orchestration platform  
**Version:** 0.1.0  
**Last updated:** 2026-07-28  
**Owner:** Field Engineering  

---

## 1. Overview

This document defines the test plan that gates fleet-llm-d from development through staging to production. It covers what is tested, how it is tested, what constitutes a pass, and what evidence each test type produces.

### System Under Test

| Component | Language | Binary | Role |
|---|---|---|---|
| fleet-controller | Go | `cmd/fleet-controller` | Control plane -- CRD reconciliation, API, placement, tenant management |
| Praxis AI | External | Praxis AI gateway | Data plane -- inference routing, token counting, access logging |
| fleet-agent | Rust | `crates/agent` | Per-cluster agent -- status reporting, local operations |
| fleetctl | Go | `cmd/fleetctl` | CLI for operators |
| dashboard | TypeScript | `web/` | Next.js operations dashboard |

### Deployment Topology

- **CpuCluster** (Dell Xeon6, multi-node) -- primary compute cluster, runs fleet-controller + OVMS + vLLM backends
- **HubCluster** (SNO) -- secondary cluster, hosts shared immutable ledger, runs fleet-agent
- **GCL pipeline** -- governed-cognitive-loop submits DecisionPackage CloudEvents; never actuates

### Staging Gate Rubric

All five dimensions must independently meet their threshold. No dimension can be traded off against another.

| Dimension | Weight | Staging Minimum | Production Minimum |
|---|---|---|---|
| Correctness | 0.30 | >= 85 | >= 95 |
| Performance | 0.25 | >= 75 | >= 90 |
| Reliability | 0.25 | >= 80 | >= 95 |
| Operability | 0.10 | >= 70 | >= 85 |
| Security | 0.10 | >= 80 | >= 95 |

Composite score = weighted sum across all dimensions (see `test/matrix/rubric.yaml`), but every individual dimension must clear its minimum regardless of the composite.

---

## 2. Test Types

### 2.1 Smoke

| Field | Value |
|---|---|
| **Purpose** | Validates basic liveness after every deployment: health probes respond, authentication rejects unauthenticated requests and accepts valid tokens, a full CRUD lifecycle (create/read/delete cluster) completes, metrics endpoint is reachable, all 15+ API endpoints respond. |
| **Rubric dimension** | Correctness |
| **Duration** | ~2 minutes |
| **Cadence** | Every deploy (CI and manual) |
| **Runner** | `make harness-smoke` |
| **Pass criteria** | 0 failures. Every check must pass or be explicitly skipped (e.g., no token provided). Any failure blocks the pipeline. |
| **Evidence** | `test/harness/results/report.json` -- JSON report with per-check pass/fail, latency, and detail fields. |

Checks executed:

- `health:/healthz`, `health:/readyz`
- `auth:no-token-rejected` (expects 401)
- `auth:valid-token-accepted` (expects 2xx)
- `crud:create-cluster`, `crud:read-clusters`, `crud:delete-cluster`
- `metrics:reachable`
- `reach:*` for every endpoint in `allEndpoints()` (16 endpoints)

---

### 2.2 Capability Soak

| Field | Value |
|---|---|
| **Purpose** | Validates all 10 fleet capabilities under sustained load over 10-72 hours. Detects memory leaks, connection exhaustion, state drift, and slow degradation that short tests miss. |
| **Rubric dimensions** | Reliability, Correctness |
| **Duration** | 10-72 hours (profile-dependent: `quick` 30m, `standard` 2h, `overnight` 8h, `72hr` 72h) |
| **Cadence** | Weekly (overnight or 72hr profile) |
| **Runner** | `python3 test/soak/capability_soak.py --fleet-url <URL> --ledger-url <URL> --inference-url <URL> --profile 72hr` |
| **Pass criteria** | See table below |
| **Evidence** | JSON results file with per-capability success rates, latency percentiles, and cycle-by-cycle logs. |

**Capability SLO gates:**

| Capability | Minimum Success Rate |
|---|---|
| Cluster registration & discovery | >= 95% |
| Agent status & metrics ingestion | >= 95% |
| Tenant creation & quota enforcement | >= 95% |
| Placement via webhook | >= 95% |
| Rollout lifecycle (create/promote/rollback) | >= 95% |
| Real inference through fleet proxy | >= 90% |
| Fleet metrics federation | >= 95% |
| Cost & pricing APIs | >= 95% |
| Ledger chain verification | 100% |
| Degradation injection & recovery | >= 95% |

**Additional soak gates:**

| Metric | Threshold |
|---|---|
| Overall availability | >= 99.5% |
| Recovery from degradation injection | < 60 seconds |
| Memory growth (controller RSS) | < 2x from start to end |

The 10 capabilities map to the clusters and tenants defined in the soak script (CpuCluster Xeon6, HubCluster SNO, tenant-prod, tenant-staging).

---

### 2.3 Pressure

| Field | Value |
|---|---|
| **Purpose** | Validates behavior under concurrent write contention -- rapid CRUD operations, burst submissions, and connection storms. Detects race conditions, deadlocks, and resource starvation. |
| **Rubric dimensions** | Reliability, Performance |
| **Duration** | ~15 minutes |
| **Cadence** | Pre-staging |
| **Runner** | `make harness-pressure` |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` with pressure suite results, including per-operation success counts and panic detection. |

**Pressure gates:**

| Metric | Threshold |
|---|---|
| Rapid sequential operations (1000 attempted) | > 900 successful |
| Burst concurrent operations (500 attempted) | > 400 successful |
| Panic/crash detection | 0 panics |
| Data corruption (read-after-write consistency) | 0 inconsistencies |

---

### 2.4 Performance

| Field | Value |
|---|---|
| **Purpose** | Establishes latency baselines (p50/p95/p99), throughput ceiling, and inference time-to-first-token (TTFT). Provides quantitative evidence for the Performance rubric dimension. |
| **Rubric dimension** | Performance |
| **Duration** | ~30 minutes |
| **Cadence** | Pre-staging |
| **Runners** | `make harness-latency harness-throughput harness-inference` (three suites, run sequentially) |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` with latency, throughput, and inference suite results. Includes `LatencyStats` (p50/p95/p99/min/max/mean) for each suite. |

**Performance gates:**

| Metric | Threshold |
|---|---|
| API latency p99 (CRUD operations) | < 10 seconds |
| API latency p95 (CRUD operations) | < 5 seconds |
| Healthz throughput ceiling | > 500 requests/second |
| Inference TTFT p95 | < 30 seconds |
| Inference TTFT p50 | < 15 seconds |

---

### 2.5 Scale

| Field | Value |
|---|---|
| **Purpose** | Validates control plane behavior as cluster count scales from 10 to 1000. Measures registration throughput, list-endpoint latency degradation, reconciliation time, and memory growth. |
| **Rubric dimensions** | Performance, Reliability |
| **Duration** | ~20 minutes |
| **Cadence** | Pre-staging |
| **Runner** | `make harness-scale` |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` with scale suite results, including per-tier latency measurements and memory snapshots. |

**Scale gates:**

| Cluster Count | Metric | Threshold |
|---|---|---|
| 100 | List clusters p95 latency | < 50ms |
| 500 | List clusters p95 latency | < 500ms |
| 1000 | List clusters p95 latency | < 2s |
| Any | Healthz latency | < 10ms |
| 10 -> 1000 | Memory growth | < 2.5x |
| Any | Registration throughput | > 10 clusters/second |

---

### 2.6 Chaos

| Field | Value |
|---|---|
| **Purpose** | Validates resilience against malformed inputs and pod-kill recovery. Malformed input tests exercise the API with oversized bodies (1MB), invalid JSON, null bytes, unicode edge cases, and XSS payloads. Pod-kill tests verify recovery time after forced container termination. |
| **Rubric dimensions** | Reliability, Security |
| **Duration** | ~15 minutes |
| **Cadence** | Pre-staging |
| **Runners** | `make harness-chaos` (malformed inputs) + `python3 test/soak/resilience_test.py` (pod-kill recovery) |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` (chaos suite) + resilience test JSON output. |

**Chaos gates:**

| Metric | Threshold |
|---|---|
| 5xx responses to malformed input | 0 (must return 400/413, never 500) |
| Panic/crash on malformed input | 0 |
| Pod recovery time after kill | < 60 seconds |
| Data integrity after recovery | No state corruption |

---

### 2.7 Penetration

| Field | Value |
|---|---|
| **Purpose** | Validates security posture through active attack simulation: auth bypass, expired/tampered token replay, privilege escalation (viewer-to-admin), SSRF, SQL injection, path traversal, and header injection. |
| **Rubric dimension** | Security |
| **Duration** | ~10 minutes |
| **Cadence** | Pre-staging + quarterly full run |
| **Runners** | `make harness-redteam` (Go harness) + `python3 test/security/pen_test.py` (extended attack vectors) |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` (redteam suite) + `pen_test.py` JSON output. |

**Penetration gates:**

| Attack Vector | Expected Response | Failure Condition |
|---|---|---|
| No token | 401 Unauthorized | Any 2xx or 5xx |
| Expired token | 401 Unauthorized | Any 2xx or 5xx |
| Tampered token (signature mismatch) | 401 Unauthorized | Any 2xx or 5xx |
| Wrong-role token (viewer on admin endpoint) | 403 Forbidden | Any 2xx or 5xx |
| SQL injection in query params | 400 Bad Request | Any 5xx or data leak |
| Path traversal (`../../../etc/passwd`) | 400/404 | Any 5xx or file content |
| SSRF via URL params | 400 Bad Request | Any 5xx or internal response |
| XSS in request body | 400 Bad Request | Any 5xx or reflected content |
| Header injection (CRLF) | 400 Bad Request | Any 5xx |
| Oversized token (100KB) | 401/413 | Any 5xx |

Hard rule: penetration tests must never produce a 5xx response. Any 5xx is a fail.

---

### 2.8 Multi-Model Inference

| Field | Value |
|---|---|
| **Purpose** | Validates concurrent inference routing across multiple models and tenant fairness (noisy-neighbor isolation). Exercises the fleet proxy with simultaneous requests to different models and verifies that greedy tenants are throttled without starving others. |
| **Rubric dimensions** | Correctness, Performance |
| **Duration** | ~20 minutes |
| **Cadence** | Pre-staging |
| **Runners** | `make harness-inference harness-multimodel harness-fairness` |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` with inference, multimodel, and fairness suite results. Includes per-model latency stats and per-tenant throughput measurements. |

**Multi-model inference gates:**

| Metric | Threshold |
|---|---|
| Error rate (across all models) | < 10% |
| Inference TTFT p95 | < 30 seconds |
| Model routing accuracy | 100% (requests reach correct backend) |
| Noisy-neighbor: greedy tenant throttled | Yes (rate limited after quota exceeded) |
| Noisy-neighbor: other tenants unaffected | Latency delta < 20% vs. baseline |

Requires `--inference-models` flag with comma-separated model names (e.g., `granite-2b,granite-7b`).

---

### 2.9 Stress

| Field | Value |
|---|---|
| **Purpose** | Ramps concurrency from 1 to 500 goroutines against the API to identify the breaking point. Determines the maximum concurrent load the controller can sustain before error rates exceed acceptable thresholds. |
| **Rubric dimensions** | Performance, Reliability |
| **Duration** | ~10 minutes |
| **Cadence** | Pre-staging |
| **Runner** | `make harness-stress` |
| **Pass criteria** | See table below |
| **Evidence** | `test/harness/results/report.json` with stress suite results, including per-tier concurrency success rates and the identified breaking point. |

**Stress gates:**

| Metric | Threshold |
|---|---|
| Survives 200 concurrent goroutines | Yes (> 90% success rate) |
| Breaking point identified | Documented in report |
| Graceful degradation | No panics, no data corruption at any concurrency level |
| Recovery after ramp-down | Controller returns to normal within 30 seconds |

---

## 3. Test Execution Order

Tests run in a defined sequence. A failure at a gate-stop step halts the pipeline.

```
Step  Suite                   Gate    Action on Failure
----  ----------------------  ------  ----------------------------------
 1    Smoke                   STOP    Block deploy. Fix before retry.
 2    Pressure                STOP    Block staging. Investigate contention.
 3    Chaos                   STOP    Block staging. Fix input handling.
 4    Penetration             STOP    Block staging. Fix security issue.
 5    Performance             WARN    Flag regression, review before staging.
 6    Scale                   WARN    Flag regression, review before staging.
 7    Stress                  WARN    Document breaking point.
 8    Multi-Model Inference   WARN    Flag inference issues, review routing.
 9    Capability Soak         STOP    Block production. Fix reliability issue.
```

**Logic:**

1. Smoke is the first gate. If the system is not alive and correct, nothing else matters.
2. Pressure, Chaos, and Penetration are hard gates -- they test correctness and security invariants that must hold.
3. Performance, Scale, and Stress are soft gates -- regressions are flagged but a human reviews whether to proceed.
4. Multi-Model Inference is a soft gate -- inference issues may be backend-dependent.
5. Capability Soak is the final hard gate before production -- it runs last because it takes the longest and validates sustained reliability.

---

## 4. Infrastructure Requirements

### Minimum Infrastructure Per Test Type

| Test Type | CpuCluster Cluster | HubCluster Cluster | OVMS Backend | vLLM Backend | Ledger | GCL |
|---|---|---|---|---|---|---|
| Smoke | Required | -- | -- | -- | -- | -- |
| Pressure | Required | -- | -- | -- | -- | -- |
| Chaos | Required | -- | -- | -- | -- | -- |
| Penetration | Required | -- | -- | -- | -- | -- |
| Performance | Required | -- | Optional | Optional | -- | -- |
| Scale | Required | -- | -- | -- | -- | -- |
| Stress | Required | -- | -- | -- | -- | -- |
| Multi-Model | Required | -- | Required | Required | -- | -- |
| Capability Soak | Required | Required | Required | Optional | Required | Optional |

### Service Dependencies

| Service | Endpoint Pattern | Required For |
|---|---|---|
| fleet-controller API | `http://<host>:8080` | All tests |
| Prometheus metrics | `http://<host>:9090/metrics` | Smoke, Performance, Soak |
| OVMS (Granite 2B) | `http://ovms-granite-2b:8080/v3` | Inference, Multi-Model, Soak |
| vLLM (Granite 7B) | `http://vllm-granite-7b:8000/v1` | Multi-Model, Soak |
| Immutable Ledger | `http://<hubcluster>:30099` | Soak (ledger chain verification) |
| Grafana | `http://<host>:3000` | Observability evidence (not gating) |

### Harness Configuration

All Go harness suites accept these flags:

```
--url        Base URL of fleet-controller (default: http://localhost:8080)
--metrics    Metrics endpoint URL (default: http://localhost:9090)
--token      Bearer token for authenticated endpoints
--secret     HMAC secret for internal token generation
--suite      Suite(s) to run (comma-separated or "all")
--duration   Duration for soak tests (default: 5m)
--output     Output path for JSON report (default: test/harness/results/report.json)
--inference-model   Model name for single-model inference tests
--inference-models  Comma-separated models for multi-model/fairness tests
```

---

## 5. Reporting & Evidence

### Artifact Locations

| Artifact | Path | Format |
|---|---|---|
| Harness report | `test/harness/results/report.json` | JSON (`Report` struct) |
| Capability soak results | stdout + JSON file | Per-capability success rates |
| Resilience test results | stdout + JSON file | Recovery times per scenario |
| Penetration test results | stdout + JSON file | Per-attack-vector verdicts |
| Test matrix | `test/matrix/matrix.yaml` | YAML capability-by-test-type grid |
| Rubric definitions | `test/matrix/rubric.yaml` | YAML dimension thresholds |

### Harness Report Structure

The Go harness produces a `Report` JSON object:

```
{
  "timestamp": "2026-07-28T12:00:00Z",
  "target": "http://fleet-controller:8080",
  "total_passed": 42,
  "total_failed": 0,
  "total_skipped": 3,
  "duration_ns": 120000000000,
  "suites": [
    {
      "name": "smoke",
      "passed": 20,
      "failed": 0,
      "skipped": 1,
      "checks": [
        {"name": "health:/healthz", "passed": true, "latency_ms": 5},
        ...
      ],
      "latencies": {"p50_ms": 4.2, "p95_ms": 12.1, "p99_ms": 28.3, ...}
    }
  ]
}
```

### Grafana Dashboards

Two dashboards are deployed via `deploy/grafana/dashboards/`:

| Dashboard | File | What It Shows |
|---|---|---|
| Fleet Overview | `fleet-overview.json` | Cluster count, agent health, request rates, error rates |
| Fleet Operations | `fleet-operations.json` | Latency percentiles, throughput, tenant usage, placement decisions |

During soak and performance tests, the dashboards provide real-time visibility. After tests complete, take a Grafana snapshot covering the test window as part of the staging evidence package.

### Generating the Staging Evidence Report

1. Run the full test sequence (Section 3).
2. Collect all JSON reports from `test/harness/results/`.
3. Run the soak analysis: `python3 test/soak/analyze-results.py <results-file>`.
4. Export Grafana dashboard snapshots for the test window.
5. Update `test/matrix/matrix.yaml` with current pass rates.
6. Compute rubric scores per dimension using `test/matrix/rubric.yaml` thresholds.
7. Package into a staging evidence bundle: reports + snapshots + matrix + rubric scores.

---

## 6. Rubric Mapping

Which test types provide evidence for which rubric dimensions.

| Test Type | Correctness (0.30) | Performance (0.25) | Reliability (0.25) | Operability (0.10) | Security (0.10) |
|---|---|---|---|---|---|
| Smoke | Primary | -- | -- | Secondary | -- |
| Capability Soak | Secondary | -- | Primary | Secondary | -- |
| Pressure | -- | Secondary | Primary | -- | -- |
| Performance | -- | Primary | -- | -- | -- |
| Scale | -- | Primary | Secondary | -- | -- |
| Chaos | -- | -- | Primary | -- | Secondary |
| Penetration | -- | -- | -- | -- | Primary |
| Multi-Model Inference | Primary | Secondary | -- | -- | -- |
| Stress | -- | Secondary | Primary | -- | -- |

**Legend:**
- **Primary** -- this test type is the main source of evidence for the dimension.
- **Secondary** -- this test type contributes supporting evidence.

### Scoring Inputs Per Dimension

**Correctness (staging >= 85):**
- Smoke: 0 failures required
- Multi-Model: routing accuracy 100%, error rate < 10%
- Soak: per-capability success >= 95% (inference >= 90%, ledger 100%)
- Also: unit test pass rate, BDD scenario pass rate, contract conformance (from `make test`)

**Performance (staging >= 75):**
- Performance: p99 < 10s, throughput > 500 rps
- Scale: list latency within tier thresholds
- Multi-Model: TTFT p95 < 30s
- Stress: documented breaking point

**Reliability (staging >= 80):**
- Soak: availability >= 99.5%, recovery < 60s
- Pressure: > 900/1000 rapid ops, no panics
- Chaos: pod recovery < 60s, no 5xx on malformed input
- Stress: survives 200 concurrent, graceful degradation

**Operability (staging >= 70):**
- Smoke: metrics endpoint reachable, all endpoints responding
- Soak: observability sustained over test duration
- Grafana dashboards functional during test windows
- Also: runbook coverage, alert fidelity (manual assessment)

**Security (staging >= 80):**
- Penetration: all attack vectors blocked (401/403/400, never 5xx)
- Chaos: no information leakage in error responses
- Also: CVE scan, RBAC conformance, tenant isolation (from `make test-contracts`)
