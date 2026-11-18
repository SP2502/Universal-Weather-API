# Local provider testing with Docker

Use `deployments/docker/.env` for private credentials and test settings. Git ignores this file. Start with [`.env.example`](../deployments/docker/.env.example); never commit or share filled-in keys. The Compose gateway is published on loopback only. The private test profile uses `APP_ENV=development`, `APP_DEPLOYMENT_USE=noncommercial`, and `APP_RELEASE_PROFILE=full_experimental` to exercise deferred data types locally. Keep production on `lean_v1`.

## What to add

| Data and adapter | Key or contact needed | Local activation |
| --- | --- | --- |
| Current weather: OpenWeather, WeatherAPI | Your existing account keys | `OPENWEATHER_API_KEY`, `WEATHERAPI_API_KEY` |
| Geocoding: LocationIQ | Your existing account key | `LOCATIONIQ_API_KEY` |
| Geocoding: Geoapify | Create a [Geoapify free-plan](https://www.geoapify.com/pricing/) key | `GEOAPIFY_API_KEY=<your key>` and `APP_ENABLE_GEOAPIFY=true` |
| Open-Meteo forecast, forward geocoding, modeled air quality, ERA5 history, marine forecast | No API key for the [noncommercial free API](https://open-meteo.com/en/terms) | Set the matching `APP_ENABLE_OPEN_METEO_*=true` flags and `APP_DEPLOYMENT_USE=noncommercial` |
| Astronomy: Sunrise-Sunset | No key; [visible attribution is required](https://sunrise-sunset.org/api/v1) | `APP_ENABLE_SUNRISE_SUNSET=true` |
| MET Norway forecast | No key; human operator must review [terms](https://api.met.no/doc/TermsOfService) and provide a real contact User-Agent | `APP_ENABLE_MET_NORWAY=true` and `APP_MET_NORWAY_USER_AGENT=<app and contact>` |
| US NWS forecast/alerts | No key; [identifying User-Agent](https://www.weather.gov/documentation/services-web-api) and verified US geographic policy needed | Set NWS flags, `APP_NWS_USER_AGENT`, and reviewed country polygons |
| Bright Sky forecast | No key; Germany coverage and reviewed geographic policy needed | `APP_ENABLE_BRIGHT_SKY=true` plus reviewed country polygons |

The operator policy must also enable each candidate. The default [policy example](../configs/provider-policy.example.json) leaves candidates disabled. Set `UWA_PROVIDER_POLICY_HOST_FILE` to a reviewed local copy and enable only approved rows. Compose mounts it read-only at `/etc/uwa/provider-policy.json`. A credential, flag, or policy row alone is insufficient. Regional NWS/Bright Sky routes also need a reviewed country-polygon index mounted into the container; the example Compose file does not supply one.

Set `APP_PROVIDER_QUOTAS_JSON` to your real account limits and a smaller test budget. Multiple Open-Meteo products must share one quota `group` and identical unit/window settings; history and marine need reviewed weighted costs. The private test profile uses a shared 80 weighted-unit daily ceiling and small per-provider limits. This is a local safety cap, **not** a statement of vendor allowance. Previous user authorization also limits live tests to 100 requests per provider.

## Start and check

From the repository root:

```powershell
docker compose --env-file deployments/docker/.env -f deployments/docker/docker-compose.yml config --quiet
docker compose --env-file deployments/docker/.env -f deployments/docker/docker-compose.yml up -d --build
docker compose --env-file deployments/docker/.env -f deployments/docker/docker-compose.yml exec -T gateway gateway --check-config
```

Set `UWA_GATEWAY_HOST_PORT` in the private `.env` if Windows reserves port 8080. Check `http://127.0.0.1:<host-port>/readiness`. The image includes `uapctl`; an operator can provision a short-lived scoped client key with `docker compose ... exec -T gateway uapctl create-key --name local-test --scopes forecast:read --expires-in 1h`. The command prints the key once. Keep it private and send it in `X-API-Key`, never in a URL.

To check a specific upstream through UWA, add `provider=<provider_id>` to the request, for example `/v1/forecast?lat=51.5074&lon=-0.1278&points=3&provider=open_meteo_forecast`. The [OpenAPI specifications](openapi.experimental.yaml) give parameters for air quality, history, marine, astronomy, and alerts. Astronomy requires an explicit `date=YYYY-MM-DD`; history requires `start_date` and `end_date`.

Only run bounded live calls after reviewing the upstream terms, attribution, and actual account limits. Public Nominatim remains disabled as a general-purpose gateway backend.
