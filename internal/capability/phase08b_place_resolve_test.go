package capability

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/georesolve"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing/decision"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

type phase08BResolver struct {
	err   error
	calls int
}

func (r *phase08BResolver) Resolve(_ context.Context, place string) (*locv1.Location, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &locv1.Location{Latitude: 21.25, Longitude: 81.63, CountryCode: "IN"}, nil
}
func TestPhase08BStrictPlaceResolutionUsedByForecastAndDaily(t *testing.T) {
	registry := provider.NewRegistry()
	eng := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	h := NewForecastHandler(registry, eng, nil)
	res := &phase08BResolver{}
	h.SetPlaceResolver(res)
	for _, path := range []string{"/v1/forecast?q=Raipur&points=5", "/v1/forecast/daily?location=Raipur&days=2"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		if path[:18] == "/v1/forecast/daily" {
			h.GetDailyForecast(w, req)
		} else {
			h.GetForecast(w, req)
		}
		if w.Code != 503 {
			t.Fatalf("%s: want provider unavailable (503) AFTER resolution, got %d: %s", path, w.Code, w.Body.String())
		}
	}
	if res.calls != 2 {
		t.Fatalf("want resolver calls, got %d", res.calls)
	}
	res.err = georesolve.ErrAmbiguousLocation
	w := httptest.NewRecorder()
	h.GetForecast(w, httptest.NewRequest("GET", "/v1/forecast?q=Paris", nil))
	if w.Code != 422 {
		t.Fatalf("ambiguous forecast should be 422, got %d", w.Code)
	}
}
