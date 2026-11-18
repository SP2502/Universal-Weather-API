package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openmeteo"
)

type forecastEnvelope struct {
	Data struct {
		Points []json.RawMessage `json:"points"`
	} `json:"data"`
	Meta struct {
		Cached   bool   `json:"cached"`
		Provider string `json:"provider"`
	} `json:"meta"`
}

func testForecastHandler(t *testing.T, upstream *httptest.Server) (*capability.ForecastHandler, *cache.PolicyEngine) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(openmeteo.NewAdapter(upstream.URL).WithHTTPClient(upstream.Client()))
	doc := providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{{ProviderID: "open_meteo_forecast", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"hourly_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}}}}
	catalog, err := providerpolicy.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	engine.SetPolicyCatalog(catalog, "noncommercial")
	handler := capability.NewForecastHandler(reg, engine, nil)
	policy := cache.NewPolicyEngine()
	store := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	t.Cleanup(func() { _ = store.Close() })
	handler.ConfigureCache(store, policy)
	return handler, policy
}
func testForecastJSON() map[string]any {
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Hour)
	var times []int64
	var temp []float64
	var humidity []int
	var codes []int
	for i := 0; i < 72; i++ {
		times = append(times, base.Add(time.Duration(i)*time.Hour).Unix())
		temp = append(temp, 21)
		humidity = append(humidity, 55)
		codes = append(codes, 2)
	}
	return map[string]any{"latitude": 21.25, "longitude": 81.63, "hourly": map[string]any{"time": times, "temperature_2m": temp, "relative_humidity_2m": humidity, "weather_code": codes}}
}
func runForecastRequest(t *testing.T, h *capability.ForecastHandler, query string) (int, forecastEnvelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetForecast(rec, httptest.NewRequest("GET", "/v1/forecast?"+query, nil))
	var env forecastEnvelope
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, env
}
func TestPhase07B_ForecastCacheFullPayloadAndRequestConstraints(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=120")
		_ = json.NewEncoder(w).Encode(testForecastJSON())
	}))
	defer upstream.Close()
	h, policy := testForecastHandler(t, upstream)
	query := "lat=21.25&lon=81.63&points=5"
	if code, x := runForecastRequest(t, h, query); code != 200 || len(x.Data.Points) != 5 || x.Meta.Cached {
		t.Fatalf("first %+v code=%d", x, code)
	}
	if code, x := runForecastRequest(t, h, "lat=21.25&lon=81.63&points=50"); code != 200 || len(x.Data.Points) != 50 || !x.Meta.Cached {
		t.Fatalf("shared full forecast %+v code=%d", x, code)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call, got %d", calls.Load())
	}
	if code, x := runForecastRequest(t, h, "lat=21.25&lon=81.63&provider=open_meteo_forecast"); code != 200 || x.Meta.Cached {
		t.Fatalf("provider-specific identity code=%d %+v", code, x)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider constraint failed to isolate cache: %d", calls.Load())
	}
	// A provider cache prohibition must invalidate an otherwise warm hit.
	policy.SetProviderDisabled("forecast", "open_meteo_forecast", true)
	if code, x := runForecastRequest(t, h, query); code != 200 || x.Meta.Cached {
		t.Fatalf("provider cache restriction bypassed %d %+v", code, x)
	}
	if calls.Load() != 3 {
		t.Fatalf("disabled cache reused upstream response, calls=%d", calls.Load())
	}
}
func TestPhase07B_NoStoreOriginAndConcurrentSingleFlight(t *testing.T) {
	var calls atomic.Int32
	noStore := atomic.Bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if noStore.Load() {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "max-age=90")
		}
		time.Sleep(45 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(testForecastJSON())
	}))
	defer upstream.Close()
	h, _ := testForecastHandler(t, upstream)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.GetForecast(rec, httptest.NewRequest("GET", "/v1/forecast?lat=21.25&lon=81.63", nil))
			if rec.Code != 200 {
				errs <- fmt.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("singleflight performed %d calls", calls.Load())
	}
	noStore.Store(true)
	// Different coordinates create a new cache entry. no-store must prevent
	// even back-to-back serialized reads from consulting a saved response.
	for i := 0; i < 2; i++ {
		if code, _ := runForecastRequest(t, h, "lat=21.2501&lon=81.63"); code != 200 {
			t.Fatalf("failed status %d", code)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("origin no-store ignored, calls=%d", calls.Load())
	}
}
