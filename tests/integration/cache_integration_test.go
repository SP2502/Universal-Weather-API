package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/api"
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
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

type CanonicalWeatherEnvelope struct {
	Data weatherv1.WeatherResponse `json:"data"`
	Meta api.Meta                  `json:"meta"`
}

func setupCacheTestGateway(customStore cache.Store, customPolicy *cache.PolicyEngine) (http.Handler, *database.MemoryStore, *atomic.Int64, *httptest.Server) {
	callCount := &atomic.Int64{}

	owServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		loc := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1278, "lat": 51.5074},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 295.65, "humidity": 60},
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
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-tenant-2",
		Name:      "Tenant 2 Key",
		KeyPrefix: "uap_t2_",
		KeyHash:   database.HashKey("uap_t2_secret_key"),
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", owServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()

	if customStore == nil {
		customStore = cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	}
	if customPolicy == nil {
		customPolicy = cache.NewPolicyEngine()
	}

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	tracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, tracker, customStore, customPolicy)
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

	return handler, store, callCount, owServer
}

func TestCache_Weather_HitMissAndUpstreamCallCount(t *testing.T) {
	handler, _, callCount, owServer := setupCacheTestGateway(nil, nil)
	defer owServer.Close()

	// 1. First Request -> MISS -> Upstream called
	req1 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req1.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec1.Code, rec1.Body.String())
	}

	var env1 CanonicalWeatherEnvelope
	if err := json.Unmarshal(rec1.Body.Bytes(), &env1); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env1.Meta.Cached {
		t.Errorf("expected first request to have cached=false, got true")
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 upstream call on cache miss, got %d", callCount.Load())
	}

	// 2. Second Request (equivalent with different casing/whitespace) -> HIT -> No upstream call!
	req2 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=%20%20london%20%20", nil)
	req2.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}

	var env2 CanonicalWeatherEnvelope
	if err := json.Unmarshal(rec2.Body.Bytes(), &env2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !env2.Meta.Cached {
		t.Errorf("expected second request to have cached=true, got false")
	}
	if callCount.Load() != 1 {
		t.Errorf("expected still 1 upstream call after cache hit, got %d", callCount.Load())
	}
	if env2.Data.Temperature.Value != env1.Data.Temperature.Value {
		t.Errorf("expected matching cached temperature, got %f vs %f", env2.Data.Temperature.Value, env1.Data.Temperature.Value)
	}

	// 3. Third Request (different city) -> MISS -> 2nd upstream call
	req3 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=Paris", nil)
	req3.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec3.Code)
	}

	var env3 CanonicalWeatherEnvelope
	if err := json.Unmarshal(rec3.Body.Bytes(), &env3); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env3.Meta.Cached {
		t.Errorf("expected Paris request to have cached=false, got true")
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 upstream calls after Paris miss, got %d", callCount.Load())
	}
}

// faultyCacheStore simulates a broken cache backend to test Fail-Open resilience
type faultyCacheStore struct {
	failGet bool
	failSet bool
}

func (f *faultyCacheStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if f.failGet {
		return nil, false, errors.New("simulated cache read timeout/connection error")
	}
	return nil, false, nil
}

func (f *faultyCacheStore) GetEntry(ctx context.Context, key string) (*cache.Entry, error) {
	if f.failGet {
		return nil, errors.New("simulated cache read timeout/connection error")
	}
	return &cache.Entry{State: cache.StateNotFound}, nil
}

func (f *faultyCacheStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if f.failSet {
		return errors.New("simulated cache write error")
	}
	return nil
}

func (f *faultyCacheStore) SetWithStale(ctx context.Context, key string, value []byte, freshTTL, staleTTL time.Duration) error {
	if f.failSet {
		return errors.New("simulated cache write error")
	}
	return nil
}

func (f *faultyCacheStore) Delete(ctx context.Context, key string) error { return nil }
func (f *faultyCacheStore) Len() int                                     { return 0 }
func (f *faultyCacheStore) Close() error                                 { return nil }

func TestCache_FailOpen_GetFailure(t *testing.T) {
	faulty := &faultyCacheStore{failGet: true}
	handler, _, callCount, owServer := setupCacheTestGateway(faulty, nil)
	defer owServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Must succeed despite cache error (fail-open)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open to return 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if callCount.Load() != 1 {
		t.Errorf("expected upstream provider execution on cache GET failure")
	}
}

func TestCache_FailOpen_SetFailure(t *testing.T) {
	faulty := &faultyCacheStore{failSet: true}
	handler, _, callCount, owServer := setupCacheTestGateway(faulty, nil)
	defer owServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", "uap_t1_secret_key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Must succeed and return canonical data despite cache write error (fail-open)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open to return 200 OK on SET failure, got %d", rec.Code)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected upstream provider execution, got %d", callCount.Load())
	}
}

func TestCache_TenantIsolation_NoDataLeakage(t *testing.T) {
	handler, store, _, owServer := setupCacheTestGateway(nil, nil)
	defer owServer.Close()

	// 1. Tenant 1 populates cache for London
	req1 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req1.Header.Set("X-API-Key", "uap_t1_secret_key")
	req1.Header.Set("X-Request-ID", "req-tenant1-uuid")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	var env1 CanonicalWeatherEnvelope
	_ = json.Unmarshal(rec1.Body.Bytes(), &env1)
	if env1.Meta.RequestID != "req-tenant1-uuid" {
		t.Errorf("expected tenant 1 request ID, got %s", env1.Meta.RequestID)
	}

	// 2. Tenant 2 requests London -> Cache HIT
	req2 := httptest.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req2.Header.Set("X-API-Key", "uap_t2_secret_key")
	req2.Header.Set("X-Request-ID", "req-tenant2-uuid")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	var env2 CanonicalWeatherEnvelope
	_ = json.Unmarshal(rec2.Body.Bytes(), &env2)

	// Verify Tenant 2 gets cached weather data, but with Tenant 2's own RequestID
	if !env2.Meta.Cached {
		t.Errorf("expected tenant 2 to receive cached weather")
	}
	if env2.Meta.RequestID != "req-tenant2-uuid" {
		t.Errorf("expected tenant 2 request ID in response meta, got %s", env2.Meta.RequestID)
	}

	// 3. Verify usage records: exactly 1 for tenant 1 and 1 for tenant 2
	time.Sleep(50 * time.Millisecond)
	if store.GetUsageCount() != 2 {
		t.Errorf("expected 2 usage records recorded, got %d", store.GetUsageCount())
	}
}
