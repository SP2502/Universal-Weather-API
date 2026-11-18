package regression

import (
	"context"
	"fmt"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/quota"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditRealRedis(t *testing.T) {
	addr := os.Getenv("UWA_AUDIT_REDIS")
	if addr == "" {
		t.Skip("requires disposable real Redis")
	}
	opts := cache.DefaultRedisOptions(addr)
	opts.FailOpen = false
	opts.Timeout = 200 * time.Millisecond
	r, e := cache.NewRedisStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	r2, e := cache.NewRedisStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer r2.Close()
	ctx := context.Background()
	if e = r.Set(ctx, "audit-cache", []byte("hello"), 80*time.Millisecond); e != nil {
		t.Fatal(e)
	}
	b, hit, e := r2.Get(ctx, "audit-cache")
	if e != nil || !hit || string(b) != "hello" {
		t.Fatalf("cache %q %v %v", b, hit, e)
	}
	time.Sleep(100 * time.Millisecond)
	_, hit, e = r2.Get(ctx, "audit-cache")
	if e != nil || hit {
		t.Fatal("expiry failed")
	}
	bud := map[string]quota.Budget{"openweather": {PerDay: 10}}
	m1, m2 := quota.NewManager(bud, r), quota.NewManager(bud, r2)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := m1
			if i%2 == 1 {
				m = m2
			}
			ok, err := m.AllowAttempt(ctx, "openweather")
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}(i)
	}
	wg.Wait()
	t.Logf("real redis two managers admitted=%d/100 budget=10", allowed.Load())
	if allowed.Load() != 10 {
		t.Fatal("atomic quota failed")
	}
	verifySharedWeatherCache(t, r, r2)
	r.Close()
	status, details, ok := health.NewCacheChecker(r).Check()
	t.Logf("closed required redis checker: status=%s details=%v ready=%v", status, details, ok)
	if ok {
		t.Fatal("required Redis loss must fail readiness")
	}
	h := middleware.NewDistributedRateLimiter(100, 200, r).ClientAdmission(http.HandlerFunc(health.NewHandler("audit").Liveness))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/liveness", nil))
	t.Logf("closed redis liveness=%d body=%s", w.Code, w.Body.String())
	if w.Code != 200 {
		t.Fatal("liveness must not depend on Redis")
	}
}
func TestAuditRedisProcessChild(t *testing.T) {
	addr := os.Getenv("UWA_AUDIT_REDIS")
	if os.Getenv("UWA_AUDIT_CHILD") != "1" || addr == "" {
		t.Skip("controller only")
	}
	opts := cache.DefaultRedisOptions(addr)
	opts.Password = "audit-fixture-password"
	opts.FailOpen = false
	opts.DB = 3
	r, e := cache.NewRedisStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	m := quota.NewManager(map[string]quota.Budget{"openweather": {PerDay: 10, Group: "audit-cross-process"}}, r)
	n := 0
	for i := 0; i < 50; i++ {
		ok, e := m.AllowAttempt(context.Background(), "openweather")
		if e != nil {
			t.Fatal(e)
		}
		if ok {
			n++
		}
	}
	t.Logf("ADMITTED=%d", n)
}

// Real Redis delivery boundaries, using only local synthetic upstreams.
func verifySharedWeatherCache(t *testing.T, r, r2 *cache.RedisStore) {
	var owCalls, waCalls atomic.Int64
	osrv := server(t, ow(21.2, 81.6, time.Now().Unix()), 200, "", &owCalls)
	wsrv := server(t, wa(), 200, "", &waCalls)
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fixture", osrv.URL))
	status, _, body := request(t, handler(t, reg, r))
	if status != 200 {
		t.Fatalf("warm shared cache:%s", body)
	}
	reg2 := provider.NewRegistry()
	reg2.Register(weatherapi.NewAdapter("fixture", wsrv.URL))
	h := handler(t, reg2, r2)
	status, env, body := request(t, h)
	if status != 200 || env.Meta.Provider != "weatherapi" || env.Meta.Cached || waCalls.Load() != 1 {
		t.Fatalf("removed source survived real Redis:%s", body)
	}
	lat, lon := 21.2, 81.6
	k := cache.BuildWeatherCacheKey(cache.WeatherKeyParams{Latitude: &lat, Longitude: &lon, PolicyMode: string(decision.PolicyAvailabilityFirst)})
	payload := fmt.Sprintf(`{"integrity_version":1,"provider_id":"weatherapi","data":{"schema_version":"weather.v1","location":{"latitude":21.2,"longitude":81.6},"temperature":{"unit":"celsius"},"condition":"clear","observed_at":%q}}`, time.Now().UTC().Format(time.RFC3339))
	if e := r.Set(context.Background(), k, []byte(payload), time.Minute); e != nil {
		t.Fatal(e)
	}
	status, env, body = request(t, h)
	if status != 200 || env.Meta.Cached || waCalls.Load() != 2 {
		t.Fatalf("missing weather number fabricated from Redis:%s", body)
	}
	t.Log("real Redis: removed source rejected; missing numeric value recomputed without fabricating zero")
}
