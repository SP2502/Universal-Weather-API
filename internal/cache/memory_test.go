package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_SetGetDelete(t *testing.T) {
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 100, NumShards: 4})
	defer store.Close()

	ctx := context.Background()
	key := "uap:cache:v1:weather:test1"
	val := []byte(`{"temperature": 22.5, "city": "London"}`)

	// 1. Get non-existent
	data, found, err := store.Get(ctx, key)
	if err != nil || found || data != nil {
		t.Fatalf("expected not found for missing key, got (%v, %v, %v)", data, found, err)
	}

	// 2. Set
	if err := store.Set(ctx, key, val, 1*time.Minute); err != nil {
		t.Fatalf("unexpected Set error: %v", err)
	}

	// 3. Get existing
	data, found, err = store.Get(ctx, key)
	if err != nil || !found || string(data) != string(val) {
		t.Fatalf("expected to find data %s, got (%s, %v, %v)", string(val), string(data), found, err)
	}

	if store.Len() != 1 {
		t.Errorf("expected store length 1, got %d", store.Len())
	}

	// 4. Delete
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}

	data, found, err = store.Get(ctx, key)
	if err != nil || found {
		t.Fatalf("expected key to be deleted, got (%v, %v)", data, found)
	}
	if store.Len() != 0 {
		t.Errorf("expected store length 0, got %d", store.Len())
	}
}

func TestMemoryStore_ValueImmutability(t *testing.T) {
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 10, NumShards: 2})
	defer store.Close()

	ctx := context.Background()
	key := "test:immutability"
	original := []byte("original-payload")

	// Store value
	_ = store.Set(ctx, key, original, 1*time.Minute)

	// Mutate local original slice
	original[0] = 'X'

	// Fetch from cache - should still be "original-payload"
	fetched1, found, err := store.Get(ctx, key)
	if err != nil || !found || string(fetched1) != "original-payload" {
		t.Fatalf("cache was mutated via local slice mutation: got %s", string(fetched1))
	}

	// Mutate fetched slice
	fetched1[0] = 'Z'

	// Fetch again from cache - should still be "original-payload"
	fetched2, found, err := store.Get(ctx, key)
	if err != nil || !found || string(fetched2) != "original-payload" {
		t.Fatalf("cache was mutated via returned slice mutation: got %s", string(fetched2))
	}
}

func TestMemoryStore_TTLExpiration(t *testing.T) {
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 10, NumShards: 2})
	defer store.Close()

	ctx := context.Background()
	key := "test:ttl"
	val := []byte("ephemeral-data")

	// Set with very short TTL
	_ = store.Set(ctx, key, val, 25*time.Millisecond)

	// Immediate fetch -> found
	data, found, err := store.Get(ctx, key)
	if err != nil || !found || string(data) != "ephemeral-data" {
		t.Fatalf("expected data immediately, got (%s, %v, %v)", string(data), found, err)
	}

	// Wait for expiration
	time.Sleep(35 * time.Millisecond)

	// Fetch after expiration -> not found
	data, found, err = store.Get(ctx, key)
	if err != nil || found || data != nil {
		t.Fatalf("expected key to be expired, got (%s, %v, %v)", string(data), found, err)
	}
}

func TestMemoryStore_CapacityBoundingAndLRUEviction(t *testing.T) {
	// 1 shard, max 3 entries
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 3, NumShards: 1})
	defer store.Close()

	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"), 1*time.Minute)
	_ = store.Set(ctx, "k2", []byte("v2"), 1*time.Minute)
	_ = store.Set(ctx, "k3", []byte("v3"), 1*time.Minute)

	// Access k1 so k2 becomes the oldest (LRU)
	_, _, _ = store.Get(ctx, "k1")

	// Insert k4, should evict k2
	_ = store.Set(ctx, "k4", []byte("v4"), 1*time.Minute)

	if store.Len() != 3 {
		t.Errorf("expected exactly 3 entries, got %d", store.Len())
	}

	// k2 should be evicted
	_, foundK2, _ := store.Get(ctx, "k2")
	if foundK2 {
		t.Errorf("expected k2 to be evicted by LRU, but it was found")
	}

	// k1, k3, k4 should exist
	_, foundK1, _ := store.Get(ctx, "k1")
	_, foundK3, _ := store.Get(ctx, "k3")
	_, foundK4, _ := store.Get(ctx, "k4")
	if !foundK1 || !foundK3 || !foundK4 {
		t.Errorf("expected k1, k3, k4 to remain in cache")
	}
}

func TestMemoryStore_Concurrency(t *testing.T) {
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 100, NumShards: 8})
	defer store.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	workers := 20
	iterations := 100

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("key-%d", (workerID*10+i)%50)
				val := []byte(fmt.Sprintf("val-%d-%d", workerID, i))

				_ = store.Set(ctx, key, val, 500*time.Millisecond)
				_, _, _ = store.Get(ctx, key)
				if i%5 == 0 {
					_ = store.Delete(ctx, key)
				}
			}
		}(w)
	}

	wg.Wait()
}

func TestMemoryStore_MaxValueSizeLimit(t *testing.T) {
	// Max value 64 bytes
	store := NewMemoryStore(MemoryStoreOptions{MaxEntries: 10, NumShards: 2, MaxValueSizeBytes: 64})
	defer store.Close()

	ctx := context.Background()

	// 1. Small payload succeeds
	smallVal := []byte("small-payload-within-limit")
	if err := store.Set(ctx, "k-small", smallVal, 1*time.Minute); err != nil {
		t.Fatalf("expected small payload to succeed, got %v", err)
	}

	// 2. Large payload exceeds limit
	largeVal := make([]byte, 128)
	for i := range largeVal {
		largeVal[i] = 'A'
	}
	err := store.Set(ctx, "k-large", largeVal, 1*time.Minute)
	if err != ErrValueTooLarge {
		t.Fatalf("expected ErrValueTooLarge, got %v", err)
	}
}
