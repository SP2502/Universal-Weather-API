# UWA API Reference — Lean v1 Release Candidate

**Status:** Locally validated source candidate; [release status](release-status.md) lists the exercised tests and remaining public-deployment gates. The presence of a route does **not** mean that an upstream provider is automatically enabled.

The authoritative machine-readable **lean-v1** contract is [openapi.yaml](openapi.yaml). The separate [experimental API specification](openapi.experimental.yaml) describes deferred routes available only through `APP_RELEASE_PROFILE=full_experimental` in development/test. Deferred products are not part of the initial release.

UWA is a deterministic, self-hosted API gateway. Coordinates are accepted directly; place-name weather/forecast resolution needs `APP_WEATHER_PLACE_RESOLVER=strict` and a permitted, configured geocoding provider. A provider's geographic, product, licensing, quota and credential restrictions are enforced regardless of route availability.

## Authentication

Protected routes require a provisioned gateway client key with the appropriate `*:read` scope. Use `X-API-Key: <client-key>` or the supported bearer authorization. Client keys are **not** upstream provider keys. Do not put keys in URLs or published demos.

| Method | Route | Required scope | Availability |
|---|---|---|---|
| GET | `/v1/weather` | `weather:read` | Lean v1; requires eligible current-weather provider |
| GET | `/v1/datasets` | Scope for each requested dataset | Lean v1; combines independent product results |
| GET | `/v1/forecast` | `forecast:read` | Lean v1; requires eligible forecast provider |
| GET | `/v1/forecast/daily` | `forecast:read` | Lean v1; summaries derived from available samples |
| GET | `/v1/geocode` | `geocode:read` | Lean v1; depends on an eligible geocoder |
| GET | `/v1/astronomy` | `astronomy:read` | Lean v1; optional, requires approved provider |
| GET | `/health` | None | Process health; not upstream acceptance proof |
| GET | `/readiness` | None | Configured dependencies/capabilities |
| GET | `/liveness` | None | Process liveness |

`/metrics` is **not public by default**. It is disabled unless explicitly enabled with `APP_METRICS_MODE=loopback`, bound to loopback, and protected using a bearer token (`APP_METRICS_TOKEN`). The metrics endpoint is documented in the experimental/operational specification, not the public lean-v1 contract.

## Current weather

```http
GET /v1/weather?lat=21.25&lon=81.63
X-API-Key: <provisioned-client-key>
```

**Illustrative schema example — not live weather:**

```json
{
  "data": {
    "schema_version": "weather.v1",
    "location": {"latitude": 21.25, "longitude": 81.63},
    "temperature": {"value": 25.5, "unit": "celsius"},
    "humidity": {"value": 65, "unit": "percent"},
    "condition": "partly_cloudy",
    "observed_at": "2026-01-01T12:00:00Z"
  },
  "meta": {
    "request_id": "illustrative-request-id",
    "timestamp": "2026-01-01T12:00:01Z",
    "provider": "openweather",
    "cached": false,
    "fallback": false
  }
}
```

Required fields and optional values are defined in [OpenAPI](openapi.yaml) and the Go schema `schemas/weather/v1/weather.go`. Humidity can be absent; absence does not mean 0%. `observed_at` is the observation time supplied or justified by the data source, not the HTTP request time. The example metadata is illustrative, not evidence of a completed upstream request.

## Forecasts

```http
GET /v1/forecast?lat=21.25&lon=81.63&points=48
X-API-Key: <provisioned-client-key>
```

The default `points` count is 24; accepted bounds are 1–168. Points are **available forecast samples**, not guaranteed consecutive clock-hours. Each point includes `valid_at`, temperature, and normalized condition; the response uses `forecast.v1` and a model-forecast data kind. This is distinct from the current-weather observation endpoint.

```http
GET /v1/forecast/daily?lat=21.25&lon=81.63
X-API-Key: <provisioned-client-key>
```

Daily summaries aggregate available forecast samples by UTC date; reported minimum/maximum are **sampled values**, not certified daily extremes.

## Geocoding

```http
GET /v1/geocode?q=Raipur%2C%20Chhattisgarh%2C%20India
X-API-Key: <provisioned-client-key>
```

For reverse geocoding, supply supported `lat` and `lon` parameters. Returned results use `location.v1` with validated coordinates. Geographic confidence is only present if justified by provider data; ambiguous place names must not be silently treated as a verified match.

## Astronomy

```http
GET /v1/astronomy?lat=21.25&lon=81.63
X-API-Key: <provisioned-client-key>
```

This is an optional calculated-data product. Specific events may be null or unavailable, including for polar locations, and should not be fabricated.

## Errors, attribution, and deployment

Protected endpoints can return authentication/authorization failures, invalid requests, quota rejections, unavailable providers, invalid upstream data, and timeouts. Consult the [OpenAPI specification](openapi.yaml) for each documented error response. Successful replies preserve provenance, provider attribution and fallback/cache metadata as applicable.

For production, configure genuine PostgreSQL, shared Redis, explicit provider budgets and operator-approved policy/credentials. Use `gateway --check-config` for **offline** validation. Use `uapctl doctor --json` for offline diagnostics; neither command contacts live upstream services or certifies the installation.

Additional deployment information: [Quickstart](quickstart.md) · [Authentication](authentication.md) · [Configuration](configuration.md).
