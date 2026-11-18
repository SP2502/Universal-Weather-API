# UWA Provider Schema Conformance — Launch-Focused Mapping

This document describes the **source-implemented** provider mapping contracts. It is not an upstream account, license, geographic coverage, or live compatibility certificate.

UWA does not dynamically infer JSON shape, guess units, or call an LLM. Different upstream product categories use different canonical schemas. Output metadata must preserve the **selected provider** and source timestamps.

| Product | Upstream adapters | Canonical schema | Normalizer |
| --- | --- | --- | --- |
| Current weather | OpenWeather; WeatherAPI.com | `weather.v1` | `internal/normalization/weather.go` |
| Forecast | MET Norway; Open-Meteo; US NWS; Bright Sky | `forecast.v1` | `forecast.go`, `forecast_regional.go` |
| Geocoding | Nominatim; LocationIQ; Geoapify; Open-Meteo Geocoding | `location.v1` | `geocoding.go`, `geocoding_new.go` |
| Air quality | Open-Meteo Air Quality | `air_quality.v1` | `air_quality.go` |
| History | Open-Meteo ERA5 | `history.v1` | `extended.go` |
| Astronomy | Sunrise-Sunset.org | `astronomy.v1` | `extended.go` |
| Marine | Open-Meteo Marine | `marine.v1` | `extended.go` |
| Alerts | US NWS | `alerts.v1` | `extended.go` |

The machine-readable audit inventory is `internal/normalization/catalog.go`. Tests verify uniqueness, schema families, coverage counts, and key drift cases.

## Mapping rules that cannot be relaxed

1. **Observation vs prediction:** `weather.v1` uses `observed_at`; `forecast.v1` uses `valid_at`. Never represent model output as a measured current observation.
2. **Temperature:** OpenWeather current API default Kelvin -> Celsius; WeatherAPI `temp_c` already Celsius; NWS Fahrenheit -> Celsius when its unit says `F`; Open-Meteo and MET Norway forecast values are Celsius only under their documented units. Never silently assume a changed upstream unit.
3. **Missing values:** Required temperature/coordinates/timestamp missing => invalid provider attempt; fallback if a second provider is eligible. Optional humidity or geocoding subfields remain absent rather than zero. A genuine 0°C or 0% humidity is preserved.
4. **Condition vocabulary:** UWA's compact enum cannot encode every hazard. Mixed sleet, ice pellets, smoke and tornado should not be guessed as a harmless canonical condition; preserve available source codes and terminology. Published WeatherAPI codes take priority over localized text.
5. **Geocoding ranking:** Nominatim/LocationIQ `importance` is not the probability of correct coordinates and is not placed in canonical `confidence`. Geoapify's `rank.confidence` is provider-defined, not a cross-provider accuracy guarantee.
6. **Forecast time bases:** Open-Meteo's adapter requests UTC Unix timestamps and Celsius, so a contradictory `utc_offset_seconds` or declared hourly unit fails normalization. Null or malformed required values cannot become plausible fake readings.
7. **Security:** Never forward credential-bearing upstream URLs (including URLs from Go `url.Error` wrappers) to responses, logs or error metadata.
8. **Schema evolution:** New optional fields may be added only after updating API specifications, SDKs, fixture tests and source-level validators; incompatible shapes require a version change.

## Coverage and remaining release gates

- Deterministic conformance tests and representative locally mocked upstream fixtures are executable without provider API keys. Run `go test ./internal/normalization ./tests/integration` using the **genuine declared dependencies** on a network-enabled host.
- Full live certification requires licensed credentials, official provider responses for each enabled product/region, real PostgreSQL and Redis, and native-service acceptance.
- Deferred products may be source-implemented without being exposed in the lean v1 public router. The static catalog does not enable providers or widen license permissions.
- Upstream schema changes should be reviewed by comparing an approved redacted fixture (no credentials, precise personal coordinates or personal addresses), declared units, nullability, time base, source provenance and product classification with these rules. Fail closed if incompatible.

## Official references

- OpenWeather current API: https://openweathermap.org/api/current
- WeatherAPI realtime: https://www.weatherapi.com/docs/realtime-api/
- WeatherAPI conditions: https://www.weatherapi.com/docs/weather-icons/
- MET Norway Locationforecast: https://api.met.no/weatherapi/locationforecast/2.0/documentation
- Open-Meteo forecast: https://open-meteo.com/en/docs
- Open-Meteo air quality: https://open-meteo.com/en/docs/air-quality-api
- NWS API: https://www.weather.gov/documentation/services-web-api
