# Provider guide

UWA uses **configured, eligible** upstream sources. A client request does not automatically discover providers or gain permission to use an upstream account. The operator must review product support, region, usage rights, attribution, credentials, and allowance.

| Provider adapter | Product in lean v1 | Credential or condition |
| --- | --- | --- |
| OpenWeather | Current weather | `OPENWEATHER_API_KEY` |
| WeatherAPI | Current weather | `WEATHERAPI_API_KEY` |
| LocationIQ | Forward and reverse geocoding | `LOCATIONIQ_API_KEY` |
| MET Norway, Open-Meteo Forecast, US NWS, Bright Sky | Forecast, subject to coverage and policy | Separate forecast configuration and eligibility |
| Geoapify | Optional geocoding | Explicit policy and credential |
| Public Nominatim | Experimental development/test only | Disabled by default; not a general-purpose hosted gateway backend |

Other adapters and deferred products may appear in the source or [experimental specification](openapi.experimental.yaml). They are not promises of lean v1 availability.

## Local live coverage (19 November 2026)

The following requests returned HTTP 200 through the gateway in a bounded, noncommercial Windows development run. A successful sample proves the route and adapter worked for that test location and time; it does not prove continuous upstream availability or a production entitlement.

| Upstream | Data delivered in the local test | Credential or restriction |
| --- | --- | --- |
| OpenWeather | Current weather | Operator API key |
| WeatherAPI | Current weather | Operator API key |
| LocationIQ | Forward and reverse geocoding | Operator API key |
| Geoapify | Forward and reverse geocoding | Operator API key and opt-in |
| Open-Meteo | Forward geocoding, forecast, derived daily forecast, modeled air quality, historical reanalysis, marine forecast | Hosted free API is noncommercial; experimental profile for air quality, history, and marine |
| MET Norway | Forecast | Identifying contact User-Agent; operator accepted its terms |
| Bright Sky | Forecast near Berlin | Germany geographic policy and nearby source; DWD data terms and attribution |
| US National Weather Service | Forecast and active-alert response near Denver | US geographic policy and identifying contact User-Agent; alert response may contain zero active alerts |
| Sunrise-Sunset | Astronomy | Attribution and explicit date |

The combined `/v1/datasets` route also returned weather, forecast, air quality, and astronomy in one HTTP 200 response. Public Nominatim was not live-tested; it remains disabled for the general gateway. The regional tests used a coarse, local-only Germany/US polygon index. Before regional providers are deployed, replace it with an operator-reviewed boundary file and verify coastal and border cases. The Docker example does not mount that file. Private credentials and the local test index are not committed.

## Configure a provider

1. Check your account's current plan and applicable data terms: deployment use, geographic coverage, required attribution, caching, retention, and request units.
2. Set the credential in a protected environment file or secret store. Never put it in source, logs, URLs, or client applications.
3. Review a copy of [the example policy](../configs/provider-policy.example.json), then set `APP_PROVIDER_POLICY_FILE`. Its legacy/example entries are not legal certification.
4. Set `APP_PROVIDER_QUOTAS_JSON` to the actual account limits. Staging/production refuse configured providers without explicit budgets. Shared upstream allowances need a shared quota group; retries and fallback attempts also consume allowance.
5. Run `gateway --check-config`, then use a bounded live smoke test with your approved account. Check attribution and `meta.provider` in the response.

## Selection and fallback

The gateway filters providers by product, coverage, policy, credentials, quota, and health, then orders them by deterministic priority. It bounds HTTP attempts and response validation. A failed source can fall back to another eligible source within the remaining time and budget. No source, no entitlement, exhausted quota, or invalid data can yield a clear error instead of a weather result. Ordering is not a claim of meteorological accuracy.

Responses include provider and fallback metadata; source attribution must be displayed by client products as required by the upstream terms. See [configuration](configuration.md), [API reference](api-reference.md), and [release status](release-status.md).

For Docker environment variables and bounded local smoke tests, see [provider testing](provider-testing.md).
