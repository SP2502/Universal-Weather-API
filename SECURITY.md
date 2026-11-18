# Security policy — Universal Weather API

> **Status: pre-release candidate; production security acceptance not complete.** This file documents the intended controls and the remaining responsibilities. It is not a penetration-test certificate.

## Reporting vulnerabilities

**Before public GitHub release, the maintainer must configure a real private security-reporting channel.** No response SLA or external mailbox is verified for this source package. Do not send confidential reports to a guessed domain/email.

Once the repository is public, enable and prominently link GitHub **private vulnerability reporting / security advisories** or provide a separately verified dedicated security contact. Keep exploit details private until a coordinated fix is available. The maintainer must update this section with the actual reporting URL and response expectations before publication.

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

## Before release

- Conduct authorized threat modeling, static analysis, dependency and container vulnerability scanning using a supported Go patch release.
- Verify actual PostgreSQL and Redis failure behavior; two-instance rate limits and auth-key rotation/revocation.
- Test SSRF, malicious redirects and DNS changes, proxy forwarding, CORS, TLS termination, request/response limits, injection, errors and log redaction.
- Verify native Windows and Linux service privileges, installation, restarts, safe upgrades and rollback.
- Obtain and test approved third-party provider accounts and their terms; never publish actual API keys or session material.
- Preserve an evidence-based test report with exact commands, versions and limitations. See [release acceptance gates](docs/RELEASE_SECURITY_BASELINE.md).
