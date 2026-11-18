package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_StaleStateTransitions(t *testing.T) {
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 10, NumShards: 2})
	defer store.Close()

	ctx := context.Background()
	key := "test:stale:transitions"
	val := []byte("weather-london-payload")

	// Set with freshTTL = 100ms, staleTTL = 400ms (wide window for OS timer reliability)
	err := store.SetWithStale(ctx, key, val, 100*time.Millisecond, 400*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected SetWithStale error: %v", err)
	}

	// 1. Immediate -> StateFresh
	entry, err := store.GetEntry(ctx, key)
	if err != nil || entry.State != StateFresh || string(entry.Value) != "weather-london-payload" {
		t.Fatalf("expected StateFresh immediately, got state=%s, val=%s, err=%v", entry.State, string(entry.Value), err)
	}

	// 2. Wait 150ms -> StateStale (100ms < 150ms < 400ms)
	time.Sleep(150 * time.Millisecond)
	entry, err = store.GetEntry(ctx, key)
	if err != nil || entry.State != StateStale || string(entry.Value) != "weather-london-payload" {
		t.Fatalf("expected StateStale after freshTTL, got state=%s, val=%s, err=%v", entry.State, string(entry.Value), err)
	}

	// Simple Get() on stale entry returns false (not fresh)
	data, found, err := store.Get(ctx, key)
	if err != nil || found || data != nil {
		t.Fatalf("expected simple Get() to return found=false for stale entry, got found=%v", found)
	}

	// 3. Wait another 300ms -> StateExpired (150 + 300 = 450ms > 400ms)
	time.Sleep(300 * time.Millisecond)
	entry, err = store.GetEntry(ctx, key)
	if err != nil || entry.State != StateExpired {
		t.Fatalf("expected StateExpired after staleTTL, got state=%s, err=%v", entry.State, err)
	}
}
