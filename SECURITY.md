# Security policy — Universal Weather API

> **Status: public source preview; production security acceptance not complete.** This file documents the intended controls and the remaining responsibilities. It is not a penetration-test certificate.

## Reporting vulnerabilities

Use [GitHub private vulnerability reporting](https://github.com/SP2502/Universal-Weather-API/security/advisories/new) to report a suspected security issue. This channel is enabled for the public repository. Include the affected version, reproduction steps, and impact if known. Keep exploit details private until a coordinated fix is available. No response-time guarantee is offered.

## Security boundaries in source

- API keys are generated with secure random bytes and stored as hashes. Authentication checks authoritative backing storage for revocation/expiry on every protected request.
- Invalid, revoked, expired or unknown keys return `401`; authentication-storage failures return retryable `503` with no credential or database detail exposed.
- Capability scopes are checked before protected handlers. Production and staging require PostgreSQL, Redis-backed quotas, explicit provider budgets, and policy-eligible sources.
- The gateway sends upstream calls with bounded HTTP clients and SSRF protections; deployment egress restrictions remain recommended. Geocoding and regional providers must satisfy licensing, geographic and quota constraints.
- `/metrics` is **disabled by default**. If enabled, it requires a loopback binding and an operator-configured bearer token. Public health/readiness routes intentionally expose limited service status.
- Runtime HTTP listener does **not** itself terminate TLS. Use a trusted HTTPS reverse proxy or ingress, and configure HSTS there. The gateway adds HSTS only to connections it receives over TLS directly. Plain-HTTP API-key access on untrusted networks is forbidden.
- The default CORS configuration allows non-credentialed cross-origin requests; it is **not an origin-restriction policy**. Restrict origins at the trusted proxy or configure a reviewed CORS policy for browser-facing deployments.
- Application logs redact known sensitive attribute names and common bearer/query key formats. Log redaction is defense-in-depth, not a substitute for secret scanning or secure operational access.
- The lean-v1 router excludes deferred air-quality, marine, historical and alert endpoints in production/staging; experimental code is not an active release feature.

## Before production deployment

- Conduct authorized threat modeling, static analysis, dependency and container vulnerability scanning using a supported Go patch release.
- Verify actual PostgreSQL and Redis failure behavior; two-instance rate limits and auth-key rotation/revocation.
- Test SSRF, malicious redirects and DNS changes, proxy forwarding, CORS, TLS termination, request/response limits, injection, errors and log redaction.
- Verify native Windows and Linux service privileges, installation, restarts, safe upgrades and rollback.
- Obtain and test approved third-party provider accounts and their terms; never publish actual API keys or session material.
- Preserve an evidence-based test report with exact commands, versions and limitations. See [release acceptance gates](docs/RELEASE_SECURITY_BASELINE.md).
