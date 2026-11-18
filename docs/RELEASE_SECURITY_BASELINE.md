# UWA v1 release security baseline

**Public production launch remains blocked pending deployment-specific acceptance.** Local evidence and remaining limits are summarized in [release status](release-status.md). This document is not a production certificate.

## Source and toolchain

- Use the supported **Go 1.27.4** security patch line in CI and release images. `go.mod` retains its Go 1.22 language compatibility minimum; this is **not** a recommendation to build or deploy with unsupported Go 1.22.
- `go mod verify` checks downloaded modules against `go.sum`. CI should run `go test -race ./...`, `go vet ./...`, No-LLM dependency check, and Linux/Windows cross-compilation with the supported compiler.
- Docker builder uses Go 1.27.4; runtime base uses Alpine 3.24. Pin container image digests at release time, and scan final images and dependencies for known vulnerabilities in a network-enabled CI environment.
- No binary produced using the offline PostgreSQL compilation stub is suitable for release or deployment.

## API correctness and fail-closed behavior

- Invalid, missing, revoked and expired API keys return 401. **An unreachable/erroring authentication store returns 503, without exposing driver errors or secrets.** These cases are covered by regression tests.
- Do not cache successful authentication decisions in a way that bypasses revocation.
- Production provider configuration must require reviewed quota budgets, shared backing Redis, licensing eligibility and regional policy checks before upstream requests.
- Model forecasts, historical reanalysis, observational current weather, and official alerts have different provenance rules. Do not advertise the deferred product routes in the lean-v1 public API.

## Acceptance gates before public launch

1. Reconfirm `go mod download`, `go mod verify`, actual `github.com/lib/pq` driver, and clean release builds using a supported Go toolchain on the published commit.
2. Real disposable PostgreSQL: schema creation and upgrades, key creation, revocation, rotation, expiry, outage status and restart persistence.
3. Real Redis: quota Lua across two gateway instances, rate-limit isolation, shared cache invalidation, dropped connections, and fail-closed behavior.
4. Real entitled provider smoke tests only after account/licensing/quotas reviewed; country-coverage and fallback correctness.
5. Native Windows SCM and Linux systemd lifecycle with safe install/rollback and limited privileges.
6. Security audit: secret scans, supply-chain/dependency audit (`govulncheck`), authorization, injection, SSRF, misconfigured proxy/CORS/TLS, fault injection, soak/load, and graceful shutdown.
7. Verify website/README only state features implemented in lean-v1, and all published API examples match the release OpenAPI document.

## Evidence discipline

Keep a report with exact commands, logs, Go version, OS, dependency hashes, container digests, exception list, and pass/fail/block status. No stub-assisted test or cross-compile counts as production certification. Stop and return report before applying repairs if following a report-only acceptance assignment.
