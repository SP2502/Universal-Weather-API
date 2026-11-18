# Configuration

UWA reads environment variables or a `gateway --env-file` file. Process environment values take precedence. Use `gateway --check-config` for offline validation; it does not connect to providers or databases. Keep credential files out of Git.

## Core settings

| Variable | Local default | Use |
| --- | --- | --- |
| `APP_ENV` | `development` | Runtime profile: development, test, staging, or production. |
| `APP_RELEASE_PROFILE` | `lean_v1` | Public route set. `full_experimental` is limited to development/test. |
| `APP_BIND_HOST` / `APP_PORT` | `127.0.0.1` / `8080` | Gateway HTTP listener. Put HTTPS at a trusted reverse proxy. |
| `APP_DB_DRIVER` | `memory` | Staging/production require `postgres` and `APP_DATABASE_URL`. |
| `APP_CACHE_DRIVER` | `memory` | Staging/production require `redis` and Redis connection settings. |
| `APP_DEV_CLIENT_KEY` | unset | Unique 24–512 character client key for loopback development with memory persistence only. No key is seeded by default. |
| `APP_REQUIRED_CAPABILITIES` | `weather` | Required capabilities checked at startup/readiness; a listed capability still needs an eligible provider. |
| `APP_WEATHER_PLACE_RESOLVER` | `disabled` | `strict` uses approved geocoding for place-name weather/forecast requests and consumes its budget. |
| `APP_METRICS_MODE` | `off` | `loopback` needs loopback binding and `APP_METRICS_TOKEN` of at least 32 characters. |
| `APP_TRUSTED_PROXY_CIDRS` | unset | Explicit addresses of proxies whose forwarded client IP headers are trusted. |

## Upstream providers

Set only the credentials for providers you intend to use: `OPENWEATHER_API_KEY`, `WEATHERAPI_API_KEY`, `LOCATIONIQ_API_KEY`, or keys required by other enabled adapters. Review `configs/provider-policy.example.json` and set `APP_PROVIDER_POLICY_FILE` to your approved policy. The example is a starting point, not a grant of provider data rights. `APP_DEPLOYMENT_USE` records the deployment use; optional hosted Open-Meteo services require the appropriate noncommercial configuration. `APP_COUNTRY_POLYGONS_FILE` can supply a reviewed geographic index.

`APP_PROVIDER_QUOTAS_JSON` defines explicit provider budgets. It is required for every configured provider in staging/production. Entries can specify `per_second`, `per_minute`, and `per_day`, with request, credit, record, or weighted-call units, product costs, and shared `group` names. Count retries and fallback attempts. Use the limits and units of your actual provider account; example numbers are not vendor allowances. When Open-Meteo forecast and geocoding are both enabled, give them the same shared group and identical allowance windows.

## Deployment rules

Staging and production require PostgreSQL-backed client keys, Redis-backed shared state, eligible provider policy, explicit budgets, and HTTPS ingress. Provision scoped client keys with `uapctl create-key`; do not use the development key there. Protect credentials and database/Redis access. See the [provider guide](provider-guide.md), [authentication](authentication.md), [server operation](CROSS_PLATFORM_SERVERS.md), and [release security baseline](RELEASE_SECURITY_BASELINE.md).
