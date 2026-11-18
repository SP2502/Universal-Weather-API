package integration

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
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/openweather"
)

type datasetsQuotaGate struct{ denyGeocode *atomic.Bool }

func (g datasetsQuotaGate) AllowAttempt(_ context.Context, providerID string) (bool, error) {
	return providerID != "locationiq" || !g.denyGeocode.Load(), nil
}

func TestDatasetsGatewayTwoProvidersAndPartialResult(t *testing.T) {
	var weatherCalls, geocodeCalls atomic.Int32
	var weatherUnavailable atomic.Bool
	var geocodeUnavailable atomic.Bool
	var denyGeocode atomic.Bool
	weatherUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		weatherCalls.Add(1)
		if weatherUnavailable.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"coord":{"lat":51.5074,"lon":-0.1278},"weather":[{"id":800,"main":"Clear","description":"clear sky"}],"main":{"temp":280.15,"humidity":60},"name":"London","dt":%d}`, time.Now().Unix())
	}))
	defer weatherUpstream.Close()
	geoUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geocodeCalls.Add(1)
		if geocodeUnavailable.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/search.php" {
			_, _ = w.Write([]byte(`[{"place_id":"liq-2","lat":"51.5074","lon":"-0.1278","display_name":"London, UK","class":"place","type":"city","address":{"city":"London","country":"United Kingdom","country_code":"gb"}}]`))
			return
		}
		_, _ = w.Write([]byte(`{"place_id":"liq-1","lat":"51.5074","lon":"-0.1278","display_name":"London, UK","class":"place","type":"city","address":{"city":"London","country":"United Kingdom","country_code":"gb"}}`))
	}))
	defer geoUpstream.Close()
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_datasets_key"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "datasets-key", Name: "datasets-test", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"weather:read", "geocode:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("fixture", weatherUpstream.URL))
	registry.Register(locationiq.NewAdapter("fixture", geoUpstream.URL))
	tracker := usage.NewTracker(store)
	geocodeHandler := capability.NewGeocodingHandler(registry, nil, normalization.NewGeocodingNormalizer(), tracker)
	geocodeHandler.SetQuotaGate(datasetsQuotaGate{denyGeocode: &denyGeocode})
	router := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:    health.NewHandler("test"),
		WeatherHandler:   capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), tracker, nil, nil),
		GeocodingHandler: geocodeHandler,
		AuthMiddleware:   auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:   "lean_v1",
	})
	request := func(path string) (int, struct {
		Data map[string]struct {
			Data json.RawMessage `json:"data"`
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
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-API-Key", key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var body struct {
			Data map[string]struct {
				Data json.RawMessage `json:"data"`
				Meta struct {
					Provider string `json:"provider"`
				} `json:"meta"`
			} `json:"data"`
			Errors map[string]json.RawMessage `json:"errors"`
			Meta   struct {
				Partial bool `json:"partial"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("response status=%d: %v", w.Code, err)
		}
		if w.Code == 503 {
			if path := os.Getenv("UWA_CAPTURE_UNAVAILABLE_BUNDLE"); path != "" {
				if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return w.Code, body
	}
	coordinates := "/v1/datasets?datasets=weather,geocode&lat=51.5074&lon=-0.1278"
	status, complete := request(coordinates)
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 || complete.Data["weather"].Meta.Provider != "openweather" || complete.Data["geocode"].Meta.Provider != "locationiq" {
		t.Fatalf("combined result status=%d data=%+v errors=%s partial=%v", status, complete.Data, complete.Errors, complete.Meta.Partial)
	}
	status, complete = request("/v1/datasets?datasets=weather,geocode&location=London")
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 || complete.Data["weather"].Meta.Provider != "openweather" || complete.Data["geocode"].Meta.Provider != "locationiq" {
		t.Fatalf("place-name result status=%d data=%+v errors=%s", status, complete.Data, complete.Errors)
	}
	status, complete = request("/v1/datasets?datasets=weather,geocode&q=London")
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 || complete.Data["weather"].Meta.Provider != "openweather" || complete.Data["geocode"].Meta.Provider != "locationiq" {
		t.Fatalf("geocode-query result status=%d data=%+v errors=%s", status, complete.Data, complete.Errors)
	}
	status, complete = request("/v1/datasets?datasets=weather,geocode&location=London&geocode.limit=1")
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 {
		t.Fatalf("dataset option result status=%d data=%+v errors=%s", status, complete.Data, complete.Errors)
	}
	priorWeatherCalls, priorGeocodeCalls := weatherCalls.Load(), geocodeCalls.Load()
	status, _ = request("/v1/datasets?datasets=weather,geocode&location=London&forecast-daily.days=3")
	if status != 400 || weatherCalls.Load() != priorWeatherCalls || geocodeCalls.Load() != priorGeocodeCalls {
		t.Fatalf("unrequested dataset option status=%d weather=%d geocode=%d", status, weatherCalls.Load(), geocodeCalls.Load())
	}
	geocodeUnavailable.Store(true)
	status, partial := request(coordinates)
	if status != 207 || !partial.Meta.Partial || len(partial.Data) != 1 || len(partial.Errors) != 1 || partial.Data["weather"].Meta.Provider != "openweather" {
		t.Fatalf("partial result status=%d data=%+v errors=%+v", status, partial.Data, partial.Errors)
	}
	if weatherCalls.Load() != 5 || geocodeCalls.Load() < 5 {
		t.Fatalf("independent calls weather=%d geocode=%d", weatherCalls.Load(), geocodeCalls.Load())
	}
	weatherUnavailable.Store(true)
	status, unavailable := request(coordinates)
	if status != 503 || unavailable.Meta.Partial || len(unavailable.Data) != 0 || len(unavailable.Errors) != 2 {
		t.Fatalf("all-failed result status=%d data=%+v errors=%+v partial=%v", status, unavailable.Data, unavailable.Errors, unavailable.Meta.Partial)
	}
	weatherUnavailable.Store(false)
	geocodeUnavailable.Store(false)
	denyGeocode.Store(true)
	priorGeocodeCalls = geocodeCalls.Load()
	status, partial = request(coordinates)
	if status != 207 || !partial.Meta.Partial || len(partial.Data) != 1 || len(partial.Errors) != 1 || partial.Data["weather"].Meta.Provider != "openweather" {
		t.Fatalf("quota partial result status=%d data=%+v errors=%+v", status, partial.Data, partial.Errors)
	}
	if geocodeCalls.Load() != priorGeocodeCalls {
		t.Fatalf("quota-denied geocoding reached upstream: before=%d after=%d", priorGeocodeCalls, geocodeCalls.Load())
	}
}
