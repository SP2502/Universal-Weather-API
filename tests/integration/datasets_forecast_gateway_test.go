package integration_test

import (
	"context"
	"encoding/json"
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
)

func TestDatasetsGatewayForecastAndDailyOptions(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(testForecastJSON())
	}))
	defer upstream.Close()
	forecast, _ := testForecastHandler(t, upstream)
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_forecast_bundle"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "forecast-bundle", Name: "forecast bundle", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"forecast:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	router := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:   health.NewHandler("test"),
		WeatherHandler:  capability.NewWeatherHandler(provider.NewRegistry(), nil, normalization.NewWeatherNormalizer(), nil, nil, nil),
		ForecastHandler: forecast,
		AuthMiddleware:  auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:  "lean_v1",
	})
	req := httptest.NewRequest("GET", "/v1/datasets?datasets=forecast,forecast-daily&lat=21.25&lon=81.63&forecast.points=5&forecast-daily.days=2", nil)
	req.Header.Set("X-API-Key", key)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var body struct {
		Data map[string]struct {
			Data struct {
				SchemaVersion string            `json:"schema_version"`
				Points        []json.RawMessage `json:"points"`
				Days          []json.RawMessage `json:"days"`
			} `json:"data"`
			Meta struct {
				Provider string `json:"provider"`
				Cached   bool   `json:"cached"`
			} `json:"meta"`
		} `json:"data"`
		Errors map[string]json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(body.Errors) != 0 || len(body.Data) != 2 {
		t.Fatalf("forecast bundle status=%d body=%s", rec.Code, rec.Body.String())
	}
	hourly, daily := body.Data["forecast"], body.Data["forecast-daily"]
	if hourly.Data.SchemaVersion != "forecast.v1" || len(hourly.Data.Points) != 5 || daily.Data.SchemaVersion != "forecast.daily.v1" || len(daily.Data.Days) != 2 || hourly.Meta.Provider != "open_meteo_forecast" || daily.Meta.Provider != "open_meteo_forecast" || !daily.Meta.Cached || calls.Load() != 1 {
		t.Fatalf("forecast bundle hourly=%+v daily=%+v upstream_calls=%d", hourly, daily, calls.Load())
	}
	longReq := httptest.NewRequest("GET", "/v1/datasets?datasets=forecast,forecast-daily&lat=21.25&lon=81.63&forecast.points=72&forecast-daily.days=2", nil)
	longReq.Header.Set("X-API-Key", key)
	longRec := httptest.NewRecorder()
	router.ServeHTTP(longRec, longReq)
	var longBody struct {
		Data map[string]struct {
			Data struct {
				Points []json.RawMessage `json:"points"`
				Days   []json.RawMessage `json:"days"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(longRec.Body.Bytes(), &longBody); err != nil {
		t.Fatal(err)
	}
	if longRec.Code != 200 || len(longBody.Data["forecast"].Data.Points) != 72 || len(longBody.Data["forecast-daily"].Data.Days) != 2 || calls.Load() != 1 {
		t.Fatalf("72-point bundle status=%d points=%d days=%d upstream_calls=%d", longRec.Code, len(longBody.Data["forecast"].Data.Points), len(longBody.Data["forecast-daily"].Data.Days), calls.Load())
	}
	if path := os.Getenv("UWA_CAPTURE_FORECAST_BUNDLE"); path != "" {
		if err := os.WriteFile(path, longRec.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
