# Penetration Testing Schedule

> Recommended product-security plan. No independent penetration test is
> claimed by `v0.3.0`; formal security review remains an open proof gate.

| Field | Value |
|-------|-------|
| **Scope** | fleet-llm-d control plane, data plane, dashboard, and cross-cluster communication |
| **Owner** | fleet-llm-d Security Lead |
| **Last Updated** | August 2026 |

## Annual Penetration Test

The recommended program commissions an annual independent assessment covering
the deployment profiles included in the agreed scope. Completion requires a
retained assessor report and remediation evidence; this repository does not
assert that such an engagement has occurred.

### Schedule

| Quarter | Activity |
|---------|----------|
| Q1 | Scope definition and rules of engagement |
| Q2 | Full penetration test execution (2-week engagement) |
| Q3 | Remediation of findings, re-test of Critical/High items |
| Q4 | Automated regression testing, report finalization |

### Test Categories

#### 1. API Fuzzing (15 REST Endpoints + gRPC)

Target all endpoints registered in `pkg/server/routes.go`:

| Endpoint Group | Endpoints | Focus Areas |
|----------------|-----------|-------------|
| Cluster management | `GET/POST /api/v1/clusters`, `DELETE /api/v1/clusters/{id}`, `POST .../drain`, `POST .../activate` | Input validation, path traversal in `{id}` |
| Pool management | `GET /api/v1/pools`, `GET /api/v1/pools/{name}/state`, webhook endpoint | Webhook payload injection |
| Tenant management | `GET/POST /api/v1/tenants`, `GET /api/v1/tenants/{id}/usage` | Tenant ID enumeration, cross-tenant data access |
| Agent communication | `POST /api/v1/agent/status`, `.../metrics`, `.../events`, `GET .../policies/{cluster_id}` | Agent impersonation, malformed metrics |
| Rollout management | `GET/POST /api/v1/rollouts`, `POST .../promote`, `POST .../rollback` | Unauthorized promotion/rollback |
| Verification | `GET /api/v1/verify/chains` | Chain verification bypass |
| Cost/Auth | `GET /api/v1/cost/*`, `POST /api/v1/auth/refresh` | Token replay, cost data exfiltration |
| gRPC | fleet-agent to fleet-controller, ARE Ledger client | mTLS bypass, proto deserialization attacks |

#### 2. Authentication and Authorization Bypass

| Test | Description | Reference |
|------|-------------|-----------|
| Token forgery | Attempt HMAC-SHA256 token forgery against `pkg/auth/token.go` | `test/security/pen_test.py` |
| Token replay | Replay expired tokens, test refresh token rotation | `test/security/pen_test.py` |
| Role escalation | Attempt viewer-to-admin escalation via API calls | `test/security/auth_test.go` |
| RBAC bypass | Access resources outside granted ClusterRole scope | `deploy/kustomize/base/rbac.yaml` |
| Rate limit bypass | Circumvent `pkg/auth/ratelimit.go` per-key token bucket | `test/security/pen_test.py` |

#### 3. Tenant Isolation Escape

| Test | Description | Reference |
|------|-------------|-----------|
| Cross-tenant pool access | Access FleetInferencePool belonging to another TenantProfile | `pkg/tenant/quota/enforcer.go` |
| Tenant usage exfiltration | Read metering data for unauthorized tenants via `/api/v1/tenants/{id}/usage` | `pkg/tenant/metering/` |
| Namespace escape (Hub) | Attempt operations outside the fleet namespace in Hub mode | `pkg/controller/webhook.go` |

#### 4. Cross-Cluster Escalation (Federated Mode)

| Test | Description |
|------|-------------|
| Cluster impersonation | Register a rogue fleet-agent with forged cluster credentials |
| Cross-cluster routing manipulation | Modify FleetRoutingPolicy to redirect traffic to attacker-controlled cluster |
| KV cache interception | Intercept NIXL-based KV cache transfers between clusters (`crates/kv-transfer/`) |
| Federation peer spoofing | Spoof peer identity in `pkg/lifecycle/federation.go` |

#### 5. Additional Attack Vectors

| Test | Description | Reference |
|------|-------------|-----------|
| SSRF via agent status | Inject URLs in agent status payloads | `test/security/pen_test.py` |
| Header injection | HTTP header injection via API parameters | `test/security/pen_test.py` |
| DecisionPackage tampering | Modify GCL-signed DecisionPackage in transit | `pkg/intents/` |
| Large payload DoS | Submit oversized payloads to all POST endpoints | `test/security/pen_test.py` |

## Quarterly Automated Scans

Run quarterly using the existing test infrastructure.

| Tool | Target | Frequency |
|------|--------|-----------|
| `test/security/pen_test.py` | All REST endpoints with SSRF, injection, escalation tests | Quarterly |
| `test/security/auth_test.go` | AuthN/AuthZ, tenant isolation, RBAC | Every PR + quarterly |
| Trivy (`.github/workflows/security.yaml`) | Container images | Weekly + quarterly deep scan |
| gosec | Go static analysis for security issues | Weekly |

## Findings Management

1. All findings are classified using CVSS v3.1 and logged as GitHub Security Advisories.
2. Critical and High findings follow the SLAs in `docs/security/cve-response-process.md`.
3. Findings are mapped back to the downstream product security review and tracked to closure.
4. Remediation is verified by re-running the specific test case from `test/security/pen_test.py`.

## References

- `pkg/server/routes.go` -- all 15+ REST API endpoint registrations
- `test/security/pen_test.py` -- automated penetration test suite
- `test/security/auth_test.go` -- authentication and authorization tests
- `test/harness/` -- test harness infrastructure
- `.github/workflows/security.yaml` -- CI security scanning
