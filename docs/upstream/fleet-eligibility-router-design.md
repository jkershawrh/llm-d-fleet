# Fleet eligibility integration with llm-d Router

## Proposed ownership

fleet-llm-d produces a model-specific eligible cluster set. The cluster-scoped
llm-d EPP scores only that set, and the selected cluster's local EPP chooses the
serving endpoint. KServe may own each cluster's `LLMInferenceService` lifecycle.

The fleet filter covers registration identity, exact model and accelerator
compatibility, policy, health freshness, draining, authorization, and failure
domain. Router continues to own queue/KV/prefix/session scoring and flow
control. No adapter can add a provider rejected by fleet policy.

## Initial discovery contract

The reference adapter writes one file per exact model for the DNS- and
TLS-aware `multicluster-file-discovery` contract currently available on
upstream main, including separate routing and metrics hosts. Files are
deterministic and atomically replaced; an index is published only after all
model files succeed. This respects the current upstream guidance that a hub
EPP candidate set is homogeneous by model.

This is beta integration evidence, not released Router qualification. Router
v0.10.0 file discovery accepts literal IPv4 endpoints and cannot carry the DNS
authority/SNI needed for verified cross-cluster TLS. The adapter must remain
beta until a compatible upstream contract is released and tested; see
[`router-release-qualification.md`](router-release-qualification.md).

This file is an implementation bridge, not the desired permanent API. The
upstream design request is a native discovery contract that can carry stable
cluster identity, exact model, endpoint and metrics addresses, freshness,
draining, failure domain, TLS identity, and capability status.

## Signals and conformance

An optional decentralized signal contract exposes allowlisted pool-level
Prometheus gauges from each site. SWIM remains the membership and capability
plane. Conformance should cover exact-model isolation, stale and draining
removal, authenticated endpoint replacement, cluster loss, spoofed routing
headers, streaming cancellation, and explicit no-compatible-capacity errors.

Governance producers, immutable evidence, semantic classifiers, Praxis
productization, and any particular cross-cluster transport are outside this
initial upstream request.
