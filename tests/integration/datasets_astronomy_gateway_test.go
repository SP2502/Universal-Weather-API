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
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/sunrisesunset"
)

func TestDatasetsGatewayPolarAstronomy(t *testing.T) {
	var astronomyCalls atomic.Int32
	weatherUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"coord":{"lat":70,"lon":20},"weather":[{"id":800,"main":"Clear","description":"clear sky"}],"main":{"temp":278.15,"humidity":60},"name":"Arctic","dt":%d}`, time.Now().Unix())
	}))
	defer weatherUpstream.Close()
	astronomyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		astronomyCalls.Add(1)
		if r.URL.Path != "/v2" || r.URL.Query().Get("date") != "2026-06-21" {
			t.Errorf("incorrect astronomy upstream request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"date":"2026-06-21","tzid":"Europe/Oslo","lat":70.0,"lng":20.0,"sun_status":"midnight_sun","sunrise":null,"sunset":null,"solar_noon":"2026-06-21T12:00:00+02:00","moonrise":null,"moonset":null}`))
	}))
	defer astronomyUpstream.Close()
	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("fixture", weatherUpstream.URL))
	registry.Register(sunrisesunset.NewAdapter(astronomyUpstream.URL).WithHTTPClient(astronomyUpstream.Client()))
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "sunrise_sunset", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"astronomy"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	store := database.NewMemoryStore()
	defer store.Close()
	key := "uap_test_astronomy_bundle"
	if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: "astronomy-bundle", Name: "astronomy bundle", KeyPrefix: "uap_test_", KeyHash: database.HashKey(key), Scopes: []string{"weather:read", "astronomy:read"}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	router := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:   health.NewHandler("test"),
		WeatherHandler:  capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), nil, nil, nil),
		ExtendedHandler: capability.NewExtendedHandler(registry, engine, nil),
		AuthMiddleware:  auth.Middleware(auth.NewAuthenticator(store), store),
		ReleaseProfile:  "lean_v1",
	})
	request := func(date string) (int, struct {
		Data map[string]struct {
			Data struct {
				SunStatus string          `json:"sun_status"`
				Sunrise   json.RawMessage `json:"sunrise"`
				Sunset    json.RawMessage `json:"sunset"`
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
		req := httptest.NewRequest("GET", "/v1/datasets?datasets=weather,astronomy&lat=70&lon=20&astronomy.date="+date, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body struct {
			Data map[string]struct {
				Data struct {
					SunStatus string          `json:"sun_status"`
					Sunrise   json.RawMessage `json:"sunrise"`
					Sunset    json.RawMessage `json:"sunset"`
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
		if date == "2026-06-21" {
			if path := os.Getenv("UWA_CAPTURE_ASTRONOMY_BUNDLE"); path != "" {
				if err := os.WriteFile(path, rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		} else if path := os.Getenv("UWA_CAPTURE_PARTIAL_BUNDLE"); path != "" {
			if err := os.WriteFile(path, rec.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code, body
	}
	status, complete := request("2026-06-21")
	astronomy := complete.Data["astronomy"]
	if status != 200 || complete.Meta.Partial || len(complete.Data) != 2 || len(complete.Errors) != 0 || complete.Data["weather"].Meta.Provider != "openweather" || astronomy.Meta.Provider != "sunrise_sunset" || astronomy.Data.SunStatus != "midnight_sun" || string(astronomy.Data.Sunrise) != "null" || string(astronomy.Data.Sunset) != "null" || astronomyCalls.Load() != 1 {
		t.Fatalf("polar bundle status=%d data=%+v errors=%+v calls=%d", status, complete.Data, complete.Errors, astronomyCalls.Load())
	}
	status, partial := request("2026-13-42")
	if status != 207 || !partial.Meta.Partial || len(partial.Data) != 1 || len(partial.Errors) != 1 || partial.Data["weather"].Meta.Provider != "openweather" || len(partial.Errors["astronomy"]) == 0 || astronomyCalls.Load() != 1 {
		t.Fatalf("invalid date result status=%d data=%+v errors=%+v calls=%d", status, partial.Data, partial.Errors, astronomyCalls.Load())
	}
}
