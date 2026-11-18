package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/database"
)

func TestAuthMiddleware(t *testing.T) {
	store := database.NewMemoryStore()
	rawKey := "uap_live_testkey123"
	keyHash := database.HashKey(rawKey)

	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-1",
		Name:      "Test App",
		KeyPrefix: "uap_live_",
		KeyHash:   keyHash,
		Scopes:    []string{"weather:read"},
		IsActive:  true,
	})

	auth := NewAuthenticator(store)
	mid := Middleware(auth, store)

	var reached bool
	handler := mid(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		caller := CallerFromContext(r.Context())
		if caller == nil || caller.KeyID != "key-1" {
			t.Errorf("expected caller key-1, got %v", caller)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: No key -> 401
	req := httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	// Case 2: Valid X-API-Key -> 200
	req = httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
	req.Header.Set("X-API-Key", rawKey)
	rec = httptest.NewRecorder()
	reached = false
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !reached {
		t.Errorf("expected 200 and reached=true, got %d, reached=%v", rec.Code, reached)
	}

	// Case 3: Revoked Key -> 401
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-revoked",
		Name:      "Revoked",
		KeyPrefix: "uap_live_",
		KeyHash:   database.HashKey("revoked-key"),
		Scopes:    []string{"weather:read"},
		IsActive:  false,
	})
	req = httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
	req.Header.Set("X-API-Key", "revoked-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for revoked key, got %d", rec.Code)
	}
}
