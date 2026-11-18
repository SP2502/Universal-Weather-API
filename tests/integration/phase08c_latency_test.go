package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPhase08CLatencyDistribution is opt-in instrumentation, not a release
// acceptance test. HTTP calls use synthetic local providers and the in-memory
// API-key store; it cannot predict live network or PostgreSQL tail latency.
func TestPhase08CLatencyDistribution(t *testing.T) {
	if os.Getenv("UWA_PERF_RUN") != "1" {
		t.Skip("set UWA_PERF_RUN=1 for isolated synthetic distribution samples")
	}
	for _, tc := range []struct {
		name, route         string
		cached              bool
		workers, iterations int
	}{
		{"uncached_weather", "/v1/weather?location=London", false, 1, 800},
		{"cached_weather", "/v1/weather?location=London", true, 1, 1000},
		{"parallel_cached_weather", "/v1/weather?location=London", true, 12, 1200},
		{"uncached_geocode", "/v1/geocode?q=London", false, 1, 800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env *BenchmarkEnvironment
			var store interface{ Close() error }
			if tc.cached {
				var s interface{ Close() error }
				env, s = SetupBenchmarkEnvironmentWithCache()
				store = s
			} else {
				env = SetupBenchmarkEnvironment()
			}
			defer env.WeatherUpstream.Close()
			defer env.GeocodingUpstream.Close()
			if store != nil {
				defer store.Close()
			}
			newRequest := func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, tc.route, nil)
				req.Header.Set("X-API-Key", env.ValidAPIKey)
				return req
			}
			if tc.cached {
				rec := httptest.NewRecorder()
				env.Handler.ServeHTTP(rec, newRequest())
				if rec.Code != http.StatusOK {
					t.Fatalf("warmup status %d", rec.Code)
				}
			}
			// Warm code paths (JIT isn't used, but memory/cache initialization matters).
			for i := 0; i < 40; i++ {
				rec := httptest.NewRecorder()
				env.Handler.ServeHTTP(rec, newRequest())
				if rec.Code != 200 {
					t.Fatalf("warmup status %d", rec.Code)
				}
			}
			var upstreamBefore int64
			if tc.route == "/v1/geocode?q=London" {
				upstreamBefore = env.GeocodingCallCount.Load()
			} else {
				upstreamBefore = env.WeatherCallCount.Load()
			}
			samples := make([]time.Duration, tc.iterations)
			var index atomic.Int64
			var errors atomic.Int64
			before := runtime.NumGoroutine()
			started := time.Now()
			var wg sync.WaitGroup
			for worker := 0; worker < tc.workers; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						i := int(index.Add(1)) - 1
						if i >= len(samples) {
							return
						}
						begin := time.Now()
						rec := httptest.NewRecorder()
						env.Handler.ServeHTTP(rec, newRequest())
						samples[i] = time.Since(begin)
						if rec.Code != http.StatusOK {
							errors.Add(1)
						}
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(started)
			if errors.Load() > 0 {
				t.Fatalf("unexpected HTTP status count: %d", errors.Load())
			}
			var afterCalls int64
			if tc.route == "/v1/geocode?q=London" {
				afterCalls = env.GeocodingCallCount.Load()
			} else {
				afterCalls = env.WeatherCallCount.Load()
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			percentile := func(p float64) time.Duration { idx := int(float64(len(samples)-1) * p); return samples[idx] }
			t.Logf("SYNTHETIC_ONLY %s N=%d workers=%d p50=%s p95=%s p99=%s throughput=%.1f req/s upstream_calls=%d goroutines_before=%d goroutines_after=%d", tc.name, tc.iterations, tc.workers,
				percentile(.50), percentile(.95), percentile(.99), float64(tc.iterations)/elapsed.Seconds(), afterCalls-upstreamBefore, before, runtime.NumGoroutine())
			if tc.cached && afterCalls-upstreamBefore != 0 {
				t.Fatalf("unexpected upstream requests during warm-cache run: %d", afterCalls-upstreamBefore)
			}
		})
	}
}
