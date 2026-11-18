package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openmeteo"
)

func TestForecastLongerPointWindowAndBadRequests(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		times := make([]int64, 0, 72)
		temps := make([]float64, 0, 72)
		humidity := make([]float64, 0, 72)
		codes := make([]int, 0, 72)
		base := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Hour)
		for i := 0; i < 72; i++ {
			times = append(times, base.Add(time.Duration(i)*time.Hour).Unix())
			temps = append(temps, 20)
			humidity = append(humidity, 50)
			codes = append(codes, 1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"latitude": 21.25, "longitude": 81.63, "hourly": map[string]any{"time": times, "temperature_2m": temps, "relative_humidity_2m": humidity, "weather_code": codes}})
	}))
	defer upstream.Close()
	reg := provider.NewRegistry()
	reg.Register(openmeteo.NewAdapter(upstream.URL).WithHTTPClient(upstream.Client()))
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "open_meteo_forecast", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"hourly_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	handler := capability.NewForecastHandler(reg, engine, nil)
	for _, tc := range []struct {
		param          string
		status, points int
	}{{"", 200, 24}, {"&points=48", 200, 48}, {"&points=168", 200, 72}, {"&points=0", 400, 0}, {"&points=169", 400, 0}, {"&points=abc", 400, 0}, {"&points=3&points=4", 400, 0}} {
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/forecast?lat=21.25&lon=81.63%s", tc.param), nil)
		rec := httptest.NewRecorder()
		before := calls.Load()
		handler.GetForecast(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s status=%d body=%s", tc.param, rec.Code, rec.Body.String())
		}
		if tc.status == 400 {
			if calls.Load() != before {
				t.Fatal("invalid request called upstream")
			}
			continue
		}
		var envelope struct {
			Data struct {
				Points []json.RawMessage `json:"points"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if got := len(envelope.Data.Points); got != tc.points {
			t.Fatalf("%s points=%d want=%d body=%s", tc.param, got, tc.points, strings.TrimSpace(rec.Body.String()))
		}
	}
}
