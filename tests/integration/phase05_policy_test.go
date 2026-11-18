package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/georesolve"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

var owOK = `{"coord":{"lat":21.2,"lon":82.1},"weather":[{"id":800,"main":"Clear"}],"main":{"temp":298.15,"humidity":40},"dt":` + fmt.Sprint(time.Now().Unix()) + `,"name":"Test"}`
var wapiOK = `{"location":{"name":"Test","country":"Test","lat":21.2,"lon":82.1},"current":{"temp_c":20.0,"humidity":55,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `,"condition":{"text":"Clear","code":1000}}}`

func policyForTest(t *testing.T, ow providerpolicy.Coverage, allowOW bool) *providerpolicy.Catalog {
	t.Helper()
	doc := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{
		{ProviderID: "openweather", Enabled: allowOW, ReviewStatus: "operator_approved", Products: []string{"current_weather"}, DeploymentUses: []string{"commercial"}, Coverage: ow},
		{ProviderID: "weatherapi", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"current_weather"}, DeploymentUses: []string{"commercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
	}}
	c, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestPhase05_CoordinatesAndWarmCacheEnforceChangedPolicy(t *testing.T) {
	var owCalls, wapiCalls atomic.Int64
	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { owCalls.Add(1); _, _ = w.Write([]byte(owOK)) }))
	defer owServer.Close()
	wapiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wapiCalls.Add(1); _, _ = w.Write([]byte(wapiOK)) }))
	defer wapiServer.Close()
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fake", owServer.URL))
	reg.Register(weatherapi.NewAdapter("fake", wapiServer.URL))
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetPolicyCatalog(policyForTest(t, providerpolicy.Coverage{Kind: "boxes", Boxes: []providerpolicy.Box{{MinLatitude: 20, MaxLatitude: 23, MinLongitude: 80, MaxLongitude: 84}}}, true), "commercial")
	mem := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	defer mem.Close()
	h := capability.NewWeatherHandler(reg, eng, normalization.NewWeatherNormalizer(), nil, mem, cache.NewPolicyEngine())
	request := func(url string) (int, string) {
		r := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		h.GetWeather(w, r)
		var env struct {
			Meta api.Meta `json:"meta"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		return w.Code, env.Meta.Provider
	}
	// Before policy change, OpenWeather is priority candidate at this coordinate.
	status, p := request("/v1/weather?lat=21.2&lon=82.1&policy=PRIORITY_FIRST")
	if status != 200 || p != "openweather" {
		t.Fatalf("first got %d %s", status, p)
	}
	// Same request and cache key, but policy is now disabled. Must not replay
	// the previously cached OpenWeather response.
	eng.SetPolicyCatalog(policyForTest(t, providerpolicy.Coverage{Kind: "global"}, false), "commercial")
	status, p = request("/v1/weather?lat=21.2&lon=82.1&policy=PRIORITY_FIRST")
	if status != 200 || p != "weatherapi" {
		t.Fatalf("cached response bypassed policy %d %s", status, p)
	}
	if owCalls.Load() != 1 || wapiCalls.Load() != 1 {
		t.Fatalf("unexpected upstream calls ow=%d wapi=%d", owCalls.Load(), wapiCalls.Load())
	}
}
func TestPhase05_CountryOnlyUnknownDeniedAndVerifiedIndexAllowed(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fake", "http://ow.example"))
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	doc := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "openweather", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"current_weather"}, DeploymentUses: []string{"commercial"}, Coverage: providerpolicy.Coverage{Kind: "countries", Countries: []string{"AA"}}}}}
	catalog, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	eng.SetPolicyCatalog(catalog, "commercial")
	lat, lon := 2.0, 2.0
	req := decision.RequestContext{Capability: "weather", Product: "current_weather", Latitude: &lat, Longitude: &lon}
	if _, _, err := eng.SelectProviders(context.Background(), req); err == nil {
		t.Fatal("coordinates were wrongly mapped to a country without a verified index")
	}
	idx := &providerpolicy.CountryIndex{Version: 1, Countries: []providerpolicy.CountryShape{{CountryCode: "AA", Coverage: providerpolicy.Coverage{Kind: "polygons", Polygons: []providerpolicy.Polygon{{Outer: []providerpolicy.Point{{Latitude: 0, Longitude: 0}, {Latitude: 0, Longitude: 10}, {Latitude: 10, Longitude: 10}, {Latitude: 10, Longitude: 0}}}}}}}}
	if err := idx.Validate(); err != nil {
		t.Fatal(err)
	}
	eng.SetCountryIndex(idx)
	providers, _, err := eng.SelectProviders(context.Background(), req)
	if err != nil || len(providers) != 1 {
		t.Fatalf("verified country coordinate not allowed: %v", err)
	}
	lat = 20
	if _, _, err = eng.SelectProviders(context.Background(), req); err == nil {
		t.Fatal("outside verified polygon allowed")
	}
}
func TestPhase05_OptionalPlaceResolver(t *testing.T) {
	var weatherCalls atomic.Int64
	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		weatherCalls.Add(1)
		if r.URL.Query().Get("lat") != "21.200000" {
			t.Errorf("weather request must use canonical coords: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(owOK))
	}))
	defer owServer.Close()
	ambiguous := false
	geoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ambiguous {
			_, _ = w.Write([]byte(`[{"lat":"21.2","lon":"82.1","display_name":"Test, AA"},{"lat":"22.2","lon":"82.1","display_name":"Test, BB"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"lat":"21.2","lon":"82.1","display_name":"Test, AA","address":{"country_code":"aa"}}]`))
	}))
	defer geoServer.Close()
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fake", owServer.URL))
	reg.Register(locationiq.NewAdapter("fake", geoServer.URL))
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	doc := providerpolicy.DefaultDocument()
	doc.Rules[2].ReviewStatus = "operator_approved"
	doc.Rules[2].Enabled = true
	cat, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	eng.SetPolicyCatalog(cat, "commercial")
	resolver := georesolve.New(reg, eng)
	h := capability.NewWeatherHandler(reg, eng, normalization.NewWeatherNormalizer(), nil, nil, nil)
	h.SetPlaceResolver(resolver)
	r := httptest.NewRequest("GET", "/v1/weather?location=Test%2C+AA", nil)
	w := httptest.NewRecorder()
	h.GetWeather(w, r)
	if w.Code != 200 || weatherCalls.Load() != 1 {
		t.Fatalf("strict resolver failed %d %s", w.Code, w.Body.String())
	}
	ambiguous = true
	r = httptest.NewRequest("GET", "/v1/weather?location=Unclear", nil)
	w = httptest.NewRecorder()
	h.GetWeather(w, r)
	if w.Code != 422 || weatherCalls.Load() != 1 {
		t.Fatalf("ambiguous location not rejected before weather request: %d %s", w.Code, w.Body.String())
	}
}
