package authorization

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/auth"
)

func TestRequireScope(t *testing.T) {
	handler := RequireScope("weather:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: No caller -> 401
	req := httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing caller, got %d", rec.Code)
	}

	// Case 2: Insufficient scope -> 403
	ctx := auth.WithCaller(req.Context(), &auth.Caller{
		KeyID:  "k1",
		Scopes: []string{"other:read"},
	})
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing scope, got %d", rec.Code)
	}

	// Case 3: Valid scope -> 200
	ctx = auth.WithCaller(req.Context(), &auth.Caller{
		KeyID:  "k1",
		Scopes: []string{"weather:read"},
	})
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid scope, got %d", rec.Code)
	}
}
