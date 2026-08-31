# Dependency Update Policy

> Maintainer policy for this repository. Downstream operators remain
> responsible for scanning and patching their complete deployment.

| Field | Value |
|-------|-------|
| **Scope** | All direct and transitive dependencies across Go, Rust, Node.js, Python, and container base images |
| **Owner** | fleet-llm-d Maintainers |
| **Last Updated** | August 2026 |

## Update Cadence

| Ecosystem | Cadence | Gate | Files |
|-----------|---------|------|-------|
| Go modules | Weekly (Monday CI) | govulncheck must pass | `go.mod`, `go.sum` |
| Rust crates | Weekly (Monday CI) | cargo audit must pass | `Cargo.toml`, `Cargo.lock` |
| npm packages | Weekly (Monday CI) | npm audit (critical) must pass | `web/package.json`, `web/package-lock.json` |
| Python packages | Monthly | pip-audit must pass | `python/requirements.txt` |
| Base container images | Monthly | Trivy CRITICAL/HIGH scan must pass | `deploy/docker/Dockerfile.controller`, `Dockerfile.agent`, `Dockerfile.inference-mock` |

The Monday CI schedule is defined in `.github/workflows/security.yaml` (`cron: "0 8 * * 1"`).

## Automation

### Dependabot / Renovate Configuration

- Enable Dependabot or Renovate for all four ecosystems.
- PRs are auto-created against `main` with the `dependencies` label.
- Each PR triggers the full security pipeline: govulncheck, cargo audit, npm audit, Trivy.
- PRs also trigger `dependency-review-action@v4` (`.github/workflows/security.yaml`) which blocks on HIGH+ severity introductions.

### PR Review Requirements

Every dependency update PR must pass:

1. All unit tests (`make test-unit`).
2. Contract tests (`make test-contracts`) to verify proto/CRD compatibility.
3. Security scan pipeline (govulncheck, cargo audit, npm audit, Trivy).
4. BDD scenarios (`make test-bdd`) for integration regressions.

## Accept / Defer / Reject Criteria

| Decision | Criteria |
|----------|----------|
| **Accept immediately** | Security patch for any severity. Minor/patch version bump that passes all CI gates. |
| **Accept with review** | Major version bump. Any change to `pkg/auth/`, `pkg/ledger/`, `crates/fleet-ledger/`, or `pkg/store/postgres/` dependencies. New transitive dependency introduction. |
| **Defer** | Update breaks a test but is not security-related. Upstream has known regressions. Defer for max 14 days, then re-evaluate. |
| **Reject** | Introduces a dependency with a known unpatched CVE. Adds a non-permissive license (GPL, AGPL) to a library linked into fleet-llm-d binaries. Dependency is unmaintained (no commits in 12+ months) with alternatives available. |

## Dependency Age SLO

| Metric | Target |
|--------|--------|
| Direct dependency age | < 90 days behind latest release |
| Transitive dependency age | < 180 days behind latest release |
| Security-critical deps (crypto, TLS, auth) | < 30 days behind latest release |

### Measurement

Track dependency freshness using:

- `go list -m -u all` for Go modules.
- `cargo outdated` for Rust crates.
- `npm outdated` for Node.js packages.

Report dependency age monthly. Flag any direct dependency older than 90 days as a risk item.

## Specific Dependency Notes

| Dependency | Package | Policy |
|------------|---------|--------|
| `lib/pq` | Go (PostgreSQL driver) | Only direct non-stdlib Go dependency. Pin to latest stable. Monitor for libpq CVEs. |
| `tonic` | Rust (gRPC) | Used in `crates/fleet-agent`, `crates/fleet-ledger`. Coordinate updates with proto schema changes in `api/proto/`. |
| `axum` | Rust (HTTP) | Used in `crates/fleet-agent`. Test fleet-agent HTTP endpoints after updates. |
| `tokio` | Rust (async runtime) | Core runtime for all Rust crates. Major updates require soak testing. |
| `next` | npm (dashboard framework) | Used in `web/`. Major updates require dashboard E2E validation. |
| UBI base images | Container | Source: `registry.access.redhat.com`. Monthly pull of latest UBI tags. Verify non-root USER 65534:65534 and readOnlyRootFilesystem after update. |

## Air-Gapped Deployments

For sovereign cloud customers without internet access:

- Mirror approved dependency versions in an internal artifact registry.
- Ship dependency updates as part of the signed release bundle (cosign in `.github/workflows/release.yaml`).
- Include the CycloneDX SBOM with each release for offline audit.

## References

- `.github/workflows/security.yaml` -- weekly security scans and dependency review
- `.github/workflows/release.yaml` -- cosign signing and SBOM attachment
- `deploy/docker/Dockerfile.*` -- base image definitions
