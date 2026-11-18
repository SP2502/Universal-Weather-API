# Release status

**Status on 18 November 2026: locally validated source candidate. Public production release is still pending.** The checks below covered a disposable, self-hosted, noncommercial setup. They do not establish the capacity or legal eligibility of another deployment.

## Completed locally

- Go 1.27.4 tests, race checks, vet, Staticcheck, module verification, vulnerability scan, and release builds passed in the last release preparation run.
- Disposable PostgreSQL and Redis acceptance, including migrations and shared-state behavior, passed.
- Bounded live requests exercised OpenWeather and WeatherAPI for current weather and LocationIQ for geocoding. The operator capped live requests at 100 per provider.
- Ubuntu systemd and Windows Service Control Manager install, start, update, rollback, and removal paths were exercised on local hosts.
- Docker image scanning found no high or critical findings in the scanned release image. A 10,000-request fixture-backed soak completed on a healthy host without request errors.
- A phone on the local network reached the gateway through a temporary local Caddy HTTPS endpoint. This did not test a public-domain certificate or renewal.

Detailed logs and the full repair history are retained by the maintainer outside the public source tree.

## Remaining before a public production launch

1. Publish the repository, configure a private security-reporting contact, and run the final GitHub CI workflow on the published commit.
2. Verify the actual deployment domain, trusted proxy, certificate issuance and renewal, ingress controls, and recovery procedure.
3. Review each provider account's current plan, permitted uses, attribution, cache/retention terms, and explicit request budget for the deployed workload. Forecast needs a separately configured eligible forecast provider.
4. Measure production capacity and recovery on the intended host. The local soak and native service tests are useful evidence, not a production load guarantee.

See [release security baseline](RELEASE_SECURITY_BASELINE.md), [configuration](configuration.md), and [OpenAPI](openapi.yaml).
