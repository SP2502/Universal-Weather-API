package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/authorization"
	"github.com/universal-api/platform/internal/database"
)

func TestAPIKeyGeneration_CryptographicProperties(t *testing.T) {
	keyPair, err := auth.GenerateKey("uap_live_")
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	if !strings.HasPrefix(keyPair.PlaintextKey, "uap_live_") {
		t.Errorf("expected key to start with uap_live_, got %s", keyPair.PlaintextKey)
	}

	// 256 bits (32 bytes) in raw base64 is ~43 chars + prefix (9 chars) = ~52 chars
	if len(keyPair.PlaintextKey) < 50 {
		t.Errorf("key length unexpectedly short: %d chars", len(keyPair.PlaintextKey))
	}

	if keyPair.KeyPrefix != keyPair.PlaintextKey[:len("uap_live_")+8] {
		t.Errorf("unexpected key prefix: %s", keyPair.KeyPrefix)
	}

	expectedHash := database.HashKey(keyPair.PlaintextKey)
	if keyPair.KeyHash != expectedHash {
		t.Errorf("key hash mismatch: %s != %s", keyPair.KeyHash, expectedHash)
	}

	// Verify randomness across multiple runs
	keyPair2, _ := auth.GenerateKey("uap_live_")
	if keyPair.PlaintextKey == keyPair2.PlaintextKey {
		t.Fatalf("collision detected between two generated keys")
	}
}

func TestAPIKeyLifecycle_Revocation(t *testing.T) {
	store := database.NewMemoryStore()
	authenticator := auth.NewAuthenticator(store)
	ctx := context.Background()

	keyPair, _ := auth.GenerateKey("uap_live_")
	_ = store.CreateAPIKey(ctx, &database.APIKey{
		ID:        "key-tenant-rev",
		Name:      "Revocable Key",
		KeyPrefix: keyPair.KeyPrefix,
		KeyHash:   keyPair.KeyHash,
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	// 1. Verify key works when active
	caller, err := authenticator.AuthenticateKey(ctx, keyPair.PlaintextKey)
	if err != nil || caller.KeyID != "key-tenant-rev" {
		t.Fatalf("expected successful authentication for active key, got err: %v", err)
	}

	// 2. Revoke key
	if err := store.RevokeAPIKey(ctx, "key-tenant-rev"); err != nil {
		t.Fatalf("failed to revoke key: %v", err)
	}

	// 3. Verify key is immediately rejected
	_, err = authenticator.AuthenticateKey(ctx, keyPair.PlaintextKey)
	if err != database.ErrKeyRevoked {
		t.Fatalf("expected ErrKeyRevoked, got %v", err)
	}
}

func TestAPIKeyLifecycle_Expiration(t *testing.T) {
	store := database.NewMemoryStore()
	authenticator := auth.NewAuthenticator(store)
	ctx := context.Background()

	keyPair, _ := auth.GenerateKey("uap_live_")
	expiredTime := time.Now().Add(-1 * time.Hour)

	_ = store.CreateAPIKey(ctx, &database.APIKey{
		ID:        "key-tenant-exp",
		Name:      "Expired Key",
		KeyPrefix: keyPair.KeyPrefix,
		KeyHash:   keyPair.KeyHash,
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().Add(-24 * time.Hour),
		ExpiresAt: &expiredTime,
	})

	// Authenticate expired key
	_, err := authenticator.AuthenticateKey(ctx, keyPair.PlaintextKey)
	if err != database.ErrKeyExpired {
		t.Fatalf("expected ErrKeyExpired, got %v", err)
	}
}

func TestAPIKeyLifecycle_RotationWithGracePeriod(t *testing.T) {
	store := database.NewMemoryStore()
	authenticator := auth.NewAuthenticator(store)
	ctx := context.Background()

	oldKey, _ := auth.GenerateKey("uap_live_")
	_ = store.CreateAPIKey(ctx, &database.APIKey{
		ID:        "key-tenant-rot",
		Name:      "Rotating Key",
		KeyPrefix: oldKey.KeyPrefix,
		KeyHash:   oldKey.KeyHash,
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	// Generate new key and start rotation with 1-hour grace period
	newKey, _ := auth.GenerateKey("uap_live_")
	err := store.RotateAPIKey(ctx, "key-tenant-rot", newKey.KeyHash, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to rotate key: %v", err)
	}

	// 1. Verify new primary key works
	callerNew, err := authenticator.AuthenticateKey(ctx, newKey.PlaintextKey)
	if err != nil || callerNew.KeyID != "key-tenant-rot" {
		t.Fatalf("expected new key to authenticate successfully, got err: %v", err)
	}

	// 2. Verify old secondary key works during grace period
	callerOld, err := authenticator.AuthenticateKey(ctx, oldKey.PlaintextKey)
	if err != nil || callerOld.KeyID != "key-tenant-rot" {
		t.Fatalf("expected old key to authenticate during grace period, got err: %v", err)
	}
}

func TestScopeAuthorization_GranularEnforcement(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authorized"))
	})

	tests := []struct {
		name           string
		callerScopes   []string
		requiredScope  string
		expectedStatus int
	}{
		{"exact scope match", []string{"weather:read"}, "weather:read", http.StatusOK},
		{"wildcard admin scope", []string{"*"}, "weather:read", http.StatusOK},
		{"multiple scopes match", []string{"geocode:read", "weather:read"}, "weather:read", http.StatusOK},
		{"insufficient scope", []string{"geocode:read"}, "weather:read", http.StatusForbidden},
		{"empty scopes", []string{}, "weather:read", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := authorization.RequireScope(tt.requiredScope)(dummyHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			caller := &auth.Caller{
				KeyID:  "test-caller",
				Scopes: tt.callerScopes,
			}
			ctx := auth.WithCaller(req.Context(), caller)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			authorizer.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}
