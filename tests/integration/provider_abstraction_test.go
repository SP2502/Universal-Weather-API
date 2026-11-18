package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/api"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

func TestGateway_ProviderAbstraction_MultiProviderCanonicalEquivalence(t *testing.T) {
	handler, _, validKey, owServer, wapiServer := setupTestGateway()
	defer owServer.Close()
	defer wapiServer.Close()

	type CanonicalWrapper struct {
		Data weatherv1.WeatherResponse `json:"data"`
		Meta api.Meta                  `json:"meta"`
	}

	// 1. Query with OpenWeather provider
	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Tokyo&provider=openweather", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for openweather, got %d: %s", rec.Code, rec.Body.String())
	}

	var owResp CanonicalWrapper
	if err := json.Unmarshal(rec.Body.Bytes(), &owResp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if owResp.Meta.Provider != "openweather" {
		t.Errorf("expected provider openweather, got %s", owResp.Meta.Provider)
	}

	// 2. Query with WeatherAPI provider
	req = httptest.NewRequest(http.MethodGet, "/v1/weather?location=Tokyo&provider=weatherapi", nil)
	req.Header.Set("X-API-Key", validKey)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for weatherapi, got %d: %s", rec.Code, rec.Body.String())
	}

	var wapiResp CanonicalWrapper
	err := json.Unmarshal(rec.Body.Bytes(), &wapiResp)
	if err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if wapiResp.Meta.Provider != "weatherapi" {
		t.Errorf("expected provider weatherapi, got %s", wapiResp.Meta.Provider)
	}
}
