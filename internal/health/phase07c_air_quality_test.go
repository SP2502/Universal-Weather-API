package health

import (
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/openmeteoairquality"
	"testing"
)

func TestPhase07C_AirQualityOnlyReadiness(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(openmeteoairquality.NewAdapter(""))
	h := NewConfiguredProviderHealthChecker(reg).WithRequiredCapabilities("air_quality").WithProductEligibility(func(id, cap string) bool { return id == "open_meteo_air_quality" && cap == "air_quality" })
	if state, _, ok := h.Check(); !ok || state != "UP" {
		t.Fatalf("expected air-quality readiness, got %s", state)
	}
	h.WithProductEligibility(func(string, string) bool { return false })
	if _, _, ok := h.Check(); ok {
		t.Fatal("policy-disabled model provider reported ready")
	}
}
