# llm-d-fleet security documentation

These documents support engineering review of the OSS implementation:

- dependency and CVE response procedures;
- penetration-test planning;
- evidence-collection guidance; and
- mappings to selected security, AI-risk, and record-keeping frameworks.

They are not legal advice, audit reports, third-party attestations, or claims
that a deployment is compliant. Most controls depend on operator-owned
identity, TLS, network, storage, logging, backup, and incident-response systems
that are intentionally outside the portable OSS core.

The release-level security evidence is limited to the tests, scans, signed
artifacts, SBOMs, and provenance recorded in the
[reviewer evidence package](../upstream/reviewer-evidence-package.md). A formal
product threat-model review remains open.

The [trusted identity boundary](../architecture/trusted-identity-boundary.md)
defines the portable integration with an external Model/API Gateway. It is an
implemented contract with unit tests, not evidence that any particular product
gateway or production deployment has completed conformance testing.
