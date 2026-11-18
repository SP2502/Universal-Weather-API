package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

type BenchmarkEnvironment struct {
	Handler            http.Handler
	ValidAPIKey        string
	WeatherUpstream    *httptest.Server
	GeocodingUpstream  *httptest.Server
	WeatherCallCount   *atomic.Int64
	GeocodingCallCount *atomic.Int64
}

func SetupBenchmarkEnvironment() *BenchmarkEnvironment {
	weatherCalls := &atomic.Int64{}
	geocodingCalls := &atomic.Int64{}

	weatherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		weatherCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1278, "lat": 51.5074},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 295.65, "humidity": 60},
			"name": "London",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))

	geocodingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geocodingCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{
			"place_id": 1001,
			"lat": "51.5074",
			"lon": "-0.1278",
			"display_name": "London, Greater London, England, SW1A 2AA, United Kingdom",
			"class": "place",
			"type": "city",
			"importance": 0.95,
			"address": {
				"city": "London",
				"country": "United Kingdom",
				"country_code": "gb"
			}
		}]`))
	}))

	store := database.NewMemoryStore()
	validRawKey := "uap_bench_secret_key_123"

	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-bench-1",
		Name:      "Benchmark Test Key",
		KeyPrefix: "uap_bench_",
		KeyHash:   database.HashKey(validRawKey),
		Scopes:    []string{"weather:read", "geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", weatherServer.URL))
	registry.Register(weatherapi.NewAdapter("dummy-key", weatherServer.URL))
	registry.Register(nominatim.NewAdapter(geocodingServer.URL))
	registry.Register(locationiq.NewAdapter("dummy-key", geocodingServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()
	geocodingNormalizer := normalization.NewGeocodingNormalizer()

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	tracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, tracker, nil, nil)
	geocodingHandler := capability.NewGeocodingHandler(registry, nil, geocodingNormalizer, tracker)
	healthHandler := health.NewHandler("0.1.0")

	tracer := tracing.DefaultTracer
	pMetrics := metrics.DefaultMetrics

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:    healthHandler,
		MetricsHandler:   metrics.Handler(pMetrics.Registry),
		WeatherHandler:   weatherHandler,
		GeocodingHandler: geocodingHandler,
		AuthMiddleware:   authMid,
	})

	logger := logging.NewLogger("ERROR", io.Discard)
	var handler http.Handler = appRouter
	handler = middleware.MaxBodySize(64 * 1024)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Telemetry(tracer, pMetrics)(handler)
	handler = middleware.Recovery(logger)(handler)

	return &BenchmarkEnvironment{
		Handler:            handler,
		ValidAPIKey:        validRawKey,
		WeatherUpstream:    weatherServer,
		GeocodingUpstream:  geocodingServer,
		WeatherCallCount:   weatherCalls,
		GeocodingCallCount: geocodingCalls,
	}
}

// BenchmarkBaseline_GatewayWeather_Sequential measures single-threaded baseline latency & throughput without caching
func BenchmarkBaseline_GatewayWeather_Sequential(b *testing.B) {
	env := SetupBenchmarkEnvironment()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()

	req, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", env.ValidAPIKey)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected response code: %d", rec.Code)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(env.WeatherCallCount.Load())/float64(b.N), "upstream_calls/req")
}

// BenchmarkBaseline_GatewayWeather_Parallel measures multi-threaded concurrent throughput without caching
func BenchmarkBaseline_GatewayWeather_Parallel(b *testing.B) {
	env := SetupBenchmarkEnvironment()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
		req.Header.Set("X-API-Key", env.ValidAPIKey)

		for pb.Next() {
			rec := httptest.NewRecorder()
			env.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Errorf("unexpected status: %d", rec.Code)
			}
		}
	})

	b.StopTimer()
	b.ReportMetric(float64(env.WeatherCallCount.Load())/float64(b.N), "upstream_calls/req")
}

// BenchmarkBaseline_GatewayGeocode_Sequential measures geocoding latency & throughput without caching
func BenchmarkBaseline_GatewayGeocode_Sequential(b *testing.B) {
	env := SetupBenchmarkEnvironment()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()

	req, _ := http.NewRequest(http.MethodGet, "/v1/geocode?q=London", nil)
	req.Header.Set("X-API-Key", env.ValidAPIKey)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected response code: %d", rec.Code)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(env.GeocodingCallCount.Load())/float64(b.N), "upstream_calls/req")
}

// BenchmarkBaseline_GatewayGeocode_Parallel measures concurrent geocoding throughput without caching
func BenchmarkBaseline_GatewayGeocode_Parallel(b *testing.B) {
	env := SetupBenchmarkEnvironment()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/geocode?q=London", nil)
		req.Header.Set("X-API-Key", env.ValidAPIKey)

		for pb.Next() {
			rec := httptest.NewRecorder()
			env.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Errorf("unexpected status: %d", rec.Code)
			}
		}
	})

	b.StopTimer()
	b.ReportMetric(float64(env.GeocodingCallCount.Load())/float64(b.N), "upstream_calls/req")
}

// SetupBenchmarkEnvironmentWithCache creates an environment with active in-memory caching
func SetupBenchmarkEnvironmentWithCache() (*BenchmarkEnvironment, cache.Store) {
	weatherCalls := &atomic.Int64{}
	geocodingCalls := &atomic.Int64{}

	weatherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		weatherCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1278, "lat": 51.5074},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 295.65, "humidity": 60},
			"name": "London",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))

	geocodingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geocodingCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{
			"place_id": 1001,
			"lat": "51.5074",
			"lon": "-0.1278",
			"display_name": "London, Greater London, England, SW1A 2AA, United Kingdom",
			"class": "place",
			"type": "city",
			"importance": 0.95,
			"address": {
				"city": "London",
				"country": "United Kingdom",
				"country_code": "gb"
			}
		}]`))
	}))

	store := database.NewMemoryStore()
	validRawKey := "uap_bench_secret_key_123"

	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-bench-1",
		Name:      "Benchmark Test Key",
		KeyPrefix: "uap_bench_",
		KeyHash:   database.HashKey(validRawKey),
		Scopes:    []string{"weather:read", "geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", weatherServer.URL))
	registry.Register(weatherapi.NewAdapter("dummy-key", weatherServer.URL))
	registry.Register(nominatim.NewAdapter(geocodingServer.URL))
	registry.Register(locationiq.NewAdapter("dummy-key", geocodingServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()
	geocodingNormalizer := normalization.NewGeocodingNormalizer()

	cacheStore := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())
	cachePolicy := cache.NewPolicyEngine()

	authenticator := auth.NewAuthenticator(store)
	authMid := auth.Middleware(authenticator, store)
	tracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, tracker, cacheStore, cachePolicy)
	geocodingHandler := capability.NewGeocodingHandler(registry, nil, geocodingNormalizer, tracker)
	healthHandler := health.NewHandler("0.1.0")

	tracer := tracing.DefaultTracer
	pMetrics := metrics.DefaultMetrics

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:    healthHandler,
		MetricsHandler:   metrics.Handler(pMetrics.Registry),
		WeatherHandler:   weatherHandler,
		GeocodingHandler: geocodingHandler,
		AuthMiddleware:   authMid,
	})

	logger := logging.NewLogger("ERROR", io.Discard)
	var handler http.Handler = appRouter
	handler = middleware.MaxBodySize(64 * 1024)(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Telemetry(tracer, pMetrics)(handler)
	handler = middleware.Recovery(logger)(handler)

	return &BenchmarkEnvironment{
		Handler:            handler,
		ValidAPIKey:        validRawKey,
		WeatherUpstream:    weatherServer,
		GeocodingUpstream:  geocodingServer,
		WeatherCallCount:   weatherCalls,
		GeocodingCallCount: geocodingCalls,
	}, cacheStore
}

// BenchmarkCache_GatewayWeather_Hit_Sequential measures single-threaded latency when 100% of requests are cache hits
func BenchmarkCache_GatewayWeather_Hit_Sequential(b *testing.B) {
	env, cStore := SetupBenchmarkEnvironmentWithCache()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()
	defer cStore.Close()

	req, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", env.ValidAPIKey)

	// Warm the cache once
	warmRec := httptest.NewRecorder()
	env.Handler.ServeHTTP(warmRec, req)
	if warmRec.Code != http.StatusOK {
		b.Fatalf("failed to warm cache: %d", warmRec.Code)
	}

	initialUpstreamCalls := env.WeatherCallCount.Load()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected response code: %d", rec.Code)
		}
	}

	b.StopTimer()
	additionalUpstreamCalls := env.WeatherCallCount.Load() - initialUpstreamCalls
	b.ReportMetric(float64(additionalUpstreamCalls)/float64(b.N), "upstream_calls/req")
}

// BenchmarkCache_GatewayWeather_Hit_Parallel measures multi-threaded concurrent throughput on 100% cache hits
func BenchmarkCache_GatewayWeather_Hit_Parallel(b *testing.B) {
	env, cStore := SetupBenchmarkEnvironmentWithCache()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()
	defer cStore.Close()

	req, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", env.ValidAPIKey)

	// Warm the cache once
	warmRec := httptest.NewRecorder()
	env.Handler.ServeHTTP(warmRec, req)
	if warmRec.Code != http.StatusOK {
		b.Fatalf("failed to warm cache: %d", warmRec.Code)
	}

	initialUpstreamCalls := env.WeatherCallCount.Load()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		r, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
		r.Header.Set("X-API-Key", env.ValidAPIKey)

		for pb.Next() {
			rec := httptest.NewRecorder()
			env.Handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				b.Errorf("unexpected status: %d", rec.Code)
			}
		}
	})

	b.StopTimer()
	additionalUpstreamCalls := env.WeatherCallCount.Load() - initialUpstreamCalls
	b.ReportMetric(float64(additionalUpstreamCalls)/float64(b.N), "upstream_calls/req")
}

// BenchmarkSingleFlight_ConcurrentMisses measures single-flight coalescing under parallel cache-miss load
func BenchmarkSingleFlight_ConcurrentMisses(b *testing.B) {
	env, cStore := SetupBenchmarkEnvironmentWithCache()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()
	defer cStore.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		r, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
		r.Header.Set("X-API-Key", env.ValidAPIKey)

		for pb.Next() {
			rec := httptest.NewRecorder()
			env.Handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				b.Errorf("unexpected status: %d", rec.Code)
			}
		}
	})

	b.StopTimer()
	b.ReportMetric(float64(env.WeatherCallCount.Load())/float64(b.N), "upstream_calls/req")
}

// BenchmarkStale_GatewayWeather_Sequential measures latency of serving a stale-while-revalidate cached item
func BenchmarkStale_GatewayWeather_Sequential(b *testing.B) {
	env, cStore := SetupBenchmarkEnvironmentWithCache()
	defer env.WeatherUpstream.Close()
	defer env.GeocodingUpstream.Close()
	defer cStore.Close()

	req, _ := http.NewRequest(http.MethodGet, "/v1/weather?location=London", nil)
	req.Header.Set("X-API-Key", env.ValidAPIKey)

	// Pre-populate with stale state
	cacheKey := cache.BuildWeatherCacheKey(cache.WeatherKeyParams{
		Location:      "London",
		SchemaVersion: "weather.v1",
		Unit:          "celsius",
	})
	_ = cStore.SetWithStale(context.Background(), cacheKey, []byte(`{
		"data": {
			"location": {"city": "London", "country": "GB", "coordinates": {"latitude": 51.51, "longitude": -0.13}},
			"temperature": {"value": 22.5, "unit": "celsius"},
			"condition": {"text": "Clear", "code": "clear"},
			"humidity": 60,
			"timestamp": "2026-08-26T12:00:00Z"
		},
		"provider_id": "openweather",
		"attribution": []
	}`), 0, 1*time.Hour) // freshUntil = now, staleUntil = now + 1h (so it's STALE)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		env.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected response code: %d", rec.Code)
		}
	}
}

func BenchmarkDecisionEngine_ProviderSelection(b *testing.B) {
	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("dummy-key", "http://mock.weather"))
	registry.Register(weatherapi.NewAdapter("dummy-key", "http://mock.weather"))
	registry.Register(nominatim.NewAdapter("http://mock.geocode"))
	registry.Register(locationiq.NewAdapter("dummy-key", "http://mock.geocode"))

	engine := decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	ctx := context.Background()
	reqCtx := decision.RequestContext{
		Capability:  "weather",
		CountryCode: "GB",
		PolicyMode:  decision.PolicyAvailabilityFirst,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		providers, _, err := engine.SelectProviders(ctx, reqCtx)
		if err != nil || len(providers) == 0 {
			b.Fatalf("unexpected error during provider selection: %v", err)
		}
	}
}
