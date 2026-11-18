package routing

import (
	"net/http"

	"github.com/universal-api/platform/internal/authorization"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/health"
)

type RouterDependencies struct {
	HealthHandler     *health.Handler
	MetricsHandler    http.Handler
	WeatherHandler    *capability.WeatherHandler
	GeocodingHandler  *capability.GeocodingHandler
	ForecastHandler   *capability.ForecastHandler
	AirQualityHandler *capability.AirQualityHandler
	ExtendedHandler   *capability.ExtendedHandler
	AuthMiddleware    func(http.Handler) http.Handler
	ReleaseProfile    string // lean_v1 hides deferred products from the release router
}

func NewRouter(deps RouterDependencies) http.Handler {
	mux := http.NewServeMux()

	// Public Health & Observability Probes
	mux.HandleFunc("GET /health", deps.HealthHandler.Health)
	mux.HandleFunc("GET /readiness", deps.HealthHandler.Readiness)
	mux.HandleFunc("GET /liveness", deps.HealthHandler.Liveness)

	if deps.MetricsHandler != nil {
		mux.Handle("GET /metrics", deps.MetricsHandler)
	}

	// Protected Capability Routes
	weatherHandler := http.HandlerFunc(deps.WeatherHandler.GetWeather)
	protectedWeather := deps.AuthMiddleware(authorization.RequireScope("weather:read")(weatherHandler))
	mux.Handle("GET /v1/weather", protectedWeather)
	datasets := map[string]datasetRoute{"weather": {"weather:read", weatherHandler}}

	if deps.ForecastHandler != nil {
		forecastHandler := http.HandlerFunc(deps.ForecastHandler.GetForecast)
		protectedForecast := deps.AuthMiddleware(authorization.RequireScope("forecast:read")(forecastHandler))
		mux.Handle("GET /v1/forecast", protectedForecast)
		mux.Handle("GET /v1/forecast/daily", deps.AuthMiddleware(authorization.RequireScope("forecast:read")(http.HandlerFunc(deps.ForecastHandler.GetDailyForecast))))
		datasets["forecast"] = datasetRoute{"forecast:read", forecastHandler}
		datasets["forecast-daily"] = datasetRoute{"forecast:read", deps.ForecastHandler.GetDailyForecast}
	}

	if deps.AirQualityHandler != nil && deps.ReleaseProfile != "lean_v1" {
		mux.Handle("GET /v1/air-quality", deps.AuthMiddleware(authorization.RequireScope("air-quality:read")(http.HandlerFunc(deps.AirQualityHandler.GetAirQuality))))
		datasets["air-quality"] = datasetRoute{"air-quality:read", deps.AirQualityHandler.GetAirQuality}
	}
	if deps.ExtendedHandler != nil {
		if deps.ReleaseProfile != "lean_v1" {
			mux.Handle("GET /v1/history", deps.AuthMiddleware(authorization.RequireScope("history:read")(http.HandlerFunc(deps.ExtendedHandler.GetHistory))))
			mux.Handle("GET /v1/marine", deps.AuthMiddleware(authorization.RequireScope("marine:read")(http.HandlerFunc(deps.ExtendedHandler.GetMarine))))
			mux.Handle("GET /v1/alerts", deps.AuthMiddleware(authorization.RequireScope("alerts:read")(http.HandlerFunc(deps.ExtendedHandler.GetAlerts))))
			datasets["history"] = datasetRoute{"history:read", deps.ExtendedHandler.GetHistory}
			datasets["marine"] = datasetRoute{"marine:read", deps.ExtendedHandler.GetMarine}
			datasets["alerts"] = datasetRoute{"alerts:read", deps.ExtendedHandler.GetAlerts}
		}
		mux.Handle("GET /v1/astronomy", deps.AuthMiddleware(authorization.RequireScope("astronomy:read")(http.HandlerFunc(deps.ExtendedHandler.GetAstronomy))))
		datasets["astronomy"] = datasetRoute{"astronomy:read", deps.ExtendedHandler.GetAstronomy}
	}
	if deps.GeocodingHandler != nil {
		geocodeHandler := http.HandlerFunc(deps.GeocodingHandler.Geocode)
		protectedGeocode := deps.AuthMiddleware(authorization.RequireScope("geocode:read")(geocodeHandler))
		mux.Handle("GET /v1/geocode", protectedGeocode)
		datasets["geocode"] = datasetRoute{"geocode:read", geocodeHandler}
	}
	mux.Handle("GET /v1/datasets", deps.AuthMiddleware(datasetsHandler(datasets)))

	return mux
}
