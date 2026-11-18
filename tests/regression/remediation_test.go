package regression

import (
	"context"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/metnorway"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvalidCurrentPrimaryFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
		epoch    int64
	}{{"ancient", 21.2, 81.6, 946684801}, {"wrong-continent", 51.5, -.12, time.Now().Unix()}} {
		t.Run(tc.name, func(t *testing.T) {
			var c1, c2 atomic.Int64
			s1 := server(t, ow(tc.lat, tc.lon, tc.epoch), 200, "", &c1)
			s2 := server(t, wa(), 200, "", &c2)
			reg := provider.NewRegistry()
			reg.Register(openweather.NewAdapter("fixture", s1.URL))
			reg.Register(weatherapi.NewAdapter("fixture", s2.URL))
			status, e, b := request(t, handler(t, reg, nil))
			if status != 200 || e.Meta.Provider != "weatherapi" || !e.Meta.Fallback || c2.Load() != 1 {
				t.Fatalf("invalid primary stopped fallback: %s", b)
			}
		})
	}
}
func TestOriginCacheRestrictions(t *testing.T) {
	for _, directive := range []string{"no-store", "private", "no-cache", "max-age=0"} {
		t.Run(directive, func(t *testing.T) {
			var c atomic.Int64
			s := server(t, ow(21.2, 81.6, time.Now().Unix()), 200, directive, &c)
			reg := provider.NewRegistry()
			reg.Register(openweather.NewAdapter("fixture", s.URL))
			cs := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
			defer cs.Close()
			h := handler(t, reg, cs)
			request(t, h)
			status, e, b := request(t, h)
			if status != 200 || e.Meta.Cached || c.Load() != 2 {
				t.Fatalf("origin restriction ignored: calls=%d body=%s", c.Load(), b)
			}
		})
	}
}
func TestOriginDeadlineCapsStaleRetention(t *testing.T) {
	var c atomic.Int64
	s := server(t, ow(21.2, 81.6, time.Now().Unix()), 200, "max-age=60", &c)
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fixture", s.URL))
	cs := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	defer cs.Close()
	start := time.Now()
	request(t, handler(t, reg, cs))
	lat, lon := 21.2, 81.6
	k := cache.BuildWeatherCacheKey(cache.WeatherKeyParams{Latitude: &lat, Longitude: &lon, PolicyMode: string(decision.PolicyAvailabilityFirst)})
	entry, e := cs.GetEntry(context.Background(), k)
	if e != nil || entry == nil || entry.State != cache.StateFresh || entry.StaleUntil.After(start.Add(61*time.Second)) {
		t.Fatalf("origin ceiling ignored: %+v %v", entry, e)
	}
}
func TestRemovedProviderCannotSurviveWarmCache(t *testing.T) {
	var c1, c2 atomic.Int64
	s1 := server(t, ow(21.2, 81.6, time.Now().Unix()), 200, "", &c1)
	s2 := server(t, wa(), 200, "", &c2)
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fixture", s1.URL))
	cs := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	defer cs.Close()
	request(t, handler(t, reg, cs))
	reg2 := provider.NewRegistry()
	reg2.Register(weatherapi.NewAdapter("fixture", s2.URL))
	status, e, b := request(t, handler(t, reg2, cs))
	if status != 200 || e.Meta.Provider != "weatherapi" || e.Meta.Cached || c2.Load() != 1 {
		t.Fatalf("removed provider replayed: %s", b)
	}
}
func TestCorruptCacheRecomputesInsteadOfReturningInvalid200(t *testing.T) {
	var c atomic.Int64
	s := server(t, ow(21.2, 81.6, time.Now().Unix()), 200, "", &c)
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fixture", s.URL))
	cs := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	defer cs.Close()
	lat, lon := 21.2, 81.6
	k := cache.BuildWeatherCacheKey(cache.WeatherKeyParams{Latitude: &lat, Longitude: &lon, PolicyMode: string(decision.PolicyAvailabilityFirst)})
	for _, payload := range []string{`{"integrity_version":1,"provider_id":"openweather","data":{"schema_version":"fake.v99","location":{"latitude":91},"temperature":{"value":999,"unit":"fahrenheit"}}}`, fmt.Sprintf(`{"integrity_version":1,"provider_id":"openweather","origin_valid_until":"2000-01-01T00:00:00Z","data":{"schema_version":"weather.v1","location":{"latitude":21.2,"longitude":81.6},"temperature":{"value":0,"unit":"celsius"},"condition":"clear","observed_at":%q}}`, time.Now().UTC().Format(time.RFC3339))} {
		if e := cs.Set(context.Background(), k, []byte(payload), time.Minute); e != nil {
			t.Fatal(e)
		}
		before := c.Load()
		status, e, b := request(t, handler(t, reg, cs))
		if status != 200 || e.Meta.Cached || c.Load() != before+1 {
			t.Fatalf("corrupt/expired origin entry replayed: %s", b)
		}
	}
}
func TestLocationIQStatusAndFieldFidelity(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var c atomic.Int64
			s := server(t, `{}`, status, "", &c)
			a := locationiq.NewAdapter("fixture", s.URL)
			want := provider.ErrProviderUnavailable
			switch status {
			case 400, 404:
				want = provider.ErrProviderNotFound
			case 401, 403:
				want = provider.ErrProviderDisabled
			case 429:
				want = provider.ErrRateLimited
			}
			_, e := a.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Raipur"})
			if !errors.Is(e, want) {
				t.Fatalf("forward classification:%v want %v", e, want)
			}
			_, e = a.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 21.2, Longitude: 81.6})
			if !errors.Is(e, want) {
				t.Fatalf("reverse classification:%v want %v", e, want)
			}
		})
	}
	for _, mode := range []string{"forward", "reverse"} {
		t.Run(mode, func(t *testing.T) {
			var c atomic.Int64
			body := `{"lat":"21.2","lon":"81.6","display_name":"Raipur","address":{"county":"Raipur District","suburb":"Shankar Nagar"}}`
			if mode == "forward" {
				body = "[" + body + "]"
			}
			s := server(t, body, 200, "", &c)
			a := locationiq.NewAdapter("fixture", s.URL)
			var raw *provider.RawGeocodeResponse
			var e error
			if mode == "forward" {
				raw, e = a.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Raipur"})
			} else {
				raw, e = a.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{})
			}
			if e != nil {
				t.Fatal(e)
			}
			v, e := normalization.NewGeocodingNormalizer().Normalize(raw)
			if e != nil || v.Results[0].District != "Raipur District" || v.Results[0].Neighborhood != "Shankar Nagar" {
				t.Fatalf("address field loss:%+v %v", v, e)
			}
		})
	}
}
func TestMETInvalidResponseDoesNotDeliver(t *testing.T) {
	for _, tc := range []struct {
		name, units string
		lat, lon    float64
	}{{"wrong-place", `{"air_temperature":"celsius"}`, 51.5, -.12}, {"wrong-unit", `{"air_temperature":"fahrenheit"}`, 21.2, 81.6}} {
		t.Run(tc.name, func(t *testing.T) {
			var c atomic.Int64
			body := fmt.Sprintf(`{"geometry":{"coordinates":[%f,%f]},"properties":{"meta":{"units":%s},"timeseries":[{"time":%q,"data":{"instant":{"details":{"air_temperature":32}}}}]}}`, tc.lon, tc.lat, tc.units, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
			s := server(t, body, 200, "", &c)
			reg := provider.NewRegistry()
			reg.Register(metnorway.NewAdapter("audit@example.invalid", s.URL).WithHTTPClient(client.NewTestHTTPClient(time.Second)))
			eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
			doc := providerpolicy.DefaultDocument()
			for i := range doc.Rules {
				if doc.Rules[i].ProviderID == "met_norway" {
					doc.Rules[i].Enabled = true
				}
			}
			cat, e := providerpolicy.NewCatalog(doc)
			if e != nil {
				t.Fatal(e)
			}
			eng.SetPolicyCatalog(cat, "noncommercial")
			h := capability.NewForecastHandler(reg, eng, nil)
			w := httptest.NewRecorder()
			h.GetForecast(w, httptest.NewRequest("GET", "/v1/forecast?lat=21.2&lon=81.6", nil))
			if w.Code != 502 {
				t.Fatalf("invalid forecast accepted:%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestWeatherAPIErrorTaxonomy(t *testing.T) {
	for _, tc := range []struct {
		status, code int
		want         error
	}{{400, 1006, provider.ErrProviderNotFound}, {400, 9999, provider.ErrProviderUnavailable}, {403, 2007, provider.ErrRateLimited}, {403, 2008, provider.ErrProviderDisabled}, {401, 2006, provider.ErrProviderDisabled}} {
		var c atomic.Int64
		s := server(t, fmt.Sprintf(`{"error":{"code":%d}}`, tc.code), tc.status, "", &c)
		_, e := weatherapi.NewAdapter("fixture", s.URL).FetchWeather(context.Background(), "Raipur")
		if !errors.Is(e, tc.want) {
			t.Fatalf("%d/%d=%v", tc.status, tc.code, e)
		}
	}
}
func TestProbeIsolationDoesNotBypassCapabilityAdmission(t *testing.T) {
	rl := middleware.NewRateLimiter(.001, 1)
	probe := health.NewHandler("test")
	h := rl.ClientAdmission(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/liveness" {
			probe.Liveness(w, r)
		} else {
			w.WriteHeader(401)
		}
	}))
	for i, path := range []string{"/v1/weather", "/v1/weather", "/liveness", "/v1/weather"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		want := 429
		if i == 0 {
			want = 401
		}
		if path == "/liveness" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s=%d want%d", path, w.Code, want)
		}
	}
}
