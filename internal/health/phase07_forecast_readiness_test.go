package health

import (
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/openmeteo"
	"testing"
)

func TestForecastOnlyConfiguredReadiness(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(openmeteo.NewAdapter(""))
	checker := NewConfiguredProviderHealthChecker(reg).WithRequiredCapabilities("forecast").WithProductEligibility(func(id, capability string) bool {
		return id == "open_meteo_forecast" && capability == "forecast"
	})
	status, details, ok := checker.Check()
	if !ok || status != "UP" || details["forecast_providers"] != "CONFIGURED" {
		t.Fatalf("forecast-only should be ready: %s %+v", status, details)
	}
	checker.WithRequiredCapabilities("weather", "forecast")
	if _, _, ok := checker.Check(); ok {
		t.Fatal("weather,forecast must require BOTH")
	}
	checker.WithRequiredCapabilities("forecast").WithProductEligibility(func(string, string) bool { return false })
	if _, _, ok := checker.Check(); ok {
		t.Fatal("policy disabled forecast should not be ready")
	}
}
