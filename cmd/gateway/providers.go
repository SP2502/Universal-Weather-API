package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/secrets"
	"github.com/universal-api/platform/providers/brightsky"
	"github.com/universal-api/platform/providers/geoapify"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/metnorway"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/nwsalerts"
	"github.com/universal-api/platform/providers/openmeteo"
	"github.com/universal-api/platform/providers/openmeteoairquality"
	"github.com/universal-api/platform/providers/openmeteogeocode"
	"github.com/universal-api/platform/providers/openmeteohistory"
	"github.com/universal-api/platform/providers/openmeteomarine"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/sunrisesunset"
	"github.com/universal-api/platform/providers/usnws"
	"github.com/universal-api/platform/providers/weatherapi"
)

// registerConfiguredProviders reports only safe metadata about credentials.
// The returned weather count is a startup prerequisite in production/staging.
func registerConfiguredProviders(secretStore secrets.SecretStore, logger *slog.Logger) (*provider.Registry, int) {
	registry := provider.NewRegistry()
	configured := 0
	resolve := func(name string) (string, bool) {
		key, err := secretStore.GetSecret(context.Background(), name)
		if err != nil || strings.TrimSpace(key) == "" {
			return "", false
		}
		return key, true
	}
	if key, ok := resolve("OPENWEATHER_API_KEY"); ok {
		registry.Register(openweather.NewAdapter(key, ""))
		configured++
		logger.Info("Configured upstream provider", "provider", "openweather")
	} else {
		logger.Warn("Upstream provider credential not configured", "provider", "openweather", "secret_name", "OPENWEATHER_API_KEY")
	}
	if key, ok := resolve("WEATHERAPI_API_KEY"); ok {
		registry.Register(weatherapi.NewAdapter(key, ""))
		configured++
		logger.Info("Configured upstream provider", "provider", "weatherapi")
	} else {
		logger.Warn("Upstream provider credential not configured", "provider", "weatherapi", "secret_name", "WEATHERAPI_API_KEY")
	}
	// Never use public Nominatim as an unpaced production default. Keep
	// explicit test/dev registration only; not eligible for lean_v1.
	if os.Getenv("APP_ENABLE_PUBLIC_NOMINATIM") == "true" && os.Getenv("APP_RELEASE_PROFILE") == "full_experimental" && (os.Getenv("APP_ENV") == "development" || os.Getenv("APP_ENV") == "test") {
		registry.Register(nominatim.NewAdapter(""))
		logger.Warn("Public Nominatim registered for isolated development only; respect external service policy")
	}
	if key, ok := resolve("LOCATIONIQ_API_KEY"); ok {
		registry.Register(locationiq.NewAdapter(key, ""))
		logger.Info("Configured upstream provider", "provider", "locationiq")
	} else {
		logger.Warn("Upstream provider credential not configured", "provider", "locationiq", "secret_name", "LOCATIONIQ_API_KEY")
	}
	// Phase 06 candidates require an explicit opt-in AND a matching enabled
	// operator policy. A research catalog entry alone never activates traffic.
	if os.Getenv("APP_ENABLE_GEOAPIFY") == "true" {
		if key, ok := resolve("GEOAPIFY_API_KEY"); ok {
			registry.Register(geoapify.NewAdapter(key, ""))
			logger.Info("Configured Geoapify geocoding candidate")
		} else {
			logger.Warn("Geoapify enabled but GEOAPIFY_API_KEY not configured")
		}
	}
	if os.Getenv("APP_ENABLE_MET_NORWAY") == "true" {
		agent := strings.TrimSpace(os.Getenv("APP_MET_NORWAY_USER_AGENT"))
		if strings.Contains(agent, "@") || strings.Contains(agent, "https://") {
			registry.Register(metnorway.NewAdapter(agent, ""))
			logger.Info("Configured MET Norway forecast candidate")
		} else {
			logger.Warn("MET Norway enabled but identifying APP_MET_NORWAY_USER_AGENT missing")
		}
	}
	// Hosted Open-Meteo's free tier does not permit commercial deployments.
	// Explicit noncommercial declaration is mandatory even if a custom policy
	// incorrectly enables the provider for another deployment.
	if os.Getenv("APP_DEPLOYMENT_USE") == "noncommercial" {
		if os.Getenv("APP_ENABLE_OPEN_METEO_FORECAST") == "true" {
			registry.Register(openmeteo.NewAdapter(""))
			logger.Info("Configured non-commercial Open-Meteo forecast candidate")
		}
		if os.Getenv("APP_ENABLE_OPEN_METEO_AIR_QUALITY") == "true" {
			registry.Register(openmeteoairquality.NewAdapter(""))
			logger.Info("Configured non-commercial Open-Meteo CAMS air-quality candidate")
		}
		if os.Getenv("APP_ENABLE_OPEN_METEO_GEOCODING") == "true" {
			registry.Register(openmeteogeocode.NewAdapter(""))
			logger.Info("Configured non-commercial Open-Meteo geocoding candidate")
		}
	} else if os.Getenv("APP_ENABLE_OPEN_METEO_FORECAST") == "true" || os.Getenv("APP_ENABLE_OPEN_METEO_GEOCODING") == "true" || os.Getenv("APP_ENABLE_OPEN_METEO_AIR_QUALITY") == "true" {
		logger.Warn("Hosted Open-Meteo candidates excluded: APP_DEPLOYMENT_USE must be noncommercial")
	}
	if os.Getenv("APP_DEPLOYMENT_USE") == "noncommercial" {
		if os.Getenv("APP_ENABLE_OPEN_METEO_HISTORY") == "true" {
			registry.Register(openmeteohistory.NewAdapter(""))
			logger.Info("Configured non-commercial ERA5 history candidate")
		}
		if os.Getenv("APP_ENABLE_OPEN_METEO_MARINE") == "true" {
			registry.Register(openmeteomarine.NewAdapter(""))
			logger.Info("Configured non-commercial marine forecast candidate")
		}
	}
	if os.Getenv("APP_ENABLE_SUNRISE_SUNSET") == "true" {
		registry.Register(sunrisesunset.NewAdapter(""))
		logger.Info("Configured astronomy calculation candidate")
	}
	if os.Getenv("APP_ENABLE_US_NWS_ALERTS") == "true" {
		registry.Register(nwsalerts.NewAdapter(strings.TrimSpace(os.Getenv("APP_NWS_USER_AGENT")), ""))
		logger.Info("Configured US NWS alerts candidate")
	}
	// Regional forecast providers are independently opted into and still require
	// an operator-reviewed geographic policy before any upstream call is possible.
	if os.Getenv("APP_ENABLE_US_NWS") == "true" {
		registry.Register(usnws.NewAdapter(strings.TrimSpace(os.Getenv("APP_NWS_USER_AGENT")), ""))
		logger.Info("Configured US NWS regional forecast candidate")
	}
	if os.Getenv("APP_ENABLE_BRIGHT_SKY") == "true" {
		registry.Register(brightsky.NewAdapter("", 25000))
		logger.Info("Configured Bright Sky DWD regional forecast candidate")
	}
	return registry, configured
}
