package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/cache"
)

func TestPhase04_RotatingUnauthenticatedKeysDoesNotBypassLimits(t *testing.T) {
	rl := NewRateLimiter(0.00001, 1)
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i, key := range []string{"faked_key_1", "faked_key_2", "faked_key_3"} {
		req := httptest.NewRequest("GET", "/v1/weather", nil)
		req.RemoteAddr = "192.0.2.10:2345"
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		want := http.StatusTooManyRequests
		if i == 0 {
			want = http.StatusNoContent
		}
		if rec.Code != want {
			t.Fatalf("request %d status=%d want=%d", i, rec.Code, want)
		}
	}
	if len(rl.buckets) != 1 {
		t.Fatalf("rotating unverified keys added %d buckets", len(rl.buckets))
	}
}

func TestPhase04_DistributedLimitFailsClosedWhenRedisOffline(t *testing.T) {
	opts := cache.DefaultRedisOptions("127.0.0.1:59998")
	opts.Timeout = 35 * time.Millisecond
	store, err := cache.NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := NewDistributedRateLimiter(1, 1, store).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing shared counter allowed request: %d", rec.Code)
	}
}
