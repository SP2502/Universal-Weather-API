package conformance

import (
	"testing"

	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

func TestConformance_OpenWeather(t *testing.T) {
	adapter := openweather.NewAdapter("test-key", "https://api.openweathermap.org")
	suite := NewConformanceSuite(adapter)
	suite.RunAll(t)

	VerifyWeatherProviderErrorResponses(t, func(baseURL string) provider.WeatherProvider {
		return openweather.NewAdapter("test-key", baseURL)
	})
}

func TestConformance_WeatherAPI(t *testing.T) {
	adapter := weatherapi.NewAdapter("test-key", "https://api.weatherapi.com")
	suite := NewConformanceSuite(adapter)
	suite.RunAll(t)

	VerifyWeatherProviderErrorResponses(t, func(baseURL string) provider.WeatherProvider {
		return weatherapi.NewAdapter("test-key", baseURL)
	})
}

func TestConformance_Nominatim(t *testing.T) {
	adapter := nominatim.NewAdapter("https://nominatim.openstreetmap.org")
	suite := NewConformanceSuite(adapter)
	suite.RunAll(t)

	VerifyGeocodingProviderErrorResponses(t, func(baseURL string) provider.GeocodingProvider {
		return nominatim.NewAdapter(baseURL)
	})
}

func TestConformance_LocationIQ(t *testing.T) {
	adapter := locationiq.NewAdapter("test-key", "https://us1.locationiq.com")
	suite := NewConformanceSuite(adapter)
	suite.RunAll(t)

	VerifyGeocodingProviderErrorResponses(t, func(baseURL string) provider.GeocodingProvider {
		return locationiq.NewAdapter("test-key", baseURL)
	})
}
