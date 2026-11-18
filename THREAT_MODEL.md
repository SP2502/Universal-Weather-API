# UWA v1 threat model and boundaries

UWA is a deterministic self-hosted BYOK gateway. No runtime model inference is used.

| Boundary | Controls | Residual risk / required acceptance |
|---|---|---|
| Internet to gateway | IP admission, finite/query bounds, hashed client keys and per-route scopes | No authenticated per-key fairness limiter; edge TLS and volumetric protection are operator responsibilities |
| Authentication to PostgreSQL | Parameterized SQL, authoritative revocation/expiry lookup, bounded handshake connector and auth lookup context | Disposable local migration/key lifecycle checks passed; repeat on the intended deployment. External SQL pools must supply equivalent connection bounds |
| Gateway to provider | Fixed production endpoints, TLS verification, private-IP filtering, bounded reads, attempt/deadline/quota controls | Whole-chain proxy/redirect behavior must be qualified before public release |
| Provider to canonical schema | Field presence/units/age/timezone and requested-point tolerance; normalization inside fallback | Plausible but erroneous weather is not independently meteorologically verified |
| Redis to delivery/admission | Cache revalidation, registry/policy eligibility, atomic shared allowance admission | Redis is an integrity-sensitive dependency; trusted isolated network, durable counters and failure recovery required |
| Operator/local files | Dedicated Linux/Windows accounts, restricted secret/config paths and offline admin CLI | Disposable native service lifecycle tests passed; target-host permissions and recovery still need review |
| Source to release | Checksummed modules and source checks | Scans, immutable inputs, SBOM, provenance, contributor rights and native artifact qualification required |

Key hash comparisons use constant-time operations; complete invalid/nonexistent-key HTTP responses do not claim constant timing. CORS defaults permit noncredentialed requests from any origin; it is not an origin allowlist. Admission is per observed/trusted-proxy client IP, not per API key. No cookie-authenticated administrative HTTP endpoints are provided.

Liveness is isolated from client Redis admission; readiness still depends on required stores. Global shutdown context covers HTTP and bounded usage/last-used cleanup. These controls require native and real-infrastructure acceptance; source implementations alone are not certification.

See [security policy](SECURITY.md), [release security baseline](docs/RELEASE_SECURITY_BASELINE.md), and [release status](docs/release-status.md) for current evidence and remaining gates. Never infer vendor billing or retention rights from this repository's MIT license.
