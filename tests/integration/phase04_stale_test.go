package integration

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/cache"
)

func phase04Request(h http.Handler, location string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/v1/weather?location="+location, nil)
	req.Header.Set("X-API-Key", "uap_t1_secret_key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestPhase04_ExpiredSWRFetchesForegroundButSIEStillWorksOnError(t *testing.T) {
	failed := &atomic.Bool{}
	handler, store, pol, count, upstream := setupStaleTestGateway(0, failed)
	defer upstream.Close()
	defer store.Close()
	pol.RegisterPolicy(&cache.CapabilityPolicy{
		Capability: "weather", Type: cache.ConditionallyCacheable,
		DefaultTTL: 50 * time.Millisecond, MaxTTL: time.Minute,
		StaleWhileRevalidateWindow: 20 * time.Millisecond,
		StaleIfErrorWindow:         3 * time.Second,
	})
	first := phase04Request(handler, "Berlin")
	if first.Code != 200 || count.Load() != 1 {
		t.Fatalf("initial request: status=%d calls=%d", first.Code, count.Load())
	}
	time.Sleep(95 * time.Millisecond) // after 50ms fresh + 20ms SWR
	second := phase04Request(handler, "Berlin")
	if second.Code != 200 || count.Load() < 2 {
		t.Fatalf("returned retained stale without foreground revalidation: status=%d calls=%d", second.Code, count.Load())
	}
	// The second request has refreshed the cache; after SWR expires again,
	// only a verified upstream failure may use the retained value.
	time.Sleep(95 * time.Millisecond)
	failed.Store(true)
	third := phase04Request(handler, "Berlin")
	if third.Code != 200 || third.Header().Get("Warning") == "" {
		t.Fatalf("SIE outage fallback unavailable: status=%d body=%s", third.Code, third.Body.String())
	}
}
