package integration

import (
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
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

var validOW = `{"coord":{"lon":-0.1278,"lat":51.5074},"weather":[{"id":800,"main":"Clear"}],"main":{"temp":295.65,"humidity":60},"name":"London","dt":` + fmt.Sprint(time.Now().Unix()) + `}`
var validWAPI = `{"location":{"name":"London","country":"UK","lat":51.5074,"lon":-0.1278},"current":{"temp_c":22.5,"humidity":60,"condition":{"text":"Sunny","code":1000},"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`

func phase03Weather(t *testing.T, primary, secondary http.HandlerFunc, withCache bool) (*capability.WeatherHandler, *provider.Registry, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	primaryHits, secondaryHits := &atomic.Int64{}, &atomic.Int64{}
	p1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { primaryHits.Add(1); primary(w, r) }))
	p2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secondaryHits.Add(1); secondary(w, r) }))
	t.Cleanup(p1.Close)
	t.Cleanup(p2.Close)
	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("fake-key", p1.URL))
	registry.Register(weatherapi.NewAdapter("fake-key", p2.URL))
	var store cache.Store
	var policy *cache.PolicyEngine
	if withCache {
		store = cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
		policy = cache.NewPolicyEngine()
	}
	return capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), nil, store, policy), registry, primaryHits, secondaryHits
}
func weatherCall(h *capability.WeatherHandler, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/v1/weather?"+query, nil)
	w := httptest.NewRecorder()
	h.GetWeather(w, r)
	return w
}
func decodeWeatherP3(t *testing.T, r *httptest.ResponseRecorder) struct {
	Data weatherv1.WeatherResponse `json:"data"`
	Meta api.Meta                  `json:"meta"`
} {
	t.Helper()
	var out struct {
		Data weatherv1.WeatherResponse `json:"data"`
		Meta api.Meta                  `json:"meta"`
	}
	if r.Code != 200 {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPhase03_WeatherSemanticFallbackAndAttribution(t *testing.T) {
	h, reg, p1, p2 := phase03Weather(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"coord":{"lat":51.5,"lon":-0.1},"main":{},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`))
		},
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(validWAPI)) }, false)
	out := decodeWeatherP3(t, weatherCall(h, "location=London"))
	if out.Meta.Provider != "weatherapi" || !out.Meta.Fallback || out.Data.Temperature.Value != 22.5 {
		t.Fatalf("wrong fallback: %+v", out)
	}
	if len(out.Meta.Attribution) != 1 || out.Meta.Attribution[0].Provider != "weatherapi" {
		t.Fatalf("wrong attribution: %+v", out.Meta)
	}
	if p1.Load() != 1 || p2.Load() != 1 {
		t.Fatalf("expected one call each: primary=%d backup=%d", p1.Load(), p2.Load())
	}
	state := reg.HealthEvaluator().GetHealth("openweather")
	if state.FailureCount == 0 {
		t.Fatal("invalid canonical payload must count as provider failure")
	}
	// Explicit provider selection is a hard constraint; never fall through.
	rec := weatherCall(h, "location=London&provider=openweather")
	if rec.Code != 502 {
		t.Fatalf("expected 502 for invalid pinned provider, got %d", rec.Code)
	}
	if p2.Load() != 1 {
		t.Fatal("explicit provider unexpectedly fell back")
	}
}
func TestPhase03_CachePreservesFallbackAndProviderIsolation(t *testing.T) {
	h, _, p1, p2 := phase03Weather(t,
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(validOW)) },
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(validWAPI)) }, true)
	// A pinned request must never read the unconstrained cached response.
	base := decodeWeatherP3(t, weatherCall(h, "location=London"))
	if base.Meta.Provider != "openweather" {
		t.Fatal(base.Meta.Provider)
	}
	pinned := decodeWeatherP3(t, weatherCall(h, "location=London&provider=weatherapi"))
	if pinned.Meta.Provider != "weatherapi" || pinned.Meta.Fallback {
		t.Fatalf("wrong pinned response: %+v", pinned.Meta)
	}
	if p1.Load() != 1 || p2.Load() != 1 {
		t.Fatalf("expected provider isolation: %d / %d", p1.Load(), p2.Load())
	}
	hit := decodeWeatherP3(t, weatherCall(h, "location=London&provider=weatherapi"))
	if !hit.Meta.Cached || hit.Meta.Provider != "weatherapi" {
		t.Fatalf("wrong pinned cache hit: %+v", hit.Meta)
	}
}
func TestPhase03_GeocodingInvalidPrimaryFallback(t *testing.T) {
	first, second := &atomic.Int64{}, &atomic.Int64{}
	p1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		w.Write([]byte(`[{"lat":"bad","lon":"0","display_name":"Invalid location"}]`))
	}))
	p2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		w.Write([]byte(`[{"lat":"51.5074","lon":"-0.1278","display_name":"London","class":"place","type":"city"}]`))
	}))
	defer p1.Close()
	defer p2.Close()
	registry := provider.NewRegistry()
	registry.Register(nominatim.NewAdapter(p1.URL))
	registry.Register(locationiq.NewAdapter("test", p2.URL))
	h := capability.NewGeocodingHandler(registry, nil, normalization.NewGeocodingNormalizer(), nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/geocode?q=London", nil)
	w := httptest.NewRecorder()
	h.Geocode(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Data locv1.GeocodeResponse `json:"data"`
		Meta api.Meta              `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Meta.Provider != "locationiq" || !out.Meta.Fallback || len(out.Data.Results) != 1 {
		t.Fatalf("incorrect geocode fallback: %+v", out)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatalf("unexpected calls %d/%d", first.Load(), second.Load())
	}
}
func TestPhase03_RecoveryEvenWithHealthySecondary(t *testing.T) {
	var available atomic.Bool
	h, reg, p1, p2 := phase03Weather(t,
		func(w http.ResponseWriter, r *http.Request) {
			if !available.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(validOW))
		},
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(validWAPI)) }, false)
	// Pin five consecutive failures to force a circuit OPEN without involving secondary.
	for i := 0; i < 5; i++ {
		rec := weatherCall(h, "location=London&provider=openweather")
		if rec.Code != 502 {
			t.Fatalf("failure %d returned %d", i, rec.Code)
		}
	}
	state := reg.HealthEvaluator().GetHealth("openweather")
	if state.CircuitState != "OPEN" || state.NextProbeAt.IsZero() {
		t.Fatalf("breaker did not open: %+v", state)
	}
	available.Store(true)
	// During cooldown the healthy secondary should be used.
	out := decodeWeatherP3(t, weatherCall(h, "location=London"))
	if out.Meta.Provider != "weatherapi" {
		t.Fatalf("should use healthy backup during cooldown: %+v", out.Meta)
	}
	// Wait until the actual recorded recovery deadline (plus a tiny scheduler margin).
	if d := time.Until(state.NextProbeAt.Add(30 * time.Millisecond)); d > 0 {
		time.Sleep(d)
	}
	out = decodeWeatherP3(t, weatherCall(h, "location=London"))
	if out.Meta.Provider != "openweather" || out.Meta.Fallback {
		t.Fatalf("recovery probe did not win despite healthy secondary: %+v", out.Meta)
	}
	// Two successful half-open probes are required to close by default.
	out = decodeWeatherP3(t, weatherCall(h, "location=London"))
	if out.Meta.Provider != "openweather" || reg.HealthEvaluator().GetHealth("openweather").CircuitState != "CLOSED" {
		t.Fatalf("circuit did not reclose: %+v", reg.HealthEvaluator().GetHealth("openweather"))
	}
	if p1.Load() < 7 || p2.Load() == 0 {
		t.Fatal("expected recovered primary and healthy backup traffic")
	}
}
