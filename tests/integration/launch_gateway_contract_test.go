package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

// TestLaunchGatewayContract checks the user-facing promise with two independent
// synthetic upstream formats, a location-limited provider, and actual HTTP
// request handling. This does not certify real provider keys or network access.
func TestLaunchGatewayContract(t *testing.T) {
	var primaryMode atomic.Int32
	var primaryCalls, secondaryCalls atomic.Int64
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		switch primaryMode.Load() {
		case 1:
			_, _ = w.Write([]byte(`{"main":{"humidity":30},"weather":[]}`))
			return
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, `{"coord":{"lat":21.2,"lon":82.1},"weather":[{"id":800,"main":"Clear"}],"main":{"temp":298.15,"humidity":40},"dt":`+fmt.Sprint(time.Now().Unix())+`,"name":"Regional"}`)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondaryCalls.Add(1)
		q := strings.Split(r.URL.Query().Get("q"), ",")
		if len(q) != 2 {
			t.Errorf("secondary not called with canonical coordinates: %q", r.URL.Query().Get("q"))
			http.Error(w, "bad coordinates", 400)
			return
		}
		lat, e1 := strconv.ParseFloat(strings.TrimSpace(q[0]), 64)
		lon, e2 := strconv.ParseFloat(strings.TrimSpace(q[1]), 64)
		if e1 != nil || e2 != nil {
			t.Errorf("secondary invalid coordinate request: %q", r.URL.Query().Get("q"))
			http.Error(w, "bad coordinates", 400)
			return
		}
		_, _ = fmt.Fprintf(w, `{"location":{"name":"Fallback","country":"Test","lat":%f,"lon":%f},"current":{"temp_c":25.0,"humidity":55,"last_updated_epoch":`+fmt.Sprint(time.Now().Unix())+`,"condition":{"text":"Clear","code":1000}}}`, lat, lon)
	}))
	defer secondary.Close()

	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("synthetic-key", primary.URL))
	reg.Register(weatherapi.NewAdapter("synthetic-key", secondary.URL))
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	eng.SetLeanRanking(true)
	rules, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{
		{ProviderID: "openweather", Enabled: true, ReviewStatus: "operator_approved", Products: []string{providerpolicy.ProductCurrentWeather}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "boxes", Boxes: []providerpolicy.Box{{MinLatitude: 20, MaxLatitude: 23, MinLongitude: 80, MaxLongitude: 84}}}},
		{ProviderID: "weatherapi", Enabled: true, ReviewStatus: "operator_approved", Products: []string{providerpolicy.ProductCurrentWeather}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	eng.SetPolicyCatalog(rules, "noncommercial")
	handler := capability.NewWeatherHandler(reg, eng, normalization.NewWeatherNormalizer(), nil, nil, nil)
	request := func(query string) (int, api.Meta, weatherv1.WeatherResponse) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.GetWeather(rec, httptest.NewRequest(http.MethodGet, "/v1/weather?"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", query, rec.Code, rec.Body.String())
		}
		var envelope struct {
			Data weatherv1.WeatherResponse `json:"data"`
			Meta api.Meta                  `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.SchemaVersion != "weather.v1" || envelope.Data.Temperature.Unit != "celsius" || envelope.Data.ObservedAt.IsZero() || envelope.Data.Location.Latitude == 0 {
			t.Fatalf("invalid canonical response: %+v", envelope.Data)
		}
		return rec.Code, envelope.Meta, envelope.Data
	}

	_, meta, sourceA := request("lat=21.2&lon=82.1")
	if meta.Provider != "openweather" || meta.Fallback || primaryCalls.Load() != 1 || secondaryCalls.Load() != 0 {
		t.Fatalf("location-eligible primary selection failed: %+v primary=%d secondary=%d", meta, primaryCalls.Load(), secondaryCalls.Load())
	}
	primaryMode.Store(1)
	_, meta, sourceB := request("lat=21.3&lon=82.2")
	if meta.Provider != "weatherapi" || !meta.Fallback || secondaryCalls.Load() != 1 {
		t.Fatalf("malformed primary must fail over and disclose source: %+v, secondary=%d", meta, secondaryCalls.Load())
	}
	if sourceA.SchemaVersion != sourceB.SchemaVersion || sourceA.Temperature.Unit != sourceB.Temperature.Unit || sourceA.Humidity.Unit != sourceB.Humidity.Unit {
		t.Fatalf("provider changed canonical contract: primary=%+v fallback=%+v", sourceA, sourceB)
	}
	primaryMode.Store(2)
	_, meta, _ = request("lat=21.4&lon=82.3")
	if meta.Provider != "weatherapi" || !meta.Fallback || secondaryCalls.Load() != 2 {
		t.Fatalf("unavailable primary must fail over: %+v secondary=%d", meta, secondaryCalls.Load())
	}
	before := primaryCalls.Load()
	_, meta, _ = request("lat=30.0&lon=77.0")
	if meta.Provider != "weatherapi" || meta.Fallback || primaryCalls.Load() != before {
		t.Fatalf("geographically ineligible provider was attempted: %+v primary=%d/%d", meta, primaryCalls.Load(), before)
	}
}
