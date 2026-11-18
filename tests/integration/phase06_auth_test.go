package integration_test

import (
	"context"
	"fmt"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openmeteo"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPhase06ForecastRouteRequiresForecastScope(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, openMeteoJSON("21.5")) }))
	defer up.Close()
	reg := provider.NewRegistry()
	reg.Register(openmeteo.NewAdapter(up.URL).WithHTTPClient(up.Client()))
	cat, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "open_meteo_forecast", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"hourly_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(cat, "noncommercial")
	store := database.NewMemoryStore()
	defer store.Close()
	for _, row := range []struct{ value, scope string }{{"forecast-key", "forecast:read"}, {"weather-key", "weather:read"}} {
		err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: row.value, Name: row.value, KeyPrefix: "test-", KeyHash: database.HashKey(row.value), Scopes: []string{row.scope}, IsActive: true, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	handler := routing.NewRouter(routing.RouterDependencies{HealthHandler: health.NewHandler("test"), ForecastHandler: capability.NewForecastHandler(reg, eng, nil), AuthMiddleware: auth.Middleware(auth.NewAuthenticator(store), store)})
	for _, tc := range []struct {
		key  string
		want int
	}{{"", 401}, {"weather-key", 403}, {"forecast-key", 200}} {
		req := httptest.NewRequest("GET", "/v1/forecast?lat=21.25&lon=81.63", nil)
		if tc.key != "" {
			req.Header.Set("X-API-Key", tc.key)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("key=%q status=%d want=%d body=%s", tc.key, rec.Code, tc.want, rec.Body.String())
		}
	}
}
