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
	"github.com/universal-api/platform/providers/nwsalerts"
	"github.com/universal-api/platform/providers/openmeteohistory"
	"github.com/universal-api/platform/providers/openmeteomarine"
	"github.com/universal-api/platform/providers/sunrisesunset"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func extendedTestHandler(t *testing.T, p provider.Provider, product, usage string) *capability.ExtendedHandler {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	cat, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: p.Manifest().ID, Enabled: true, ReviewStatus: "operator_approved", Products: []string{product}, DeploymentUses: []string{usage}, Coverage: providerpolicy.Coverage{Kind: "global"}}}})
	if err != nil {
		t.Fatal(err)
	}
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(cat, usage)
	return capability.NewExtendedHandler(reg, eng, nil)
}
func TestPhase07D_HistoricalReanalysisAndInvalidData(t *testing.T) {
	var calls atomic.Int32
	start := time.Now().UTC().AddDate(0, 0, -15).Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	invalid := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/archive" || r.URL.Query().Get("models") != "era5" || r.URL.Query().Get("start_date") == "" {
			t.Errorf("bad history request: %s", r.URL)
		}
		temp := "0"
		if invalid {
			temp = "null"
		}
		fmt.Fprintf(w, `{"latitude":21.25,"longitude":81.63,"hourly_units":{"temperature_2m":"°C","relative_humidity_2m":"%%","precipitation":"mm"},"hourly":{"time":[%d],"temperature_2m":[%s],"relative_humidity_2m":[50],"precipitation":[0]}}`, start.Unix(), temp)
	}))
	defer up.Close()
	h := extendedTestHandler(t, openmeteohistory.NewAdapter(up.URL).WithHTTPClient(up.Client()), "historical_reanalysis", "noncommercial")
	url := "/v1/history?lat=21.25&lon=81.63&start_date=" + start.Format("2006-01-02") + "&end_date=" + end.Format("2006-01-02")
	rec := httptest.NewRecorder()
	h.GetHistory(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"historical_reanalysis"`) || !strings.Contains(rec.Body.String(), `"temperature_c":0`) {
		t.Fatalf("history %d: %s", rec.Code, rec.Body.String())
	}
	invalid = true
	bad := httptest.NewRecorder()
	h.GetHistory(bad, httptest.NewRequest("GET", url, nil))
	if bad.Code != 502 {
		t.Fatalf("missing temp accepted: %d %s", bad.Code, bad.Body.String())
	}
	old := calls.Load()
	invalidReq := httptest.NewRecorder()
	h.GetHistory(invalidReq, httptest.NewRequest("GET", "/v1/history?lat=NaN&lon=0&start_date=2024-01-01&end_date=2024-01-02", nil))
	if invalidReq.Code != 400 || calls.Load() != old {
		t.Fatal("malformed coordinates reached history provider")
	}
}
func TestPhase07D_PolarAstronomyNullable(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2" || r.URL.Query().Get("date") != "2026-06-21" {
			t.Errorf("bad astro request %s", r.URL)
		}
		fmt.Fprint(w, `{"date":"2026-06-21","tzid":"Europe/Oslo","lat":70.0,"lng":20.0,"sun_status":"midnight_sun","sunrise":null,"sunset":null,"solar_noon":"2026-06-21T12:00:00+02:00","moonrise":null,"moonset":null}`)
	}))
	defer up.Close()
	h := extendedTestHandler(t, sunrisesunset.NewAdapter(up.URL).WithHTTPClient(up.Client()), "astronomy", "commercial")
	rr := httptest.NewRecorder()
	h.GetAstronomy(rr, httptest.NewRequest("GET", "/v1/astronomy?lat=70&lon=20&date=2026-06-21", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"sunrise":null`) || !strings.Contains(rr.Body.String(), `"midnight_sun"`) {
		t.Fatalf("polar astronomy: %d %s", rr.Code, rr.Body.String())
	}
}
func TestPhase07D_MarineRejectsInlandAndNull(t *testing.T) {
	gridLat := 0.1
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"latitude":%.4f,"longitude":0,"hourly_units":{"wave_height":"m","wave_period":"s","wave_direction":"°"},"hourly":{"time":[%d],"wave_height":[0],"wave_period":[7],"wave_direction":[0]}}`, gridLat, time.Now().UTC().Add(time.Hour).Unix())
	}))
	defer up.Close()
	h := extendedTestHandler(t, openmeteomarine.NewAdapter(up.URL).WithHTTPClient(up.Client()), "marine_forecast", "noncommercial")
	rr := httptest.NewRecorder()
	h.GetMarine(rr, httptest.NewRequest("GET", "/v1/marine?lat=0&lon=0", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"wave_height_m":0`) {
		t.Fatalf("marine: %d %s", rr.Code, rr.Body.String())
	}
	gridLat = 25
	inland := httptest.NewRecorder()
	h.GetMarine(inland, httptest.NewRequest("GET", "/v1/marine?lat=0&lon=0", nil))
	if inland.Code != 502 {
		t.Fatalf("inland distant grid should fail: %d", inland.Code)
	}
}
func TestPhase07D_EmptyNWSAlertsAndMalformed(t *testing.T) {
	invalid := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alerts/active" || r.URL.Query().Get("point") != "40.0000,-75.0000" || r.Header.Get("Accept") != "application/geo+json" {
			t.Errorf("bad alert route: %s", r.URL)
		}
		if invalid {
			fmt.Fprint(w, `{"type":"FeatureCollection","features":[{"properties":{"event":"Storm"}}]}`)
		} else {
			fmt.Fprint(w, `{"type":"FeatureCollection","features":[]}`)
		}
	}))
	defer up.Close()
	h := extendedTestHandler(t, nwsalerts.NewAdapter("UWA Test (ops@example.org)", up.URL).WithHTTPClient(up.Client()), "active_alerts", "commercial")
	rr := httptest.NewRecorder()
	h.GetAlerts(rr, httptest.NewRequest("GET", "/v1/alerts?lat=40&lon=-75", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"alerts":[]`) {
		t.Fatalf("empty alert list should be array: %d %s", rr.Code, rr.Body.String())
	}
	invalid = true
	bad := httptest.NewRecorder()
	h.GetAlerts(bad, httptest.NewRequest("GET", "/v1/alerts?lat=40&lon=-75", nil))
	if bad.Code != 502 {
		t.Fatalf("malformed alert accepted: %d", bad.Code)
	}
}

func TestPhase07D_RouteAuthenticationAndScopes(t *testing.T) {
	store := database.NewMemoryStore()
	defer store.Close()
	for _, row := range []struct{ key, scope string }{{"astronomy-key", "astronomy:read"}, {"weather-only", "weather:read"}} {
		if err := store.CreateAPIKey(context.Background(), &database.APIKey{ID: row.key, Name: row.key, KeyPrefix: "test-", KeyHash: database.HashKey(row.key), Scopes: []string{row.scope}, IsActive: true, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	reg := provider.NewRegistry()
	registry := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "sunrise_sunset", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"astronomy"}, DeploymentUses: []string{"commercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}}
	policy, err := providerpolicy.NewCatalog(registry)
	if err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"date":"2026-06-21","tzid":"UTC","lat":0,"lng":0,"sun_status":"normal","sunrise":"2026-06-21T06:00:00+00:00","sunset":"2026-06-21T18:00:00+00:00"}`)
	}))
	defer up.Close()
	reg.Register(sunrisesunset.NewAdapter(up.URL).WithHTTPClient(up.Client()))
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(policy, "commercial")
	router := routing.NewRouter(routing.RouterDependencies{HealthHandler: health.NewHandler("test"), ExtendedHandler: capability.NewExtendedHandler(reg, eng, nil), AuthMiddleware: auth.Middleware(auth.NewAuthenticator(store), store)})
	for _, tc := range []struct {
		key  string
		want int
	}{{"", 401}, {"weather-only", 403}, {"astronomy-key", 200}} {
		req := httptest.NewRequest("GET", "/v1/astronomy?lat=0&lon=0&date=2026-06-21", nil)
		if tc.key != "" {
			req.Header.Set("X-API-Key", tc.key)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Fatalf("key=%q status %d body %s", tc.key, rr.Code, rr.Body.String())
		}
	}
}
