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
	"github.com/universal-api/platform/providers/nwsalerts"
	"github.com/universal-api/platform/providers/openmeteohistory"
	"github.com/universal-api/platform/providers/openmeteomarine"
)

func TestDatasetsGatewayHistoryMarinePartial(t *testing.T) {
	start := time.Now().UTC().AddDate(0, 0, -15).Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	var historyCalls, marineCalls atomic.Int32
	var distantMarine atomic.Bool
	historyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		historyCalls.Add(1)
		if r.URL.Path != "/v1/archive" || r.URL.Query().Get("start_date") != start.Format("2006-01-02") || r.URL.Query().Get("end_date") != end.Format("2006-01-02") {
			t.Errorf("incorrect history upstream request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"latitude":0,"longitude":0,"hourly_units":{"temperature_2m":"°C","relative_humidity_2m":"%%","precipitation":"mm"},"hourly":{"time":[%d],"temperature_2m":[0],"relative_humidity_2m":[50],"precipitation":[0]}}`, start.Unix())
	}))
	defer historyUpstream.Close()
	marineUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marineCalls.Add(1)
		if r.URL.Path != "/v1/marine" {
			t.Errorf("incorrect marine upstream request: %s", r.URL)
		}
		lat := 0.1
		if distantMarine.Load() {
			lat = 25
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"latitude":%.4f,"longitude":0,"hourly_units":{"wave_height":"m","wave_period":"s","wave_direction":"°"},"hourly":{"time":[%d],"wave_height":[0],"wave_period":[7],"wave_direction":[0]}}`, lat, time.Now().UTC().Add(time.Hour).Unix())
	}))
	defer marineUpstream.Close()
	registry := provider.NewRegistry()
	registry.Register(openmeteohistory.NewAdapter(historyUpstream.URL).WithHTTPClient(historyUpstream.Client()))
	registry.Register(openmeteomarine.NewAdapter(marineUpstream.URL).WithHTTPClient(marineUpstream.Client()))
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{
		{ProviderID: "open_meteo_history", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"historical_reanalysis"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
		{ProviderID: "open_meteo_marine", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"marine_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_extended_bundle"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "extended-bundle", Name: "extended bundle", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"history:read", "marine:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	router := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:   health.NewHandler("test"),
		WeatherHandler:  capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), nil, nil, nil),
		ExtendedHandler: capability.NewExtendedHandler(registry, engine, nil),
		AuthMiddleware:  auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:  "full_experimental",
	})
	path := fmt.Sprintf("/v1/datasets?datasets=history,marine&lat=0&lon=0&history.start_date=%s&history.end_date=%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	request := func() (int, struct {
		Data map[string]struct {
			Data struct {
				SchemaVersion string `json:"schema_version"`
			} `json:"data"`
			Meta struct {
				Provider string `json:"provider"`
			} `json:"meta"`
		} `json:"data"`
		Errors map[string]json.RawMessage `json:"errors"`
		Meta   struct {
			Partial bool `json:"partial"`
		} `json:"meta"`
	}) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body struct {
			Data map[string]struct {
				Data struct {
					SchemaVersion string `json:"schema_version"`
				} `json:"data"`
				Meta struct {
					Provider string `json:"provider"`
				} `json:"meta"`
			} `json:"data"`
			Errors map[string]json.RawMessage `json:"errors"`
			Meta   struct {
				Partial bool `json:"partial"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code == 200 {
			if path := os.Getenv("UWA_CAPTURE_EXTENDED_BUNDLE"); path != "" {
				if err := os.WriteFile(path, rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return rec.Code, body
	}
	status, complete := request()
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 || len(complete.Errors) != 0 || complete.Data["history"].Meta.Provider != "open_meteo_history" || complete.Data["marine"].Meta.Provider != "open_meteo_marine" || historyCalls.Load() != 1 || marineCalls.Load() != 1 {
		t.Fatalf("extended bundle status=%d data=%+v errors=%+v calls=%d/%d", status, complete.Data, complete.Errors, historyCalls.Load(), marineCalls.Load())
	}
	distantMarine.Store(true)
	status, partial := request()
	if status != 207 || !partial.Meta.Partial || len(partial.Data) != 1 || len(partial.Errors) != 1 || partial.Data["history"].Meta.Provider != "open_meteo_history" || len(partial.Errors["marine"]) == 0 {
		t.Fatalf("distant marine result status=%d data=%+v errors=%+v", status, partial.Data, partial.Errors)
	}
}

func TestDatasetsGatewayHistoryAndAlerts(t *testing.T) {
	start := time.Now().UTC().AddDate(0, 0, -15).Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	var invalidAlerts atomic.Bool
	historyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"latitude":40,"longitude":-75,"hourly_units":{"temperature_2m":"°C","relative_humidity_2m":"%%","precipitation":"mm"},"hourly":{"time":[%d],"temperature_2m":[10],"relative_humidity_2m":[50],"precipitation":[0]}}`, start.Unix())
	}))
	defer historyUpstream.Close()
	alertsUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alerts/active" || r.URL.Query().Get("point") != "40.0000,-75.0000" {
			t.Errorf("incorrect alerts upstream request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/geo+json")
		if invalidAlerts.Load() {
			_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[{"properties":{"event":"Storm"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer alertsUpstream.Close()
	registry := provider.NewRegistry()
	registry.Register(openmeteohistory.NewAdapter(historyUpstream.URL).WithHTTPClient(historyUpstream.Client()))
	registry.Register(nwsalerts.NewAdapter("UWA Test (ops@example.org)", alertsUpstream.URL).WithHTTPClient(alertsUpstream.Client()))
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{
		{ProviderID: "open_meteo_history", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"historical_reanalysis"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
		{ProviderID: "us_nws_alerts", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"active_alerts"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_alerts_bundle"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "alerts-bundle", Name: "alerts bundle", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"history:read", "alerts:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	router := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:   health.NewHandler("test"),
		WeatherHandler:  capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), nil, nil, nil),
		ExtendedHandler: capability.NewExtendedHandler(registry, engine, nil),
		AuthMiddleware:  auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:  "full_experimental",
	})
	path := fmt.Sprintf("/v1/datasets?datasets=history,alerts&lat=40&lon=-75&history.start_date=%s&history.end_date=%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	request := func() (int, struct {
		Data map[string]struct {
			Data struct {
				Alerts json.RawMessage `json:"alerts"`
			} `json:"data"`
			Meta struct {
				Provider string `json:"provider"`
			} `json:"meta"`
		} `json:"data"`
		Errors map[string]json.RawMessage `json:"errors"`
	}) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body struct {
			Data map[string]struct {
				Data struct {
					Alerts json.RawMessage `json:"alerts"`
				} `json:"data"`
				Meta struct {
					Provider string `json:"provider"`
				} `json:"meta"`
			} `json:"data"`
			Errors map[string]json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code == 200 {
			if path := os.Getenv("UWA_CAPTURE_EXTENDED_BUNDLE"); path != "" {
				if err := os.WriteFile(path, rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return rec.Code, body
	}
	status, complete := request()
	if status != 200 || len(complete.Data) != 2 || len(complete.Errors) != 0 || complete.Data["history"].Meta.Provider != "open_meteo_history" || complete.Data["alerts"].Meta.Provider != "us_nws_alerts" || string(complete.Data["alerts"].Data.Alerts) != "[]" {
		t.Fatalf("alerts bundle status=%d data=%+v errors=%+v", status, complete.Data, complete.Errors)
	}
	invalidAlerts.Store(true)
	status, partial := request()
	if status != 207 || len(partial.Data) != 1 || len(partial.Errors) != 1 || partial.Data["history"].Meta.Provider != "open_meteo_history" || len(partial.Errors["alerts"]) == 0 {
		t.Fatalf("malformed alerts result status=%d data=%+v errors=%+v", status, partial.Data, partial.Errors)
	}
}
