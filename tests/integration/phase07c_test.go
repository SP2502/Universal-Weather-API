package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/geoapify"
	"github.com/universal-api/platform/providers/openmeteoairquality"
)

func TestPhase07C_DailySummariesReuseForecastCache(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(testForecastJSON())
	}))
	defer up.Close()
	handler, _ := testForecastHandler(t, up)
	one := httptest.NewRecorder()
	handler.GetForecast(one, httptest.NewRequest("GET", "/v1/forecast?lat=21.25&lon=81.63&points=5", nil))
	if one.Code != 200 {
		t.Fatalf("first %d: %s", one.Code, one.Body.String())
	}
	rec := httptest.NewRecorder()
	handler.GetDailyForecast(rec, httptest.NewRequest("GET", "/v1/forecast/daily?lat=21.25&lon=81.63&days=2", nil))
	if rec.Code != 200 {
		t.Fatalf("daily %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			SchemaVersion    string `json:"schema_version"`
			AggregationBasis string `json:"aggregation_basis"`
			Days             []struct {
				SampleCount int `json:"sample_count"`
			} `json:"days"`
		} `json:"data"`
		Meta struct {
			Cached bool `json:"cached"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.SchemaVersion != "forecast.daily.v1" || env.Data.AggregationBasis != "available_sampled_points_only" || len(env.Data.Days) != 2 || env.Data.Days[0].SampleCount < 1 || !env.Meta.Cached || calls.Load() != 1 {
		t.Fatalf("invalid daily derived result %+v calls=%d", env, calls.Load())
	}
	for _, q := range []string{"&days=0", "&days=8", "&days=abc", "&days=1&days=2", "&points=5"} {
		bad := httptest.NewRecorder()
		handler.GetDailyForecast(bad, httptest.NewRequest("GET", "/v1/forecast/daily?lat=21.25&lon=81.63"+q, nil))
		if bad.Code != 400 || calls.Load() != 1 {
			t.Fatalf("invalid query %s returned %d, calls=%d", q, bad.Code, calls.Load())
		}
	}
}
func TestPhase07C_AirQualityHandlerModelsNotMeasurements(t *testing.T) {
	var calls atomic.Int32
	data := func(pm string) string {
		return fmt.Sprintf(`{"latitude":21.25,"longitude":81.63,"hourly":{"time":[%d],"pm2_5":[%s],"pm10":[20],"ozone":[30],"nitrogen_dioxide":[4],"sulphur_dioxide":[1],"carbon_monoxide":[13]}}`, time.Now().UTC().Add(time.Hour).Unix(), pm)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/air-quality" || r.URL.Query().Get("timezone") != "UTC" || r.URL.Query().Get("timeformat") != "unixtime" || r.URL.Query().Get("hourly") == "" {
			t.Errorf("bad vendor request %s", r.URL.String())
		}
		fmt.Fprint(w, data("0"))
	}))
	defer upstream.Close()
	reg := provider.NewRegistry()
	reg.Register(openmeteoairquality.NewAdapter(upstream.URL).WithHTTPClient(upstream.Client()))
	doc := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "open_meteo_air_quality", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"air_quality_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}}
	policy, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(policy, "noncommercial")
	h := capability.NewAirQualityHandler(reg, eng, nil)
	rec := httptest.NewRecorder()
	h.GetAirQuality(rec, httptest.NewRequest("GET", "/v1/air-quality?lat=21.25&lon=81.63", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			DataKind    string `json:"data_kind"`
			ModelSource string `json:"model_source"`
			Points      []struct {
				PM25 struct {
					Value float64 `json:"value"`
					Unit  string  `json:"unit"`
				} `json:"pm2_5"`
			} `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.DataKind != "model_forecast" || len(resp.Data.Points) != 1 || resp.Data.Points[0].PM25.Value != 0 || resp.Data.Points[0].PM25.Unit != "µg/m³" {
		t.Fatalf("bad response %+v", resp)
	}
	bad := httptest.NewRecorder()
	h.GetAirQuality(bad, httptest.NewRequest("GET", "/v1/air-quality?lat=NaN&lon=0", nil))
	if bad.Code != 400 || calls.Load() != 1 {
		t.Fatalf("malformed request passed %d", bad.Code)
	}
	eng.SetPolicyCatalog(policy, "commercial")
	rejected := httptest.NewRecorder()
	h.GetAirQuality(rejected, httptest.NewRequest("GET", "/v1/air-quality?lat=21.25&lon=81.63", nil))
	if rejected.Code != 503 || calls.Load() != 1 {
		t.Fatalf("commercial hosted free was allowed %d", rejected.Code)
	}
	_ = data // explicit for fixture documentation
}
func TestPhase07C_GeocodingCacheExplicitPolicyAndLimit(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"results":[{"lat":21.25,"lon":81.63,"formatted":"Raipur, India","city":"Raipur"}]}`)
	}))
	defer up.Close()
	reg := provider.NewRegistry()
	reg.Register(geoapify.NewAdapter("fake-test-key", up.URL).WithHTTPClient(up.Client()))
	doc := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "geoapify", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"forward_geocoding"}, DeploymentUses: []string{"internal"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}}
	policy, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(policy, "internal")
	h := capability.NewGeocodingHandler(reg, eng, nil, nil)
	store := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	defer store.Close()
	h.ConfigureCache(store, []string{"geoapify"}, time.Minute)
	for i, q := range []string{"q=Raipur&limit=1", "q=Raipur&limit=1", "q=Raipur&limit=2", "q=Raipur&limit=2&provider=geoapify"} {
		rr := httptest.NewRecorder()
		h.Geocode(rr, httptest.NewRequest("GET", "/v1/geocode?"+q, nil))
		if rr.Code != 200 {
			t.Fatalf("%s status %d: %s", q, rr.Code, rr.Body.String())
		}
		var envelope struct {
			Meta struct {
				Cached bool `json:"cached"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Meta.Cached != (i == 1) {
			t.Fatalf("%s expected cached=%v got %v", q, i == 1, envelope.Meta.Cached)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 vendor calls, got %d", calls.Load())
	}
	eng.SetPolicyCatalog(policy, "commercial")
	bad := httptest.NewRecorder()
	h.Geocode(bad, httptest.NewRequest("GET", "/v1/geocode?q=Raipur&limit=1", nil))
	if bad.Code != 503 || calls.Load() != 3 {
		t.Fatalf("cache bypassed policy: %d", bad.Code)
	}
}
