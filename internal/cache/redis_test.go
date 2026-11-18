package cache

import (
	"context"
	"testing"
	"time"
)

func TestRedisStore_FailOpenBehavior(t *testing.T) {
	ctx := context.Background()
	// Non-existent Redis port to test fail-open memory fallback
	opts := RedisOptions{
		Addr:         "127.0.0.1:59999",
		Timeout:      100 * time.Millisecond,
		MaxIdleConns: 2,
		FailOpen:     true,
	}

	store, err := NewRedisStore(opts)
	if err != nil {
		t.Fatalf("expected NewRedisStore to succeed with FailOpen=true, got err: %v", err)
	}
	defer store.Close()

	key := "uap:cache:test:failopen"
	val := []byte(`{"temperature": 22.5, "city": "London"}`)

	// Set with stale
	if err := store.SetWithStale(ctx, key, val, 50*time.Millisecond, 150*time.Millisecond); err != nil {
		t.Fatalf("expected SetWithStale to succeed in fail-open mode, got %v", err)
	}

	// Immediate Get (Fresh)
	entry, err := store.GetEntry(ctx, key)
	if err != nil {
		t.Fatalf("expected GetEntry to succeed, got %v", err)
	}
	if entry.State != StateFresh {
		t.Errorf("expected StateFresh, got %v", entry.State)
	}
	if string(entry.Value) != string(val) {
		t.Errorf("unexpected value: %s", string(entry.Value))
	}

	// Wait past fresh TTL (Stale)
	time.Sleep(70 * time.Millisecond)
	entryStale, err := store.GetEntry(ctx, key)
	if err != nil {
		t.Fatalf("expected GetEntry to succeed, got %v", err)
	}
	if entryStale.State != StateStale {
		t.Errorf("expected StateStale, got %v", entryStale.State)
	}

	// Wait past stale TTL (Expired)
	time.Sleep(100 * time.Millisecond)
	entryExp, err := store.GetEntry(ctx, key)
	if err != nil {
		t.Fatalf("expected GetEntry to succeed, got %v", err)
	}
	if entryExp.State != StateExpired {
		t.Errorf("expected StateExpired, got %v", entryExp.State)
	}

	// Delete
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("expected Delete to succeed, got %v", err)
	}
}

func TestRedisStore_Validation(t *testing.T) {
	_, err := NewRedisStore(RedisOptions{})
	if err == nil {
		t.Errorf("expected error when address is empty")
	}

	opts := DefaultRedisOptions("127.0.0.1:59999")
	store, _ := NewRedisStore(opts)
	defer store.Close()

	if err := store.Set(context.Background(), "", []byte("test"), time.Minute); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
	if err := store.Set(context.Background(), "k", nil, time.Minute); err != ErrNilValue {
		t.Errorf("expected ErrNilValue, got %v", err)
	}
	if _, err := store.GetEntry(context.Background(), ""); err != ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}
