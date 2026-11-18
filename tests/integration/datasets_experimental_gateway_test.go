package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openmeteoairquality"
	"github.com/universal-api/platform/providers/openweather"
)

func TestDatasetsGatewayExperimentalAirQualityAndLeanGate(t *testing.T) {
	var weatherCalls, airCalls atomic.Int32
	weatherUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		weatherCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"coord":{"lat":21.25,"lon":81.63},"weather":[{"id":800,"main":"Clear","description":"clear sky"}],"main":{"temp":295.15,"humidity":60},"name":"Raipur","dt":%d}`, time.Now().Unix())
	}))
	defer weatherUpstream.Close()
	airUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		airCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"latitude":21.25,"longitude":81.63,"hourly":{"time":[%d],"pm2_5":[11],"pm10":[20],"ozone":[30],"nitrogen_dioxide":[4],"sulphur_dioxide":[1],"carbon_monoxide":[13]}}`, time.Now().UTC().Add(time.Hour).Unix())
	}))
	defer airUpstream.Close()
	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("fixture", weatherUpstream.URL))
	registry.Register(openmeteoairquality.NewAdapter(airUpstream.URL).WithHTTPClient(airUpstream.Client()))
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "open_meteo_air_quality", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"air_quality_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_air_bundle"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "air-bundle", Name: "air bundle", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"weather:read", "air-quality:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	deps := routing.RouterDependencies{
		HealthHandler:     health.NewHandler("test"),
		WeatherHandler:    capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), nil, nil, nil),
		AirQualityHandler: capability.NewAirQualityHandler(registry, engine, nil),
		AuthMiddleware:    auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:    "full_experimental",
	}
	path := "/v1/datasets?datasets=weather,air-quality&lat=21.25&lon=81.63"
	request := func(handler http.Handler) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	full := request(routing.NewRouter(deps))
	if path := os.Getenv("UWA_CAPTURE_EXPERIMENTAL_BUNDLE"); path != "" {
		if err := os.WriteFile(path, full.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var body struct {
		Data map[string]struct {
			Data struct {
				SchemaVersion string `json:"schema_version"`
				DataKind      string `json:"data_kind"`
			} `json:"data"`
			Meta struct {
				Provider    string            `json:"provider"`
				Attribution []json.RawMessage `json:"attribution"`
			} `json:"meta"`
		} `json:"data"`
		Errors map[string]json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(full.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if full.Code != 200 || len(body.Data) != 2 || len(body.Errors) != 0 || body.Data["weather"].Meta.Provider != "openweather" || body.Data["air-quality"].Meta.Provider != "open_meteo_air_quality" || body.Data["air-quality"].Data.DataKind != "model_forecast" || len(body.Data["air-quality"].Meta.Attribution) == 0 || weatherCalls.Load() != 1 || airCalls.Load() != 1 {
		t.Fatalf("experimental bundle status=%d body=%s calls=%d/%d", full.Code, full.Body.String(), weatherCalls.Load(), airCalls.Load())
	}
	deps.ReleaseProfile = "lean_v1"
	lean := request(routing.NewRouter(deps))
	if lean.Code != 400 || weatherCalls.Load() != 1 || airCalls.Load() != 1 {
		t.Fatalf("lean gate status=%d body=%s calls=%d/%d", lean.Code, lean.Body.String(), weatherCalls.Load(), airCalls.Load())
	}
	engine.SetPolicyCatalog(policy, "commercial")
	deps.ReleaseProfile = "full_experimental"
	commercial := request(routing.NewRouter(deps))
	var partial struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors map[string]json.RawMessage `json:"errors"`
		Meta   struct {
			Partial bool `json:"partial"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(commercial.Body.Bytes(), &partial); err != nil {
		t.Fatal(err)
	}
	if commercial.Code != 207 || !partial.Meta.Partial || len(partial.Data) != 1 || len(partial.Errors) != 1 || len(partial.Data["weather"]) == 0 || len(partial.Errors["air-quality"]) == 0 || airCalls.Load() != 1 {
		t.Fatalf("commercial gate status=%d body=%s air_calls=%d", commercial.Code, commercial.Body.String(), airCalls.Load())
	}
}
