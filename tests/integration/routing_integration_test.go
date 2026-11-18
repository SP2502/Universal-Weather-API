package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

func TestRouting_EndToEnd_DynamicSelectionAndFailover(t *testing.T) {
	// 1. Mock Servers
	owCalls := 0
	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owCalls++
		// OpenWeather returns 500 error to trigger failure feedback
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"cod": 500, "message": "internal upstream failure"}`))
	}))
	defer owServer.Close()

	wapiCalls := 0
	wapiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wapiCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"location":{"name":"London","country":"UK","lat":51.51,"lon":-0.13},"current":{"temp_c":18.5,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `,"condition":{"text":"Partly cloudy"}}}`))
	}))
	defer wapiServer.Close()

	// 2. Setup Platform
	store := database.NewMemoryStore()
	rawKey := "uap_test_routing_key_12345"
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "routing-test-key",
		Name:      "Routing Tenant",
		KeyPrefix: "uap_test_",
		KeyHash:   database.HashKey(rawKey),
		Scopes:    []string{"weather:read", "geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", owServer.URL))
	registry.Register(weatherapi.NewAdapter("dummy-key", wapiServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()
	decisionEngine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	usageTracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, decisionEngine, weatherNormalizer, usageTracker, nil, nil)
	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)

	appRouter := routing.NewRouter(routing.RouterDependencies{
		WeatherHandler: weatherHandler,
		AuthMiddleware: authMid,
	})

	// 3. First request: OpenWeather is priority 1, fails -> falls back to WeatherAPI
	req1 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req1.Header.Set("Authorization", "Bearer "+rawKey)
	rec1 := httptest.NewRecorder()
	appRouter.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 via fallback, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var resp1 struct {
		Meta api.Meta `json:"meta"`
	}
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	if resp1.Meta.Provider != "weatherapi" || !resp1.Meta.Fallback {
		t.Errorf("expected weatherapi with fallback=true on first call, got provider=%s, fallback=%v", resp1.Meta.Provider, resp1.Meta.Fallback)
	}

	// 4. Verify Health Evaluator recorded OpenWeather failure and WeatherAPI success
	owHealth := registry.HealthEvaluator().GetHealth("openweather")
	wapiHealth := registry.HealthEvaluator().GetHealth("weatherapi")

	if owHealth.FailureCount == 0 || owHealth.Score >= 1.0 {
		t.Errorf("expected openweather health score to drop below 1.0, got %f (failures: %d)", owHealth.Score, owHealth.FailureCount)
	}
	if wapiHealth.SuccessCount != 1 || wapiHealth.Score != 1.0 {
		t.Errorf("expected weatherapi health score 1.0, got %f", wapiHealth.Score)
	}

	// 5. Subsequent request: WeatherAPI now has higher health score -> Decision Engine routes to WeatherAPI first!
	req2 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London&policy=AVAILABILITY_FIRST", nil)
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	rec2 := httptest.NewRecorder()
	appRouter.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second call, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp2 struct {
		Meta api.Meta `json:"meta"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)

	if resp2.Meta.Provider != "weatherapi" || resp2.Meta.Fallback {
		t.Errorf("expected weatherapi as primary (fallback=false) due to higher health score, got provider=%s, fallback=%v", resp2.Meta.Provider, resp2.Meta.Fallback)
	}
}

func TestRouting_GeographicCoverage_Selection(t *testing.T) {
	// Nominatim returns French geocode
	nomServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"lat":"48.85","lon":"2.35","display_name":"Paris, France"}]`))
	}))
	defer nomServer.Close()

	// LocationIQ returns US geocode
	liqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"lat":"37.77","lon":"-122.41","display_name":"San Francisco, CA, USA"}]`))
	}))
	defer liqServer.Close()

	store := database.NewMemoryStore()
	rawKey := "uap_test_geo_key_12345"
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "geo-key",
		Name:      "Geo Tenant",
		KeyPrefix: "uap_test_",
		KeyHash:   database.HashKey(rawKey),
		Scopes:    []string{"geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(nominatim.NewAdapter(nomServer.URL))
	registry.Register(locationiq.NewAdapter("dummy-key", liqServer.URL))

	decisionEngine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	// Restrict nominatim to FR only, locationiq to US only
	decisionEngine.SetProviderMetadata("nominatim", decision.ProviderMetadata{
		CostClass:       decision.CostClassFree,
		CoverageRegions: []string{"FR"},
	})
	decisionEngine.SetProviderMetadata("locationiq", decision.ProviderMetadata{
		CostClass:       decision.CostClassStandard,
		CoverageRegions: []string{"US"},
	})

	geocodingNormalizer := normalization.NewGeocodingNormalizer()
	usageTracker := usage.NewTracker(store)
	geocodingHandler := capability.NewGeocodingHandler(registry, decisionEngine, geocodingNormalizer, usageTracker)

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)

	appRouter := routing.NewRouter(routing.RouterDependencies{
		GeocodingHandler: geocodingHandler,
		AuthMiddleware:   authMid,
	})

	// 1. Request with country=FR -> routes to Nominatim
	reqFR := httptest.NewRequest(http.MethodGet, "/v1/geocode?q=Paris&country=FR", nil)
	reqFR.Header.Set("Authorization", "Bearer "+rawKey)
	recFR := httptest.NewRecorder()
	appRouter.ServeHTTP(recFR, reqFR)

	if recFR.Code != http.StatusOK {
		t.Fatalf("expected 200 for FR geocode, got %d: %s", recFR.Code, recFR.Body.String())
	}
	var respFR struct {
		Meta api.Meta `json:"meta"`
	}
	_ = json.Unmarshal(recFR.Body.Bytes(), &respFR)
	if respFR.Meta.Provider != "nominatim" {
		t.Errorf("expected nominatim for country FR, got %s", respFR.Meta.Provider)
	}

	// 2. Request with country=US -> routes to LocationIQ
	reqUS := httptest.NewRequest(http.MethodGet, "/v1/geocode?q=San+Francisco&country=US", nil)
	reqUS.Header.Set("Authorization", "Bearer "+rawKey)
	recUS := httptest.NewRecorder()
	appRouter.ServeHTTP(recUS, reqUS)

	if recUS.Code != http.StatusOK {
		t.Fatalf("expected 200 for US geocode, got %d: %s", recUS.Code, recUS.Body.String())
	}
	var respUS struct {
		Meta api.Meta `json:"meta"`
	}
	_ = json.Unmarshal(recUS.Body.Bytes(), &respUS)
	if respUS.Meta.Provider != "locationiq" {
		t.Errorf("expected locationiq for country US, got %s", respUS.Meta.Provider)
	}
}
