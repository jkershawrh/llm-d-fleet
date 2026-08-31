# Historical llm-d-fleet whitepaper diagrams

> These diagrams support archived whitepaper revisions and may show older
> component or authentication contracts. The current architecture is in
> `docs/architecture-diagram.md` and `llm-d-fleet-whitepaper.md`.

Mermaid diagrams for the fleet-llm-d governed AI inference fleet whitepaper.

---

## 1. Platform Architecture -- 4-System Ecosystem Pipeline

The observe-govern-act-prove pipeline with trust boundaries between each system.

```mermaid
graph LR
    subgraph TB1["Trust Boundary: Signal Intelligence"]
        DF["deepfield-fleet<br/>(Observe)"]
    end

    subgraph TB2["Trust Boundary: Governance"]
        GCL["governed-cognitive-loop<br/>(Govern)"]
    end

    subgraph TB3["Trust Boundary: Fleet Orchestration"]
        FLEET["fleet-llm-d<br/>(Act)"]
    end

    subgraph TB4["Trust Boundary: Evidence Infrastructure"]
        ARE["are-immutable-ledger<br/>(Prove)"]
    end

    DF -- "Advisory CloudEvents<br/>(observation, finding,<br/>forecast, remediation)" --> GCL
    GCL -- "Signed DecisionPackages<br/>(Ed25519, expiry-bounded,<br/>scope-bound, SPIFFE identity)" --> FLEET
    FLEET -- "Evidence entries<br/>(hash-chained,<br/>correlation-indexed)" --> ARE

    FLEET -. "Independent verification<br/>before actuation" .-> FLEET
    ARE -. "Proof receipts<br/>(never authorize execution)" .-> ARE

    style TB1 fill:#e8f4fd,stroke:#2196F3,stroke-width:2px
    style TB2 fill:#fff3e0,stroke:#FF9800,stroke-width:2px
    style TB3 fill:#e8f5e9,stroke:#4CAF50,stroke-width:2px
    style TB4 fill:#fce4ec,stroke:#E91E63,stroke-width:2px

    style DF fill:#bbdefb,stroke:#1565C0,stroke-width:2px
    style GCL fill:#ffe0b2,stroke:#E65100,stroke-width:2px
    style FLEET fill:#c8e6c9,stroke:#2E7D32,stroke-width:2px
    style ARE fill:#f8bbd0,stroke:#AD1457,stroke-width:2px
```

**Key invariants:**
- deepfield-fleet never contacts fleet-llm-d directly
- GCL cannot actuate infrastructure
- The ledger cannot authorize operations
- fleet-llm-d independently verifies, authorizes, and decides

---

## 2. fleet-llm-d Internal Architecture

Go control plane with CRD reconciliation, intent admission, and Praxis AI data plane for inference routing.

```mermaid
graph TB
    subgraph EXTERNAL["External Systems"]
        GOV["Governance Systems<br/>(GCL, CI/CD)"]
        PROM["Prometheus"]
        LEDGER["ARE Ledger"]
    end

    subgraph GOCP["Go Control Plane (fleet-controller)"]
        API["REST API<br/>(27 endpoints)<br/>TLS 1.3 + HMAC-SHA256"]
        ADMIT["v2 Intent Admission<br/>(957-line adapter)<br/>Signature, expiry, scope,<br/>SPIFFE, policy checks"]

        subgraph CAPS["7 Capabilities"]
            PL["Placement<br/>pkg/placement/"]
            RT["Routing<br/>pkg/routing/"]
            AS["Autoscaling<br/>pkg/autoscaling/"]
            OB["Observability<br/>pkg/observability/"]
            TG["Tenant Governance<br/>pkg/tenant/"]
            LM["Lifecycle Mgmt<br/>pkg/lifecycle/"]
            KV["KV Cache<br/>pkg/kvcache/"]
        end

        RECON["CRD Reconciliation Loop"]
        COST["Cost Model<br/>(6 GPU types x 3 tiers)"]
        METRICS["/metrics :9091<br/>Prometheus exposition"]
    end

    subgraph RUSTDP["Data Plane"]
        AGENT["fleet-agent<br/>(spoke cluster mgmt)<br/>Rust: tokio + tonic + axum"]
        PRAXIS["Praxis AI Gateway<br/>(inference routing,<br/>token counting,<br/>access logging)"]
        KVTX["KV Transfer Coordinator<br/>(cache state migration)<br/>Rust: tokio + tonic + axum"]
    end

    subgraph K8S["Kubernetes (OpenShift)"]
        CRDS["10 CRDs<br/>fleet.llm-d.ai API group"]
    end

    GOV -- "POST /api/v2/intents<br/>Signed DecisionPackages" --> API
    API --> ADMIT
    ADMIT -- "Creates FleetOperation" --> RECON
    RECON -- "Reconcile" --> CRDS
    RECON --> PL & RT & AS & TG & LM
    PL & RT --> PRAXIS
    AS --> AGENT
    LM --> AGENT
    KV --> KVTX
    OB --> METRICS
    METRICS --> PROM
    RECON -- "Evidence entries" --> LEDGER
    COST -. "Cost attribution" .-> TG

    style GOCP fill:#e8f5e9,stroke:#2E7D32,stroke-width:2px
    style RUSTDP fill:#fff3e0,stroke:#E65100,stroke-width:2px
    style CAPS fill:#f1f8e9,stroke:#558B2F,stroke-width:1px
    style K8S fill:#e3f2fd,stroke:#1565C0,stroke-width:2px
    style EXTERNAL fill:#f5f5f5,stroke:#757575,stroke-width:1px
```

---

## 3. Decision Pipeline Event Flow -- Single Governance Cycle

Sequence from deepfield observation through ledger evidence write.

```mermaid
sequenceDiagram
    participant DF as deepfield-fleet
    participant GCL as governed-cognitive-loop
    participant FLEET as fleet-llm-d
    participant OPS as FleetOperation
    participant K8S as Kubernetes CRDs
    participant ARE as are-immutable-ledger

    Note over DF: Signal detection
    DF->>GCL: Advisory CloudEvent<br/>(observation/finding/forecast)

    rect rgb(255, 243, 224)
        Note over GCL: 7-Stage Governance Pipeline
        GCL->>GCL: 1. Classify (scenario type)
        GCL->>GCL: 2. Predict (forecast impact)
        GCL->>GCL: 3. Interpret (extract objectives)
        GCL->>GCL: 4. Plan (deterministic actions)
        GCL->>GCL: 5. Falsify (7 checks challenge proposal)
        GCL->>GCL: 6. Commit (sign DecisionPackage)
        Note over GCL: Ed25519 signature,<br/>expiry timestamp,<br/>scope binding (tenant + zone),<br/>SPIFFE URI identity
    end

    GCL->>FLEET: POST /api/v2/intents<br/>(Signed DecisionPackage)

    rect rgb(232, 245, 233)
        Note over FLEET: v2 Intent Admission (957 lines)
        FLEET->>FLEET: Verify Ed25519 signature with GCL public key
        FLEET->>FLEET: Check expiry timestamp
        FLEET->>FLEET: Validate scope binding
        FLEET->>FLEET: Verify proposer identity (SPIFFE)
        FLEET->>FLEET: Check evidence reference integrity
        FLEET->>FLEET: Enforce fleet authorization policy
    end

    alt Admission passed
        FLEET->>OPS: Create FleetOperation (RECEIVED)
        OPS->>OPS: RECEIVED -> ACCEPTED
        OPS->>OPS: ACCEPTED -> PLANNED
        OPS->>OPS: PLANNED -> AUTHORIZED
        OPS->>K8S: AUTHORIZED -> ACTUATING<br/>(apply CRD changes)
        K8S-->>OPS: Reconciliation result
        OPS->>OPS: ACTUATING -> OBSERVING
        OPS->>OPS: OBSERVING -> VERIFIED
        OPS->>OPS: VERIFIED -> SUCCEEDED
        FLEET->>ARE: WriteEntry (hash-chained,<br/>correlation-indexed)
        ARE-->>FLEET: Proof receipt (p50=1.7ms)
    else Admission failed
        FLEET-->>GCL: 401/403 rejection<br/>(invalid signature, expired, unauthorized)
        FLEET->>ARE: WriteEntry (auth failure / RBAC denial)
    end
```

---

## 4. 17-Phase Operation Lifecycle

State diagram for FleetOperation phases from RECEIVED through completion or failure.

```mermaid
stateDiagram-v2
    [*] --> RECEIVED: Intent admitted

    RECEIVED --> ACCEPTED: Validation passed
    RECEIVED --> REJECTED: Validation failed

    ACCEPTED --> PLANNED: Placement/routing computed
    ACCEPTED --> FAILED_PLANNING: No feasible plan

    PLANNED --> AUTHORIZED: Authorization checks passed
    PLANNED --> UNAUTHORIZED: Policy denied

    AUTHORIZED --> ACTUATING: Begin CRD changes
    AUTHORIZED --> AUTHORIZATION_REVOKED: Authorization expired

    ACTUATING --> OBSERVING: Changes applied
    ACTUATING --> ACTUATION_FAILED: Apply error

    OBSERVING --> VERIFIED: Observed matches desired
    OBSERVING --> OBSERVATION_TIMEOUT: Deadline exceeded
    OBSERVING --> DRIFT_DETECTED: State divergence

    VERIFIED --> SUCCEEDED: All gates passed
    VERIFIED --> SLO_BREACH: SLO check failed

    SUCCEEDED --> [*]

    REJECTED --> FAILED
    FAILED_PLANNING --> FAILED
    UNAUTHORIZED --> FAILED
    AUTHORIZATION_REVOKED --> FAILED
    ACTUATION_FAILED --> FAILED
    OBSERVATION_TIMEOUT --> FAILED
    DRIFT_DETECTED --> FAILED
    SLO_BREACH --> FAILED

    FAILED --> [*]

    state FAILED {
        direction LR
        note left of FAILED
            Records:
            - Failure reason
            - Phase at failure
            - Full evidence chain
        end note
    }

    note right of RECEIVED
        Each phase transition
        is recorded to the
        evidence chain
    end note

    note right of SUCCEEDED
        Evidence written to
        ARE immutable ledger
    end note
```

**Primary path:** RECEIVED -> ACCEPTED -> PLANNED -> AUTHORIZED -> ACTUATING -> OBSERVING -> VERIFIED -> SUCCEEDED

**Failure branches:** Each phase can fail independently, recording the failure reason, phase, and full evidence chain.

---

## 5. 8-Hour Soak Latency Profile

Representation of the on-cluster soak latency behavior over 8 hours with degradation injections.

```mermaid
xychart-beta
    title "8-Hour Soak: E2E Latency (pod-to-pod, on-cluster)"
    x-axis "Time (hours)" [0, 1, 2, 3, 4, 5, 6, 7, 8]
    y-axis "Latency ms" 0 --> 700
    line "p50 Latency" [154, 149, 152, 155, 150, 158, 153, 160, 154]
    line "p95 Latency" [485, 470, 490, 495, 480, 500, 488, 510, 485]
    line "p99 Latency" [606, 590, 610, 620, 600, 630, 615, 640, 606]
```

**Soak metrics summary:**

```mermaid
graph LR
    subgraph SOAK["8-Hour Production-Emulation Soak (HubCluster, On-Cluster)"]
        M1["5,534<br/>governance cycles"]
        M2["100%<br/>success rate"]
        M3["154ms<br/>p50 latency"]
        M4["0<br/>errors"]
        M5["15/15<br/>injections passed"]
        M6["95/95<br/>chain verifications"]
        M7["1.1s<br/>max recovery"]
        M8["0<br/>latency drift"]
    end

    style SOAK fill:#e8f5e9,stroke:#2E7D32,stroke-width:2px
    style M1 fill:#c8e6c9,stroke:#2E7D32
    style M2 fill:#c8e6c9,stroke:#2E7D32
    style M3 fill:#c8e6c9,stroke:#2E7D32
    style M4 fill:#c8e6c9,stroke:#2E7D32
    style M5 fill:#c8e6c9,stroke:#2E7D32
    style M6 fill:#c8e6c9,stroke:#2E7D32
    style M7 fill:#c8e6c9,stroke:#2E7D32
    style M8 fill:#c8e6c9,stroke:#2E7D32
```

**Degradation injection profile (15/15 passed):**

```mermaid
gantt
    title Degradation Injections Over 8-Hour Soak
    dateFormat HH:mm
    axisFormat %H:%M

    section Burst-50
    Burst injection 1 (0 errors)          :b1, 01:00, 2m
    Burst injection 2 (0 errors)          :b2, 03:00, 2m
    Burst injection 3 (0 errors)          :b3, 05:00, 2m
    Burst injection 4 (0 errors)          :b4, 07:00, 2m

    section Invalid Intent
    Invalid submission 1 (401 reject)     :i1, 01:30, 1m
    Invalid submission 2 (401 reject)     :i2, 03:30, 1m
    Invalid submission 3 (401 reject)     :i3, 05:30, 1m
    Invalid submission 4 (401 reject)     :i4, 07:30, 1m

    section Upstream Reset
    Governance reset 1 (~1s recovery)     :r1, 02:00, 2m
    Governance reset 2 (~1s recovery)     :r2, 04:00, 2m
    Governance reset 3 (~1s recovery)     :r3, 06:00, 2m
    Governance reset 4 (~1s recovery)     :r4, 07:45, 2m

    section Expired Event
    Expired event 1 (403 reject)          :e1, 02:30, 1m
    Expired event 2 (403 reject)          :e2, 04:30, 1m
    Expired event 3 (403 reject)          :e3, 06:30, 1m
```

---

## 6. Test Coverage Matrix

Per-system test counts and ecosystem test phases.

```mermaid
graph TB
    subgraph UNIT["Per-System Test Coverage (1,619 tests + 55 proofs + 63 BDD)"]
        subgraph DF_TESTS["deepfield-fleet: 295 tests"]
            DT1["Unit"]
            DT2["Integration"]
            DT3["Ecosystem Contract"]
            DT4["BDD"]
        end

        subgraph GCL_TESTS["GCL: 822 tests"]
            GT1["Unit"]
            GT2["33 EDD Rubric"]
            GT3["15 BDD"]
            GT4["300 Property Tests"]
        end

        subgraph FLEET_TESTS["fleet-llm-d: 462 tests"]
            FT1["436 Go Tests"]
            FT2["26 Python Tests"]
            FT3["55 Architecture Proofs"]
            FT4["63 BDD Scenarios"]
        end

        subgraph ARE_TESTS["ARE Ledger: 40 tests"]
            AT1["38 Rust Tests"]
            AT2["2 Python Tests"]
            AT3["Chain Integrity"]
            AT4["Concurrent Writes"]
        end
    end

    style UNIT fill:#f5f5f5,stroke:#616161,stroke-width:2px
    style DF_TESTS fill:#e8f4fd,stroke:#2196F3,stroke-width:1px
    style GCL_TESTS fill:#fff3e0,stroke:#FF9800,stroke-width:1px
    style FLEET_TESTS fill:#e8f5e9,stroke:#4CAF50,stroke-width:1px
    style ARE_TESTS fill:#fce4ec,stroke:#E91E63,stroke-width:1px
```

**Ecosystem stress test phases (HubCluster cluster, 42/48 passed):**

```mermaid
graph LR
    P1["Phase 1<br/>Smoke<br/>5/6"] --> P2["Phase 2<br/>Performance<br/>1/2"]
    P2 --> P3["Phase 3<br/>Pressure<br/>7/7"]
    P3 --> P4["Phase 4<br/>Edge Cases<br/>7/9"]
    P4 --> P5["Phase 5<br/>Degradation<br/>10/10"]
    P5 --> P6["Phase 6<br/>Soak<br/>6/6"]
    P6 --> P7["Phase 7<br/>Pen Testing<br/>5/5"]
    P7 --> P8["Phase 8<br/>Chaos<br/>1/3"]

    style P1 fill:#fff9c4,stroke:#F9A825,stroke-width:2px
    style P2 fill:#fff9c4,stroke:#F9A825,stroke-width:2px
    style P3 fill:#c8e6c9,stroke:#2E7D32,stroke-width:2px
    style P4 fill:#fff9c4,stroke:#F9A825,stroke-width:2px
    style P5 fill:#c8e6c9,stroke:#2E7D32,stroke-width:2px
    style P6 fill:#c8e6c9,stroke:#2E7D32,stroke-width:2px
    style P7 fill:#c8e6c9,stroke:#2E7D32,stroke-width:2px
    style P8 fill:#ffcdd2,stroke:#C62828,stroke-width:2px
```

Legend: Green = all passed, Yellow = partial pass, Red = majority failed (single-pod ceiling)

---

## 7. Heterogeneous Inference Routing

Prompt classification and routing to cost-appropriate hardware tiers.

```mermaid
graph TD
    PROMPT["Incoming Inference Prompt"] --> CLASSIFIER["Semantic Classifier<br/>(prompt complexity analysis)"]

    CLASSIFIER --> SIMPLE["Simple<br/>Factual lookups,<br/>short answers"]
    CLASSIFIER --> STANDARD["Standard<br/>Moderate reasoning,<br/>structured output"]
    CLASSIFIER --> COMPLEX["Complex<br/>Multi-step reasoning,<br/>long-form generation"]

    subgraph CPU_TIER["CPU Tier -- $0.60/hr"]
        XEON["Intel Xeon 6<br/>(256 cores, AMX)"]
        OVMS["OVMS Runtime<br/>(C++ continuous batching)"]
        INT8["INT8 Precision"]
        XEON --> OVMS --> INT8
    end

    subgraph GPU_TIER["GPU Tier -- $12-32/hr"]
        GPU["GPU Accelerator<br/>(Intel Gaudi / NVIDIA H100)"]
        VLLM["vLLM Runtime<br/>(PagedAttention)"]
        FP16["FP16 Precision"]
        GPU --> VLLM --> FP16
    end

    SIMPLE --> CPU_TIER
    STANDARD --> CPU_TIER
    COMPLEX --> GPU_TIER

    CPU_TIER --> RESP1["Response"]
    GPU_TIER --> RESP2["Response"]

    SAVINGS["Up to 53x cost reduction<br/>for simple/standard prompts"]

    style PROMPT fill:#e3f2fd,stroke:#1565C0,stroke-width:2px
    style CLASSIFIER fill:#f3e5f5,stroke:#7B1FA2,stroke-width:2px
    style SIMPLE fill:#c8e6c9,stroke:#2E7D32
    style STANDARD fill:#c8e6c9,stroke:#2E7D32
    style COMPLEX fill:#ffcdd2,stroke:#C62828
    style CPU_TIER fill:#e8f5e9,stroke:#2E7D32,stroke-width:2px
    style GPU_TIER fill:#fff3e0,stroke:#E65100,stroke-width:2px
    style SAVINGS fill:#fffde7,stroke:#F57F17,stroke-width:2px
    style RESP1 fill:#f5f5f5,stroke:#757575
    style RESP2 fill:#f5f5f5,stroke:#757575
```

**Benchmarked models on the heterogeneous pipeline:**

```mermaid
graph LR
    subgraph MODELS["Validated Models"]
        M1["Granite 350M<br/>Nanoagent tasks"]
        M2["Granite 2B INT8<br/>AMX accelerated"]
        M3["Granite 4.1 3B<br/>Latest generation"]
        M4["Phi-3-Mini 3.8B<br/>Strong reasoning"]
        M5["Qwen 2.5 3B<br/>Multilingual"]
    end

    M1 & M2 & M3 --> CPU["CPU Tier<br/>Intel Xeon 6 / OVMS"]
    M4 & M5 --> EITHER["CPU or GPU Tier<br/>(based on prompt complexity)"]

    style MODELS fill:#f5f5f5,stroke:#616161,stroke-width:1px
    style CPU fill:#e8f5e9,stroke:#2E7D32
    style EITHER fill:#e3f2fd,stroke:#1565C0
```
