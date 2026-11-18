package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

func setupTestGateway() (http.Handler, *database.MemoryStore, string, *httptest.Server, *httptest.Server) {
	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1278, "lat": 51.5074},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 295.65, "humidity": 60},
			"name": "London",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))

	wapiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"location": {"name": "London", "country": "United Kingdom", "lat": 51.5074, "lon": -0.1278, "tz_id": "Europe/London"},
			"current": {
				"temp_c": 22.5,
				"humidity": 60,
				"condition": {"text": "Sunny", "code": 1000},
				"last_updated_epoch": ` + fmt.Sprint(time.Now().Unix()) + `
			}
		}`))
	}))

	store := database.NewMemoryStore()
	validRawKey := "uap_test_live_secret_key"

	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-tenant-1",
		Name:      "Tenant Production Key",
		KeyPrefix: "uap_test_",
		KeyHash:   database.HashKey(validRawKey),
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", owServer.URL))
	registry.Register(weatherapi.NewAdapter("dummy-key", wapiServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	tracker := usage.NewTracker(store)
	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, tracker, nil, nil)
	healthHandler := health.NewHandler("0.1.0")

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:  healthHandler,
		WeatherHandler: weatherHandler,
		AuthMiddleware: authMid,
	})

	logger := logging.NewLogger("INFO", io.Discard)
	var handler http.Handler = appRouter
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(logger)(handler)

	return handler, store, validRawKey, owServer, wapiServer
}

func TestGateway_E2E_AuthenticatedCapabilityFlow(t *testing.T) {
	handler, store, validKey, owServer, wapiServer := setupTestGateway()
	defer owServer.Close()
	defer wapiServer.Close()

	// 1. Unauthenticated request -> 401
	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", rec.Code)
	}

	// 2. Authenticated request -> 200 OK with Canonical Schema
	req = httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", validKey)
	req.Header.Set("X-Request-ID", "req-client-trace-101")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid key, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	type CanonicalWrapper struct {
		Data weatherv1.WeatherResponse `json:"data"`
		Meta api.Meta                  `json:"meta"`
	}

	var resp CanonicalWrapper
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response envelope: %v", err)
	}

	if resp.Meta.RequestID != "req-client-trace-101" {
		t.Errorf("expected request_id req-client-trace-101, got %s", resp.Meta.RequestID)
	}
	if resp.Data.SchemaVersion != "weather.v1" {
		t.Errorf("expected schema_version weather.v1, got %s", resp.Data.SchemaVersion)
	}
	if resp.Data.Temperature.Value != 22.5 {
		t.Errorf("expected 22.5 Celsius, got %f", resp.Data.Temperature.Value)
	}

	// 3. Verify usage record was saved
	time.Sleep(50 * time.Millisecond)
	if store.GetUsageCount() != 1 {
		t.Errorf("expected 1 usage record, got %d", store.GetUsageCount())
	}
}

func TestGateway_Weather_CoordinatesQuery(t *testing.T) {
	handler, _, validKey, owServer, wapiServer := setupTestGateway()
	defer owServer.Close()
	defer wapiServer.Close()

	// 1. Coordinates via ?lat=...&lon=...
	req := httptest.NewRequest(http.MethodGet, "/v1/weather?lat=51.5074&lon=-0.1278", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for lat/lon query, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 2. Coordinates via ?location=51.5074,-0.1278
	req2 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=51.5074,-0.1278", nil)
	req2.Header.Set("X-API-Key", validKey)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for comma-separated coordinate query, got %d (body: %s)", rec2.Code, rec2.Body.String())
	}

	// 3. Invalid out-of-bounds latitude
	reqBad := httptest.NewRequest(http.MethodGet, "/v1/weather?lat=195.0&lon=0.0", nil)
	reqBad.Header.Set("X-API-Key", validKey)
	recBad := httptest.NewRecorder()
	handler.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-bounds latitude, got %d", recBad.Code)
	}
}
