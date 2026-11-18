package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/database"
)

type mockFailingChecker struct{}

func (m *mockFailingChecker) Check() (string, map[string]string, bool) {
	return "DOWN", map[string]string{"mock_db": "DOWN", "error": "connection refused"}, false
}

func TestHealthEndpoints(t *testing.T) {
	dbStore := database.NewMemoryStore()
	cacheStore := cache.NewMemoryStore(cache.DefaultMemoryStoreOptions())

	dbChecker := NewDatabaseChecker(dbStore)
	cacheChecker := NewCacheChecker(cacheStore)

	h := NewHandler("0.1.0", dbChecker, cacheChecker)

	// 1. Health
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /health, got %d", rec.Code)
	}

	// 2. Readiness (Healthy)
	req = httptest.NewRequest(http.MethodGet, "/readiness", nil)
	rec = httptest.NewRecorder()
	h.Readiness(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /readiness, got %d", rec.Code)
	}

	// 3. Liveness
	req = httptest.NewRequest(http.MethodGet, "/liveness", nil)
	rec = httptest.NewRecorder()
	h.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /liveness, got %d", rec.Code)
	}

	// 4. Readiness with Failing Checker
	failingHandler := NewHandler("0.1.0", &mockFailingChecker{})
	req = httptest.NewRequest(http.MethodGet, "/readiness", nil)
	rec = httptest.NewRecorder()
	failingHandler.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for failing /readiness, got %d", rec.Code)
	}

	// 5. Readiness with closed database
	_ = dbStore.Close()
	req = httptest.NewRequest(http.MethodGet, "/readiness", nil)
	rec = httptest.NewRecorder()
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for closed database /readiness, got %d", rec.Code)
	}

	// Reset db for other potential tests
	_ = dbStore.Ping(context.Background())
}
