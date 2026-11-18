# Universal Weather API

**A self-hosted Go API for weather and location data.** Connect your own upstream provider accounts, then give your clients one consistent, versioned API. UWA selects eligible providers using explicit rules, validates their responses, and can fall back to another configured source. It uses no LLM or hosted UWA account.

> **Release status:** The source candidate passed local tests with real PostgreSQL, Redis, selected live providers, native Windows and Ubuntu services, Docker, and local HTTPS. Public deployment checks remain. [See exactly what passed and what is left](docs/release-status.md).

## What it serves

| Endpoint | Data | Required client scope |
| --- | --- | --- |
| `GET /v1/weather` | Current weather observation | `weather:read` |
| `GET /v1/forecast` | Available forecast samples | `forecast:read` |
| `GET /v1/forecast/daily` | Daily summaries of available samples | `forecast:read` |
| `GET /v1/geocode` | Forward or reverse geocoding | `geocode:read` |
| `GET /v1/astronomy` | Optional astronomy data | `astronomy:read` |
| `GET /v1/datasets` | Several datasets in one request | Scope for each requested dataset |

`/health`, `/liveness`, and `/readiness` are public probes. Metrics require an explicit loopback-only setting and bearer token. The [lean v1 OpenAPI specification](docs/openapi.yaml) defines the public contract. Air quality, history, marine, alerts, and other experimental routes are outside the lean v1 release profile.

**Availability depends on your configuration.** OpenWeather and WeatherAPI adapters provide current weather; LocationIQ provides geocoding. Forecasts need a separately enabled and eligible forecast provider. An API key by itself does not enable a provider: its policy, region, credentials, budget, and permitted use must also be configured. UWA does not promise that its highest-ranked source is meteorologically the most accurate.

## Try it locally

Use Go **1.27.4 or a newer supported security-patched release**. The Go 1.22 line in `go.mod` is the language compatibility floor. These examples use loopback development with in-memory stores; they do not set up a production server.

1. Create a unique development client key in your current shell. Set an upstream key you are entitled to use and review the corresponding provider policy. Never commit either key.

   **PowerShell**

   ```powershell
   $env:APP_DEV_CLIENT_KEY = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
   $env:OPENWEATHER_API_KEY = '<your OpenWeather key>'
   go run ./cmd/gateway --check-config
   go run ./cmd/gateway
   ```

   **Bash / Ubuntu**

   ```bash
   export APP_DEV_CLIENT_KEY="$(openssl rand -hex 32)"
   export OPENWEATHER_API_KEY='<your OpenWeather key>'
   go run ./cmd/gateway --check-config
   go run ./cmd/gateway
   ```

2. In another terminal, check `http://127.0.0.1:8080/liveness`. For a weather request, send `X-API-Key` with the **same** development client key from step 1:

   ```text
   GET http://127.0.0.1:8080/v1/weather?lat=51.5074&lon=-0.1278
   X-API-Key: <your development client key>
   ```

The development key is accepted only with development mode, loopback binding, and memory persistence. No client key is created by default. See the [full quickstart](docs/quickstart.md) for shell-specific requests and the [provider guide](docs/provider-guide.md) for policy and budgets.

## How it works

1. The gateway checks the client key, scope, and request. Coordinates are accepted directly; strict place-name resolution is an optional setting.
2. It filters configured sources by product, coverage, policy, credentials, quota, and operational health.
3. It makes bounded upstream attempts, rejects invalid or stale data, and tries another eligible source when the failure allows it.
4. It returns canonical JSON with a schema version plus provider, attribution, cache, and fallback metadata where applicable.

Client applications must display required provider attribution. Provider data rights and billing terms are separate from this repository's MIT license.

## Run and deploy

Local development defaults to `127.0.0.1:8080` with memory database and cache. Staging and production require PostgreSQL, Redis for shared admission and quotas, provisioned scoped client keys, explicit provider budgets, reviewed provider policy, protected secrets, and HTTPS at a reverse proxy. Run `gateway --check-config` before startup; it validates configuration offline and is not a live provider check.

- [Configuration](docs/configuration.md) · [Authentication and key provisioning](docs/authentication.md) · [API reference](docs/api-reference.md)
- [Windows, Ubuntu, and Docker operation](docs/CROSS_PLATFORM_SERVERS.md) · [Architecture](ARCHITECTURE.md)
- [Release status](docs/release-status.md) · [Security policy](SECURITY.md) · [Release security baseline](docs/RELEASE_SECURITY_BASELINE.md)
- [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md) · [Third-party notices](THIRD_PARTY_NOTICES.md)

## License

The source code is [MIT licensed](LICENSE). The maintainer has confirmed ownership and permission to publish it. Upstream weather/location data and third-party dependencies have their own terms.
