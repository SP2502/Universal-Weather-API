package routing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing/decision"
)

func TestPhase08BLeanRoutesHideDeferredProducts(t *testing.T) {
	reg := provider.NewRegistry()
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	deps := RouterDependencies{
		HealthHandler:     health.NewHandler("test"),
		ReleaseProfile:    "lean_v1",
		AuthMiddleware:    func(next http.Handler) http.Handler { return next },
		ExtendedHandler:   capability.NewExtendedHandler(reg, eng, nil),
		AirQualityHandler: capability.NewAirQualityHandler(reg, eng, nil),
	}
	router := NewRouter(deps)
	for _, path := range []string{"/v1/air-quality", "/v1/history", "/v1/marine", "/v1/alerts", "/metrics"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 404 {
			t.Errorf("lean route %s returned %d, want 404", path, rr.Code)
		}
	}
	req := httptest.NewRequest("GET", "/v1/astronomy", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code == 404 {
		t.Error("optional astronomy route missing")
	}
	deps.ReleaseProfile = "full_experimental"
	experimental := NewRouter(deps)
	for _, path := range []string{"/v1/air-quality", "/v1/history", "/v1/marine", "/v1/alerts"} {
		w := httptest.NewRecorder()
		experimental.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 404 {
			t.Errorf("experimental route %s unexpectedly absent", path)
		}
	}
}
