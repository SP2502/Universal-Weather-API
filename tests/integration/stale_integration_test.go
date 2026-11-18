package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/openweather"
)

func setupStaleTestGateway(upstreamLatency time.Duration, upstreamFails *atomic.Bool) (http.Handler, cache.Store, *cache.PolicyEngine, *atomic.Int64, *httptest.Server) {
	callCount := &atomic.Int64{}

	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if upstreamLatency > 0 {
			time.Sleep(upstreamLatency)
		}

		if upstreamFails != nil && upstreamFails.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"upstream down"}`))
			return
		}

		loc := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": 13.4050, "lat": 52.5200},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 298.15, "humidity": 50},
			"name": "` + loc + `",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))

	store := database.NewMemoryStore()
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-tenant-1",
		Name:      "Tenant 1 Key",
		KeyPrefix: "uap_t1_",
		KeyHash:   database.HashKey("uap_t1_secret_key"),
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", owServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()
	cacheStore := &controlledStaleStore{Store: cache.NewMemoryStore(cache.DefaultMemoryStoreOptions()), writes: make(chan struct{}, 16)}
	cachePolicy := cache.NewPolicyEngine()

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	tracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, tracker, cacheStore, cachePolicy)
	healthHandler := health.NewHandler("0.1.0")

	tracer := tracing.DefaultTracer
	pMetrics := metrics.DefaultMetrics

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:  healthHandler,
		WeatherHandler: weatherHandler,
		AuthMiddleware: authMid,
	})

	logger := logging.NewLogger("ERROR", io.Discard)
	var handler http.Handler = appRouter
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Telemetry(tracer, pMetrics)(handler)
	handler = middleware.Recovery(logger)(handler)

	return handler, cacheStore, cachePolicy, callCount, owServer
}

func TestSingleFlight_ConcurrentCacheMisses_SingleUpstreamExecution(t *testing.T) {
	// Upstream has 40ms latency so concurrent requests overlap
	handler, cStore, _, callCount, owServer := setupStaleTestGateway(40*time.Millisecond, nil)
	defer owServer.Close()
	defer cStore.Close()

	concurrency := 20
	var wg sync.WaitGroup
	statusCodes := make([]int, concurrency)
	cachedFlags := make([]bool, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Berlin", nil)
			req.Header.Set("X-API-Key", "uap_t1_secret_key")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			statusCodes[idx] = rec.Code
			var env CanonicalWeatherEnvelope
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
			cachedFlags[idx] = env.Meta.Cached
		}(i)
	}

	wg.Wait()

	// Exactly 1 upstream call occurred due to Single-Flight request coalescing!
	if callCount.Load() != 1 {
		t.Fatalf("expected exactly 1 upstream call across %d concurrent requests, got %d", concurrency, callCount.Load())
	}

	for i := 0; i < concurrency; i++ {
		if statusCodes[i] != http.StatusOK {
			t.Errorf("request %d returned non-200 status: %d", i, statusCodes[i])
		}
	}
}

func TestStaleWhileRevalidate_ReturnsStaleAndAsyncRefreshes(t *testing.T) {
	handler, cStore, policies, calls, upstream := setupStaleTestGateway(0, nil)
	defer upstream.Close()
	defer cStore.Close()
	policies.RegisterPolicy(&cache.CapabilityPolicy{Capability: "weather", Type: cache.ConditionallyCacheable, DefaultTTL: time.Hour, MaxTTL: time.Hour, StaleWhileRevalidateWindow: 5 * time.Minute, StaleIfErrorWindow: 30 * time.Minute})
	controlled := cStore.(*controlledStaleStore)
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/weather?location=Tokyo", nil)
		r.Header.Set("X-API-Key", "uap_t1_secret_key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request(); w.Code != 200 || calls.Load() != 1 {
		t.Fatalf("warmup: status=%d calls=%d", w.Code, calls.Load())
	}
	<-controlled.writes
	controlled.stale.Store(true)
	w := request()
	var env CanonicalWeatherEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if w.Code != 200 || !env.Meta.Cached || w.Header().Get("Warning") == "" {
		t.Fatalf("SWR did not return retained response: %s", w.Body.String())
	}
	select {
	case <-controlled.writes:
	case <-time.After(2 * time.Second):
		t.Fatal("background refresh did not complete")
	}
	if w = request(); w.Code != 200 || calls.Load() != 2 || w.Header().Get("Warning") != "" {
		t.Fatalf("refreshed entry not fresh: status=%d calls=%d", w.Code, calls.Load())
	}
}

// Force only the read state, while the real store still owns persistence.
// A successful refresh resets the state and notifies the waiting test.
type controlledStaleStore struct {
	cache.Store
	stale  atomic.Bool
	writes chan struct{}
}

func (s *controlledStaleStore) GetEntry(ctx context.Context, key string) (*cache.Entry, error) {
	entry, err := s.Store.GetEntry(ctx, key)
	if err == nil && entry != nil && s.stale.Load() && entry.State == cache.StateFresh {
		entry.State = cache.StateStale
		entry.FreshUntil = time.Now().Add(-time.Second)
	}
	return entry, err
}
func (s *controlledStaleStore) SetWithStale(ctx context.Context, key string, value []byte, fresh, stale time.Duration) error {
	if err := s.Store.SetWithStale(ctx, key, value, fresh, stale); err != nil {
		return err
	}
	s.stale.Store(false)
	select {
	case s.writes <- struct{}{}:
	default:
	}
	return nil
}

func TestStaleWhileRevalidate_ConcurrentCallers_SingleRefreshExecution(t *testing.T) {
	handler, cStore, policyEngine, callCount, owServer := setupStaleTestGateway(20*time.Millisecond, nil)
	defer owServer.Close()
	defer cStore.Close()

	policyEngine.RegisterPolicy(&cache.CapabilityPolicy{
		Capability:                 "weather",
		Type:                       cache.ConditionallyCacheable,
		DefaultTTL:                 30 * time.Millisecond,
		MaxTTL:                     1 * time.Minute,
		StaleWhileRevalidateWindow: 300 * time.Millisecond,
		StaleIfErrorWindow:         500 * time.Millisecond,
		DisabledProviders:          make(map[string]bool),
	})

	// Warm cache
	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Rome", nil)
	req.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Wait for entry to become stale
	time.Sleep(45 * time.Millisecond)

	initialCalls := callCount.Load()

	// 10 concurrent requests arrive for stale entry
	concurrency := 10
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Rome", nil)
			r.Header.Set("X-API-Key", "uap_t1_secret_key")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, r)
		}()
	}

	wg.Wait()

	// Wait for background refresh to finish
	time.Sleep(60 * time.Millisecond)

	// Verify that only 1 background refresh was triggered across all 10 concurrent stale callers!
	refreshes := callCount.Load() - initialCalls
	if refreshes != 1 {
		t.Fatalf("expected exactly 1 background refresh across %d concurrent callers, got %d", concurrency, refreshes)
	}
}

func TestStaleIfError_ProviderOutage_ServesStaleFallback(t *testing.T) {
	upstreamFails := &atomic.Bool{}
	handler, cStore, policyEngine, callCount, owServer := setupStaleTestGateway(0, upstreamFails)
	defer owServer.Close()
	defer cStore.Close()

	policyEngine.RegisterPolicy(&cache.CapabilityPolicy{
		Capability:                 "weather",
		Type:                       cache.ConditionallyCacheable,
		DefaultTTL:                 30 * time.Millisecond,
		MaxTTL:                     1 * time.Minute,
		StaleWhileRevalidateWindow: 0, // Disable SWR to test Stale-If-Error fallback on provider failure
		StaleIfErrorWindow:         500 * time.Millisecond,
		DisabledProviders:          make(map[string]bool),
	})

	// 1. Initial request -> Cache populated
	req1 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Sydney", nil)
	req1.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("failed initial request: %d", rec1.Code)
	}

	// 2. Wait for entry to become stale
	time.Sleep(40 * time.Millisecond)

	// 3. Upstream provider fails!
	upstreamFails.Store(true)

	// 4. Request arrives during outage -> Stale-If-Error serves cached response with fallback=true
	req2 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Sydney", nil)
	req2.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected Stale-If-Error to return 200 OK during outage, got %d", rec2.Code)
	}

	var env2 CanonicalWeatherEnvelope
	_ = json.Unmarshal(rec2.Body.Bytes(), &env2)
	if !env2.Meta.Cached || !env2.Meta.Fallback {
		t.Errorf("expected cached=true and fallback=true under Stale-If-Error, got cached=%v, fallback=%v", env2.Meta.Cached, env2.Meta.Fallback)
	}

	if rec2.Header().Get("Warning") == "" {
		t.Errorf("expected Warning header on Stale-If-Error fallback")
	}

	// 5. Upstream recovers -> Next request succeeds and refreshes
	upstreamFails.Store(false)
	time.Sleep(50 * time.Millisecond)

	req3 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Sydney", nil)
	req3.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected recovered 200 OK, got %d", rec3.Code)
	}

	if callCount.Load() < 2 {
		t.Errorf("expected at least 2 upstream attempts, got %d", callCount.Load())
	}
}

func TestStaleIfError_ExpiredData_ReturnsError(t *testing.T) {
	upstreamFails := &atomic.Bool{}
	upstreamFails.Store(true) // Upstream fails immediately

	handler, cStore, policyEngine, _, owServer := setupStaleTestGateway(0, upstreamFails)
	defer owServer.Close()
	defer cStore.Close()

	policyEngine.RegisterPolicy(&cache.CapabilityPolicy{
		Capability:                 "weather",
		Type:                       cache.ConditionallyCacheable,
		DefaultTTL:                 20 * time.Millisecond,
		MaxTTL:                     1 * time.Minute,
		StaleWhileRevalidateWindow: 20 * time.Millisecond,
		StaleIfErrorWindow:         40 * time.Millisecond,
		DisabledProviders:          make(map[string]bool),
	})

	// No cache entry exists -> Provider fails -> 502 UPSTREAM_ERROR
	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Oslo", nil)
	req.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway when no stale data exists, got %d", rec.Code)
	}
}
