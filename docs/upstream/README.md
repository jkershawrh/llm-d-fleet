# llm-d upstream contribution package

This directory contains draft material for starting an upstream discussion
with llm-d SIG Router. It is deliberately narrower than the complete
fleet-llm-d implementation.

The current portable product evidence is recorded in
`multicluster-product-conformance-2026-08-31.md`. It separates reusable product
claims from the disposable HubCluster/CpuCluster/GpuCluster reference environment.

For review, start with `reviewer-evidence-package.md`. It links the product
boundary, design, conformance result, release integrity evidence, limitations,
and independent reproduction protocol without requiring access to a private
deployment repository.

The proposed sequence is:

1. Review and post `rfc-discussion.md` as a GitHub discussion or RFC issue.
2. Present the problem and experience report in `#sig-router`.
3. Ask maintainers whether the work belongs in `llm-d-router`, a new
   `llm-d-incubation` repository, or a multi-cluster Well-Lit Path.
4. Revise `proposal-fleet-level-multicluster-orchestration.md` based on that
   feedback and submit it under `llm-d/llm-d/proposals/`.
5. Submit implementation and test changes as small DCO-signed pull requests.

Do not submit the historical `docs/proposals/fleet-orchestration.md` directly.
It describes superseded components and transport choices and is much broader
than the initial upstream discussion should be.
