# Speculative Routing & Zero-Cost Prompt Classification

> **Exploratory design, not a released capability claim.** Classification is
> optional and cannot override exact-model compatibility, tenant policy, or
> provider health in the current llm-d-fleet contract.

**Author:** Jonathan Kershaw
**Date:** July 2026
**Status:** Proposal
**Audience:** AI Engineering, Praxis team, llm-d community

---

## Problem

Semantic routing — classifying prompts to route simple work to cheap CPU ($0.60/hr) and complex work to expensive GPU ($32/hr) — is a 53x cost lever. The upstream vLLM Semantic Router prototype proved the concept but used an LLM for classification, which takes 60-120s on CPU. That's unusable.

The question: how do you classify prompts at <10ms with near-zero compute overhead, at thousands of requests per second, on CPU?

## Key Insight

**The best classifier is no classifier.** The inference engine itself produces signals that reveal prompt complexity — TTFT, KV cache pressure, prefix cache hits — and these signals are already flowing through the fleet-llm-d EPP adapter. The remaining gap is in how we act on them.

## Five Approaches, Ranked by Compute Cost

### 1. Speculative Routing — Zero Classifier

Send every prompt to CPU first. Watch the clock.

```
Client → Praxis → fire CPU attempt
                   │
                   ├─ CPU responds in <500ms → done ($0.60/hr)
                   │
                   └─ CPU still thinking after 500ms
                      → fire GPU attempt in parallel ($32/hr)
                      → first good response wins
                      → cancel the loser
```

**Why this works:** Simple prompts are fast on CPU (sub-second on OVMS). Complex prompts are slow. The latency itself IS the classification. No ML model, no training data, no additional compute.

**Why only Praxis can do this:** Speculative routing requires owning the request lifecycle — starting one backend, monitoring latency, firing a second, racing the responses, canceling the loser. A stateless proxy (Envoy, LiteLLM, any ext_proc component) structurally cannot do this. Praxis's filter pipeline owns the request end-to-end.

**fleet-llm-d's role:** Tell Praxis which clusters have CPU capacity and which have GPU capacity via `BuildClusterHealth()`. Session affinity ensures follow-up messages in a conversation go to the same tier that won the first race.

| Metric | Value |
|--------|-------|
| Compute cost | Zero (inference itself is the classifier) |
| Latency added | 0ms (classification is implicit) |
| Accuracy | Perfect (the result proves the routing was correct) |
| Training data needed | None |
| Effort to implement | Medium (Praxis filter + timeout + parallel dispatch) |

### 2. Inference Telemetry as Classifier — Zero Additional Compute

The EPP signals already flowing through fleet-llm-d's pipeline contain the classification:

| EPP Metric | What it reveals |
|-----------|-----------------|
| `llm_d_epp_request_ttft_seconds` | Prompt complexity (20ms = simple, 2s = complex) |
| `llm_d_epp_average_kv_cache_utilization` | Memory pressure (2% = simple, 20% = complex) |
| `llm_d_epp_prefix_indexer_hit_ratio` | Cache efficiency (high hit = known prompt = route to cached cluster) |
| `llm_d_epp_flow_control_pool_saturation` | Capacity (>1.0 = this tier is full, route elsewhere) |

After observing N requests, fleet-llm-d builds a per-model, per-cluster routing table: "model X on cluster Y handles simple prompts in 200ms, complex prompts in 3s." New requests get routed based on this learned baseline.

**This costs nothing** — the signals are already flowing through the EPP adapter we shipped. fleet-llm-d's `MetricsFederator` already aggregates them. The only new code is a routing policy that acts on TTFT thresholds.

| Metric | Value |
|--------|-------|
| Compute cost | Zero (signals already exist) |
| Latency added | 0ms |
| Accuracy | High (after warmup period of ~100 requests) |
| Training data needed | None (learns from production traffic) |
| Effort to implement | Low (routing policy threshold) |

### 3. Locality-Sensitive Hashing — Sub-Microsecond

Hash each prompt into a 64-bit fingerprint using SimHash or MinHash. Similar prompts produce similar hashes. Each hash bucket gets a learned tier assignment.

```
"Summarize this quarterly report..."  → hash → bucket 0x7A3F → tier: complex
"What is the capital of France?"      → hash → bucket 0x12B4 → tier: simple
"Summarize this annual report..."     → hash → bucket 0x7A3E → tier: complex (similar hash)
```

**Why this is powerful for agents:** Enterprise AI workloads are structurally repetitive. Agents send the same system prompt with different user messages. The system prompt dominates the hash. After one observation, every request from that agent gets instant classification.

**Doubles as prefix cache routing:** Same hash = same prompt structure = same KV cache prefix. Route to the cluster that already has it cached. This solves P1-4 (prefix cache routing) as a side effect.

| Metric | Value |
|--------|-------|
| Compute cost | Sub-microsecond (integer operations only) |
| Latency added | <1μs |
| Accuracy | Medium-high (degrades for novel prompts) |
| Training data needed | None (learns from observations) |
| Effort to implement | Low (hash function + lookup table) |

### 4. Prefix Trie — Sub-Microsecond Structural Routing

Agent system prompts are typically 1,000-10,000 tokens of identical text per tenant. A trie on the first 50 tokens instantly identifies the tenant and their known tier.

```
"You are a helpful financial analyst..." → trie match → tenant: acme-finance → tier: complex
"You are a customer service agent..."    → trie match → tenant: acme-support → tier: simple
"Tell me a joke"                         → no trie match → fallback to LSH or speculative
```

This isn't semantic routing — it's structural routing. But it covers 80%+ of enterprise agent traffic because agents are template-driven.

| Metric | Value |
|--------|-------|
| Compute cost | Sub-microsecond (trie traversal) |
| Latency added | <1μs |
| Accuracy | High for agents, N/A for ad-hoc prompts |
| Training data needed | None (learns from tenant registration) |
| Effort to implement | Low (trie data structure + tenant mapping) |

### 5. 1-Bit Classifier — The Nuclear Option

BitNet b1.58 (Microsoft Research) proved ternary weights (-1, 0, 1) match FP16 model quality. For a classifier (not a generator), you need far less:

- **10M parameter 1-bit classifier** = <2MB binary
- **No floating point multiplication** — only integer addition
- **Intel AMX** on Xeon 6 handles INT8 matrix operations at near-SIMD speed
- Embedded directly in a Praxis filter via ONNX Runtime

This is the highest-accuracy approach but requires training data (prompt → tier labels) to bootstrap. The telemetry from approach #2 naturally produces this training data over time.

| Metric | Value |
|--------|-------|
| Compute cost | <1ms on CPU |
| Latency added | <1ms |
| Accuracy | Highest |
| Training data needed | Yes (~10K labeled examples) |
| Effort to implement | High (model training + ONNX embedding + Praxis filter) |

## Recommended Stack

Layer the approaches — each handles what the previous can't:

```
Request arrives
    │
    ├─ Prefix trie match? → known tenant → known tier → route      (Day 1)
    │
    ├─ LSH bucket has learned tier? → route                         (Day 1)
    │
    ├─ TTFT history for this model? → predict tier → route          (Day 1)
    │
    └─ Unknown prompt → speculative routing                         (Day 2)
       → fire CPU, escalate to GPU if slow
       → result feeds back into LSH + TTFT history
```

**Day 1** costs zero additional compute — it uses signals and data structures that already exist or are trivial to add. **Day 2** adds speculative routing for the long tail. **Future** adds the 1-bit classifier trained on the labeled data that Day 1 automatically produces.

## Architecture Alignment

| Component | Owner | Layer |
|-----------|-------|-------|
| Speculative routing (fire CPU, escalate GPU) | Praxis filter | L2 — AI data plane |
| LSH fingerprint → tier table | Praxis filter (stateful) | L2 — per-request |
| Prefix trie → tenant mapping | fleet-llm-d (extends SessionAffinityTable) | L3 — fleet state |
| TTFT-based learned routing | fleet-llm-d MetricsFederator → RoutingPolicy | L3 → L2 |
| 1-bit ONNX classifier (future) | Praxis filter (embedded) | L2 — per-request |
| Cluster CPU/GPU capacity signals | fleet-llm-d BuildClusterHealth | L3 — fleet routing |
| EPP inference telemetry | llm-d EPP → fleet-agent → fleet-llm-d | L1 → L3 |

This follows Jason Greene's "one authority per layer" principle:
- **Praxis** owns the per-request classification and routing decision
- **fleet-llm-d** owns the fleet-level capacity signals and learned routing state
- **llm-d EPP** owns the inference telemetry that feeds both

## Competitive Differentiation

| Capability | Red Hat (Praxis + fleet-llm-d) | LiteLLM | Envoy AI Gateway |
|-----------|------|---------|------------------|
| Speculative routing | Possible (owns request lifecycle) | Impossible (stateless proxy) | Impossible (ext_proc) |
| Inference telemetry routing | EPP signals already flowing | No inference awareness | No inference awareness |
| LSH prompt fingerprinting | Praxis filter | Not available | Not available |
| Cross-cluster tier routing | fleet-llm-d placement | Single-cluster only | No fleet concept |
| KV cache-aware routing | EPP prefix indexer signals | Not available | Not available |

The combination is structurally unique to Red Hat's stack: Praxis owns the request long enough to race backends, fleet-llm-d aggregates inference signals across clusters, and EPP provides the within-cluster intelligence that makes the signals meaningful. No other vendor has all three layers.

## Cost Impact

For a customer running 1M inference requests/day across CPU + GPU:

| Routing approach | GPU requests | CPU requests | Daily cost | Savings |
|-----------------|-------------|-------------|------------|---------|
| All GPU (no routing) | 1,000,000 | 0 | $768,000/yr | — |
| 50/50 split (manual) | 500,000 | 500,000 | $389,280/yr | 49% |
| Speculative routing (80/20) | 200,000 | 800,000 | $158,112/yr | **79%** |
| Perfect routing (90/10) | 100,000 | 900,000 | $81,396/yr | **89%** |

The difference between "manual 50/50" and "speculative routing 80/20" is $231K/year. The classification compute cost is effectively zero.
