# Trusted identity boundary

llm-d-fleet consumes identity; it does not own customer login, OAuth/OIDC,
API-key lifecycle, subscriptions, or commercial rate plans. Those functions
belong to an identity-aware API or Model Gateway. Fleet uses the resulting
verified identity only for tenant admission, fleet placement constraints,
internal authorization, and quota enforcement.

## Modes

`--identity-provider` (or `FLEET_IDENTITY_PROVIDER`) selects one mode:

- `disabled` is suitable only for unexposed development deployments.
- `hmac` preserves the existing fleet bearer-token compatibility contract.
- `trusted-proxy` accepts a short-lived Ed25519-signed identity assertion from
  an authenticated upstream gateway.

If the setting is omitted, an existing `FLEET_AUTH_SECRET` selects `hmac`;
otherwise authentication remains disabled. This preserves existing installs.

## Trusted-proxy contract

The gateway sends `X-Fleet-Identity-Assertion` containing two base64url parts:

```text
base64url(JSON payload).base64url(Ed25519 signature over the JSON bytes)
```

The payload fields are `kid`, `sub`, `tenant`, `roles`, optional `groups`,
`subscription`, `entitlements`, `request_id`, and required `iss`, `aud`, `iat`,
and `exp`. Times are Unix seconds. Assertions must:

- have a signature from `FLEET_TRUSTED_IDENTITY_KEYS_JSON`;
- match `FLEET_TRUSTED_IDENTITY_ISSUER` and
  `FLEET_TRUSTED_IDENTITY_AUDIENCE`;
- fit inside `FLEET_TRUSTED_IDENTITY_MAX_AGE` (five minutes by default); and
- arrive over a connection with a verified client certificate by default.

`FLEET_TRUSTED_PROXY_REQUIRE_MTLS=false` exists for deployments where an
authenticated local sidecar terminates mTLS. It is a deliberate weakening and
the operator must prevent direct access to the fleet listener.

The key JSON maps key IDs to `base64:`-prefixed Ed25519 public keys. Private
keys remain in the upstream gateway. Multiple public keys permit overlap
during rotation.

## Header authority

Fleet consumes and removes the assertion before dispatch. It also removes
caller-provided actor, tenant, target-cluster, routing-reason, data-plane,
Router-upstream, and destination-endpoint headers. The inference gateway
reconstructs routing headers only after fleet policy selects an exact model and
qualified provider.

The signed identity is not routing authority. It cannot add an incompatible,
stale, draining, unhealthy, or policy-ineligible provider and cannot request a
different physical model.

## Current limitation

The optional JSON-RPC listener currently supports only `hmac`. Startup rejects
other identity providers when that listener is enabled. A future JSON-RPC
identity contract should use authenticated transport identity rather than
copying HTTP headers.
