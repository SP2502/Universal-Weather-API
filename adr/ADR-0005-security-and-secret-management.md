# ADR-0005: Security Hardening, Secret Abstraction, and SSRF Defense

## Status
Accepted

## Context
The Universal API Platform acts as a multi-tenant gateway that routes inbound client requests to external third-party API providers (weather, geocoding, etc.). As the platform matures, it faces distinct security challenges:
1. **Multi-tenant isolation and API key lifecycle**: Need secure generation, constant-time verification, storage hashing, revocation, and zero-downtime key rotation.
2. **Provider credential security**: Provider API keys should be decoupled from direct OS environment reads to support future secret backends (Vault, AWS Secrets Manager).
3. **Outbound network security (SSRF)**: Because the platform makes outbound HTTP requests to dynamically configured or provider-reported endpoints, it is vulnerable to Server-Side Request Forgery targeting cloud metadata (169.254.169.254), loopback, or private internal network resources.
4. **Denial of Service & Abuse**: Inbound and outbound size limits, rate limiting, and defensive HTTP headers are required.

## Decisions

### 1. Cryptographic Key Lifecycle
- Keys are generated with 256 bits of CSPRNG entropy (`crypto/rand`) with public prefixes (`uap_live_...`).
- Plaintext keys are never stored; only SHA-256 digests are persisted.
- Key verification uses constant-time comparisons (`crypto/subtle.ConstantTimeCompare`) where appropriate.
- Keys support `ACTIVE`, `REVOKED`, `EXPIRED`, and rotation grace periods (allowing secondary key hash coexistence during transitions).

### 2. Secret Store Abstraction (`internal/secrets`)
- Define a minimal `SecretStore` interface (`GetSecret`, `SetSecret`).
- Provide standard implementations: `EnvSecretStore` for environment-injected secrets and `MemorySecretStore` for deterministic unit testing.
- Provider adapters receive resolved credentials or reference the secret store rather than issuing unmediated `os.Getenv` calls.

### 3. Outbound SSRF Mitigation (`internal/http/client`)
- Construct an outbound HTTP transport with a custom `DialContext` that evaluates every resolved IP address from DNS.
- Reject connections to IPv4 loopback (`127.0.0.0/8`), IPv6 loopback (`::1`), RFC1918 private subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), link-local/cloud metadata (`169.254.0.0/16`), and carrier-grade NAT (`100.64.0.0/10`).
- Validate redirect targets against the SSRF policy on each hop up to a maximum of 3 redirects.
- Provide a strict test-only option (`AllowPrivateIPs`) to allow local integration tests running on `httptest.Server`.

### 4. Edge Security & Audit Logging
- Enforce strict HTTP security headers (`X-Content-Type-Options`, `X-Frame-Options`, `CSP`, `Referrer-Policy`).
- Implement request body size bounding (64 KB ceiling) and response size bounding (1 MB `LimitReader`).
- Implement in-memory token-bucket rate limiting per API key / client IP.
- Implement structured security audit event emission (`internal/audit`) for critical security events.

## Consequences

### Positive
- Robust defense against SSRF, cloud metadata exfiltration, and local network traversal.
- Complete timing side-channel resistance in key verification.
- Pluggable secret backend architecture without refactoring provider adapters.
- Defense-in-depth against payload flooding and upstream poison data.

### Negative / Trade-offs
- Custom DNS dialing introduces a minor overhead for IP validation on outbound connection establishment.
- Local tests against `httptest.Server` must explicitly enable the private IP bypass flag.
