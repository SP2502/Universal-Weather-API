package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/openweather"
)

// Run only against disposable, task-owned PostgreSQL and Redis instances.
func TestRealBackendFixtureSoak(t *testing.T) {
	dsn, redisAddr := os.Getenv("UWA_SOAK_POSTGRES_DSN"), os.Getenv("UWA_SOAK_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("requires task-owned UWA_SOAK_POSTGRES_DSN and UWA_SOAK_REDIS_ADDR")
	}
	requestCount := 500
	if raw := os.Getenv("UWA_SOAK_REQUESTS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 2 || parsed > 10000 || parsed%2 != 0 {
			t.Fatal("UWA_SOAK_REQUESTS must be an even number from 2 to 10000")
		}
		requestCount = parsed
	}
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"coord":{"lat":51.5074,"lon":-0.1278},"weather":[{"id":800,"main":"Clear","description":"clear sky"}],"main":{"temp":280.15,"humidity":60},"name":"London","dt":%d}`, time.Now().Unix())
	}))
	defer upstream.Close()
	key := fmt.Sprintf("uap_soak_%d", time.Now().UnixNano())
	stores := make([]*database.PostgresStore, 0, 2)
	redisStores := make([]*cache.RedisStore, 0, 2)
	servers := make([]*httptest.Server, 0, 2)
	defer func() {
		for _, server := range servers {
			server.Close()
		}
		for _, store := range redisStores {
			_ = store.Close()
		}
		for _, store := range stores {
			_ = store.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, store)
		if i == 0 {
			err = store.CreateAPIKey(context.Background(), &database.APIKey{ID: key, Name: "fixture soak", KeyPrefix: "uap_soak_", KeyHash: database.HashKey(key), Scopes: []string{"weather:read"}, IsActive: true, CreatedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
		}
		opts := cache.DefaultRedisOptions(redisAddr)
		opts.FailOpen = false
		redisStore, err := cache.NewRedisStore(opts)
		if err != nil {
			t.Fatal(err)
		}
		redisStores = append(redisStores, redisStore)
		registry := provider.NewRegistry()
		registry.Register(openweather.NewAdapter("fixture", upstream.URL))
		weather := capability.NewWeatherHandler(registry, nil, normalization.NewWeatherNormalizer(), usage.NewTracker(store), redisStore, cache.NewPolicyEngine())
		server := httptest.NewServer(routing.NewRouter(routing.RouterDependencies{
			HealthHandler: health.NewHandler("soak"), WeatherHandler: weather,
			AuthMiddleware: auth.Middleware(auth.NewAuthenticator(store), store), ReleaseProfile: "lean_v1",
		}))
		servers = append(servers, server)
	}
	// A unique coordinate makes this cache key independent of earlier test runs.
	path := fmt.Sprintf("/v1/weather?lat=%.6f&lon=-0.1278", 51.4+float64(time.Now().UnixNano()%100000)/1000000)
	client := &http.Client{Timeout: 5 * time.Second}
	type sample struct {
		duration time.Duration
		status   int
		cached   bool
		provider string
		err      error
	}
	request := func(server *httptest.Server) sample {
		started := time.Now()
		req, err := http.NewRequest("GET", server.URL+path, nil)
		if err != nil {
			return sample{err: err}
		}
		req.Header.Set("X-API-Key", key)
		resp, err := client.Do(req)
		if err != nil {
			return sample{duration: time.Since(started), err: err}
		}
		defer resp.Body.Close()
		var body struct {
			Meta struct {
				Provider string `json:"provider"`
				Cached   bool   `json:"cached"`
			} `json:"meta"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		return sample{duration: time.Since(started), status: resp.StatusCode, cached: body.Meta.Cached, provider: body.Meta.Provider, err: err}
	}
	warm := request(servers[0])
	if warm.err != nil || warm.status != 200 || warm.provider != "openweather" {
		t.Fatalf("warm request: %+v", warm)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeGoroutines := runtime.NumGoroutine()
	started := time.Now()
	results := make(chan sample, requestCount)
	var wg sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func(server *httptest.Server) {
			defer wg.Done()
			for i := 0; i < requestCount/2; i++ {
				results <- request(server)
			}
		}(servers[worker])
	}
	wg.Wait()
	close(results)
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	durations := make([]time.Duration, 0, requestCount)
	hits, failures := 0, 0
	for result := range results {
		durations = append(durations, result.duration)
		if result.cached {
			hits++
		}
		if result.err != nil || result.status != 200 || result.provider != "openweather" {
			failures++
			if failures <= 5 {
				t.Logf("fixture soak failed sample %d: duration=%s status=%d cached=%t provider=%q err=%v", failures, result.duration, result.status, result.cached, result.provider, result.err)
			}
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("fixture soak: requests=%d workers=2 errors=%d cache_hits=%d upstream_calls=%d elapsed=%s throughput=%.1f/s p50=%s p95=%s p99=%s heap_before=%d heap_after=%d goroutines_before=%d goroutines_after=%d", len(durations), failures, hits, upstreamCalls.Load(), elapsed, float64(len(durations))/elapsed.Seconds(), durations[(len(durations)-1)*50/100], durations[(len(durations)-1)*95/100], durations[(len(durations)-1)*99/100], before.HeapAlloc, after.HeapAlloc, beforeGoroutines, runtime.NumGoroutine())
	if failures != 0 || hits != requestCount || upstreamCalls.Load() != 1 {
		t.Fatalf("soak failure: errors=%d hits=%d upstream_calls=%d", failures, hits, upstreamCalls.Load())
	}
}
