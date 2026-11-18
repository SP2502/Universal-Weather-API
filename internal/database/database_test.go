package database

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("expected ping ok, got %v", err)
	}

	rawOriginalKey := "uap_live_test_123"
	originalHash := HashKey(rawOriginalKey)

	key := &APIKey{
		ID:        "key-1",
		Name:      "Test Key",
		KeyPrefix: "uap_live_",
		KeyHash:   originalHash,
		Scopes:    []string{"weather:read", "geocode:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	fetched, err := store.GetAPIKeyByHash(ctx, originalHash)
	if err != nil {
		t.Fatalf("failed to get api key by hash: %v", err)
	}
	if fetched.ID != "key-1" || !fetched.HasScope("weather:read") {
		t.Errorf("unexpected fetched key: %+v", fetched)
	}

	byID, err := store.GetAPIKeyByID(ctx, "key-1")
	if err != nil {
		t.Fatalf("failed to get api key by id: %v", err)
	}
	if byID.Name != "Test Key" {
		t.Errorf("unexpected key name: %s", byID.Name)
	}

	// Update last used
	now := time.Now().UTC()
	if err := store.UpdateAPIKeyLastUsed(ctx, "key-1", now); err != nil {
		t.Fatalf("failed to update last used: %v", err)
	}
	fetched2, _ := store.GetAPIKeyByID(ctx, "key-1")
	if fetched2.LastUsedAt == nil || !fetched2.LastUsedAt.Equal(now) {
		t.Errorf("expected last used updated")
	}

	// Usage records & Batch
	u1 := &UsageRecord{KeyID: "key-1", Capability: "weather", Provider: "openweather", StatusCode: 200, DurationMs: 50, RequestID: "req-1"}
	u2 := &UsageRecord{KeyID: "key-1", Capability: "geocode", Provider: "nominatim", StatusCode: 200, DurationMs: 80, RequestID: "req-2"}
	if err := store.RecordUsage(ctx, u1); err != nil {
		t.Fatalf("failed to record usage: %v", err)
	}
	if err := store.RecordUsageBatch(ctx, []*UsageRecord{u2}); err != nil {
		t.Fatalf("failed to record usage batch: %v", err)
	}
	if store.GetUsageCount() != 2 {
		t.Errorf("expected 2 usage records, got %d", store.GetUsageCount())
	}

	// Rotation with grace period
	newHash := HashKey("uap_live_new_456")
	if err := store.RotateAPIKey(ctx, "key-1", newHash, 100*time.Millisecond); err != nil {
		t.Fatalf("failed to rotate key: %v", err)
	}

	// Both old and new work during grace period
	if _, err := store.GetAPIKeyByHash(ctx, newHash); err != nil {
		t.Errorf("expected new hash to work during grace period: %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, originalHash); err != nil {
		t.Errorf("expected old hash to work during grace period: %v", err)
	}

	// After grace period, old hash should expire
	time.Sleep(150 * time.Millisecond)
	if _, err := store.GetAPIKeyByHash(ctx, originalHash); err != ErrKeyExpired {
		t.Errorf("expected ErrKeyExpired for old hash, got %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, newHash); err != nil {
		t.Errorf("expected new hash to continue working: %v", err)
	}

	// Revoke
	if err := store.RevokeAPIKey(ctx, "key-1"); err != nil {
		t.Fatalf("failed to revoke key: %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, newHash); err != ErrKeyRevoked {
		t.Errorf("expected ErrKeyRevoked after revocation, got %v", err)
	}

	_ = store.Close()
	if err := store.Ping(ctx); err == nil {
		t.Errorf("expected ping failure after close")
	}
}

func TestScopeFormatting(t *testing.T) {
	scopes := []string{"weather:read", "geocode:read"}
	formatted := formatScopesArray(scopes)
	if formatted != "{weather:read,geocode:read}" {
		t.Errorf("unexpected formatted scopes: %s", formatted)
	}

	parsed := parseScopesArray(formatted)
	if len(parsed) != 2 || parsed[0] != "weather:read" || parsed[1] != "geocode:read" {
		t.Errorf("unexpected parsed scopes: %+v", parsed)
	}

	emptyParsed := parseScopesArray("{}")
	if len(emptyParsed) != 0 {
		t.Errorf("expected empty slice for empty scopes")
	}
}
