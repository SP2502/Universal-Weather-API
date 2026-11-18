package integration

import (
	"context"
	"encoding/json"
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
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

func setupGeocodingTestGateway() (http.Handler, string, *httptest.Server, *httptest.Server) {
	nomServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/search" {
			_, _ = w.Write([]byte(`[{
				"place_id": 1001,
				"lat": "51.5074",
				"lon": "-0.1278",
				"display_name": "London, UK",
				"class": "place",
				"type": "city",
				"importance": 0.95,
				"address": {"city": "London", "country": "United Kingdom", "country_code": "gb"}
			}]`))
		} else {
			_, _ = w.Write([]byte(`{
				"place_id": 2002,
				"lat": "51.5074",
				"lon": "-0.1278",
				"display_name": "10 Downing Street, London",
				"class": "building",
				"type": "yes",
				"importance": 0.85,
				"address": {"house_number": "10", "road": "Downing Street", "city": "London", "country": "United Kingdom", "country_code": "gb"}
			}`))
		}
	}))

	liqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/search.php" {
			_, _ = w.Write([]byte(`[{
				"place_id": "liq-991",
				"lat": "48.8566",
				"lon": "2.3522",
				"display_name": "Paris, Île-de-France, France",
				"class": "place",
				"type": "city",
				"importance": 0.98,
				"address": {"city": "Paris", "state": "Île-de-France", "country": "France", "country_code": "fr"}
			}]`))
		} else {
			_, _ = w.Write([]byte(`{
				"place_id": "liq-992",
				"lat": "48.8584",
				"lon": "2.2945",
				"display_name": "Eiffel Tower, Paris, France",
				"class": "tourism",
				"type": "attraction",
				"importance": 0.99,
				"address": {"name": "Eiffel Tower", "city": "Paris", "country": "France", "country_code": "fr"}
			}`))
		}
	}))

	store := database.NewMemoryStore()
	validRawKey := "uap_geocode_test_key"

	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-geo-1",
		Name:      "Geocoding Test Key",
		KeyPrefix: "uap_geo_",
		KeyHash:   database.HashKey(validRawKey),
		Scopes:    []string{"geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(nominatim.NewAdapter(nomServer.URL))
	registry.Register(locationiq.NewAdapter("dummy-key", liqServer.URL))

	geocodingNormalizer := normalization.NewGeocodingNormalizer()

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	usageTracker := usage.NewTracker(store)

	geocodingHandler := capability.NewGeocodingHandler(registry, nil, geocodingNormalizer, usageTracker)
	healthHandler := health.NewHandler("1.0.0")

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:    healthHandler,
		GeocodingHandler: geocodingHandler,
		AuthMiddleware:   authMid,
	})

	logger := logging.NewLogger("INFO", io.Discard)
	var handler http.Handler = appRouter
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(logger)(handler)

	return handler, validRawKey, nomServer, liqServer
}

func TestGateway_Geocoding_EndToEnd(t *testing.T) {
	handler, validKey, nomServer, liqServer := setupGeocodingTestGateway()
	defer nomServer.Close()
	defer liqServer.Close()

	type CanonicalGeocodeEnvelope struct {
		Data locv1.GeocodeResponse `json:"data"`
		Meta api.Meta              `json:"meta"`
	}

	// 1. Forward Geocoding (Nominatim primary)
	req := httptest.NewRequest(http.MethodGet, "/v1/geocode?q=London", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for forward geocode, got %d: %s", rec.Code, rec.Body.String())
	}

	var fwdResp CanonicalGeocodeEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &fwdResp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if fwdResp.Meta.Provider != "nominatim" {
		t.Errorf("expected primary provider nominatim, got %s", fwdResp.Meta.Provider)
	}
	if len(fwdResp.Data.Results) == 0 || fwdResp.Data.Results[0].City != "London" {
		t.Errorf("unexpected forward geocode payload: %+v", fwdResp.Data)
	}

	// 2. Reverse Geocoding (LocationIQ explicitly requested)
	req = httptest.NewRequest(http.MethodGet, "/v1/geocode?lat=48.8584&lon=2.2945&provider=locationiq", nil)
	req.Header.Set("X-API-Key", validKey)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for reverse geocode, got %d: %s", rec.Code, rec.Body.String())
	}

	var revResp CanonicalGeocodeEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &revResp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if revResp.Meta.Provider != "locationiq" {
		t.Errorf("expected provider locationiq, got %s", revResp.Meta.Provider)
	}
	if len(revResp.Data.Results) == 0 || revResp.Data.Results[0].City != "Paris" {
		t.Errorf("unexpected reverse geocode payload: %+v", revResp.Data)
	}
}
