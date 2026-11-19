# Release status

**Status on 19 November 2026: public source preview, locally validated. Public production deployment is still pending.** The checks below covered a disposable, self-hosted, noncommercial setup. They do not establish the capacity or legal eligibility of another deployment.

## Completed locally

- Go 1.27.4 tests, race checks, vet, Staticcheck, module verification, vulnerability scan, and release builds passed in the last release preparation run.
- The complete Go suite also passed locally on 19 November 2026 with Go 1.26.7 after the NWS alert timeout repair.
- The public source preview has CI checks for native Windows and Linux tests and cross-platform builds; each published change must pass its own run.
- Disposable PostgreSQL and Redis acceptance, including migrations and shared-state behavior, passed.
- Bounded live requests exercised OpenWeather and WeatherAPI for current weather; LocationIQ and Geoapify for forward and reverse geocoding; Open-Meteo for forecast, derived daily forecast, geocoding, modeled air quality, historical reanalysis, and marine forecast; Sunrise-Sunset for astronomy; MET Norway and Bright Sky for forecast; and US NWS for forecast and active alerts. A four-product `/v1/datasets` request also passed. These were local noncommercial smoke tests, within the 100-authorized-request limit per provider. See the [provider coverage matrix](provider-guide.md#local-live-coverage-19-november-2026).
- Ubuntu systemd and Windows Service Control Manager install, start, update, rollback, and removal paths were exercised on local hosts.
- Docker image scanning found no high or critical findings in the scanned release image. A 10,000-request fixture-backed soak completed on a healthy host without request errors.
- A phone on the local network reached the gateway through a temporary local Caddy HTTPS endpoint. This did not test a public-domain certificate or renewal.

Detailed logs and the full repair history are retained by the maintainer outside the public source tree.

## Remaining before a public production launch

1. Verify the actual deployment domain, trusted proxy, certificate issuance and renewal, ingress controls, and recovery procedure.
2. Review each provider account's current plan, permitted uses, attribution, cache/retention terms, and explicit request budget for the deployed workload. Forecast needs a separately configured eligible forecast provider for each deployment.
   Replace the coarse Germany/US country-polygon index used in local regional smoke tests with reviewed production boundaries before enabling those routes on a deployed server.
3. Measure production capacity and recovery on the intended host. The local soak and native service tests are useful evidence, not a production load guarantee.

See [release security baseline](RELEASE_SECURITY_BASELINE.md), [configuration](configuration.md), and [OpenAPI](openapi.yaml).
