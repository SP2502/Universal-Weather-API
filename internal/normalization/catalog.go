package normalization

// ProviderSchemaContract documents the canonical output contract for each
// integrated adapter. It is intentionally static and has no runtime network,
// model, or dynamic field-inference dependency. Provider availability is
// decided separately by deployment policy, credentials, licensing and health.
type ProviderSchemaContract struct {
	ProviderID    string
	Product       string
	SchemaVersion string
	ReferenceURL  string
}

// ProviderSchemaCatalog is the audit inventory of the integrated adapters.
// The catalog does not imply that an upstream has been tested live or that it
// is enabled in a release profile. Caller receives a copy, not mutable state.
func ProviderSchemaCatalog() []ProviderSchemaContract {
	return []ProviderSchemaContract{
		{"openweather", "weather", "weather.v1", "https://openweathermap.org/api/current"},
		{"weatherapi", "weather", "weather.v1", "https://www.weatherapi.com/docs/realtime-api/"},
		{"met_norway", "forecast", "forecast.v1", "https://api.met.no/weatherapi/locationforecast/2.0/documentation"},
		{"open_meteo_forecast", "forecast", "forecast.v1", "https://open-meteo.com/en/docs"},
		{"us_nws", "forecast", "forecast.v1", "https://www.weather.gov/documentation/services-web-api"},
		{"bright_sky", "forecast", "forecast.v1", "https://brightsky.dev/docs/"},
		{"nominatim", "geocoding", "location.v1", "https://nominatim.org/release-docs/latest/api/Search/"},
		{"locationiq", "geocoding", "location.v1", "https://docs.locationiq.com/"},
		{"geoapify", "geocoding", "location.v1", "https://apidocs.geoapify.com/docs/geocoding/forward-geocoding/"},
		{"open_meteo_geocoding", "geocoding", "location.v1", "https://open-meteo.com/en/docs/geocoding-api"},
		{"open_meteo_air_quality", "air_quality", "air_quality.v1", "https://open-meteo.com/en/docs/air-quality-api"},
		{"open_meteo_history", "history", "history.v1", "https://open-meteo.com/en/docs/historical-weather-api"},
		{"sunrise_sunset", "astronomy", "astronomy.v1", "https://sunrise-sunset.org/api"},
		{"open_meteo_marine", "marine", "marine.v1", "https://open-meteo.com/en/docs/marine-weather-api"},
		{"us_nws_alerts", "alerts", "alerts.v1", "https://www.weather.gov/documentation/services-web-api"},
	}
}
