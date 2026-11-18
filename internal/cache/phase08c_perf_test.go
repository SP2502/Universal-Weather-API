package cache

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"testing"
	"time"
)

func TestPhase08CShardCapacityNeverExceedsMaximum(t *testing.T) {
	for _, tc := range []struct{ entries, shards int }{
		{1, 16}, {3, 16}, {10, 3}, {11, 4}, {17, 16}, {100, 12},
	} {
		t.Run(fmt.Sprintf("entries_%d_shards_%d", tc.entries, tc.shards), func(t *testing.T) {
			s := NewMemoryStore(MemoryStoreOptions{MaxEntries: tc.entries, NumShards: tc.shards})
			defer s.Close()
			total := 0
			for _, sh := range s.shards {
				total += sh.maxEntries
			}
			if total != tc.entries {
				t.Fatalf("max allowed %d, sum of shard capacity %d", tc.entries, total)
			}
			if len(s.shards) > tc.entries {
				t.Fatalf("created more shards than total entries")
			}
			for i := 0; i < 1000; i++ {
				if err := s.Set(context.Background(), fmt.Sprintf("entry-%d", i), []byte("data"), time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			if s.Len() > tc.entries {
				t.Fatalf("cache exceeds configured capacity: %d > %d", s.Len(), tc.entries)
			}
		})
	}
}

func TestPhase08CShardHashMatchesFNV1a(t *testing.T) {
	s := NewMemoryStore(MemoryStoreOptions{MaxEntries: 128, NumShards: 16})
	defer s.Close()
	for _, key := range []string{"", "plain", "uap:cache:v1:weather:27fafa7f", "हिन्दी", "☀️", "a\\\"b"} {
		h := fnv.New32a()
		_, _ = h.Write([]byte(key))
		want := s.shards[h.Sum32()%uint32(s.numShards)]
		if got := s.getShard(key); got != want {
			t.Fatalf("hash mismatch %q", key)
		}
	}
}

func TestPhase08CCloseSynchronizesWithConcurrentWrites(t *testing.T) {
	for trial := 0; trial < 20; trial++ {
		s := NewMemoryStore(MemoryStoreOptions{MaxEntries: 64, NumShards: 4})
		var wg sync.WaitGroup
		for worker := 0; worker < 6; worker++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; i < 80; i++ {
					err := s.Set(context.Background(), fmt.Sprintf("key-%d-%d", id, i), []byte("value"), time.Minute)
					if err != nil && err != ErrStoreClosed {
						t.Errorf("unexpected cache Set: %v", err)
						return
					}
					_, _ = s.GetEntry(context.Background(), "missing")
				}
			}(worker)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if got := s.Len(); got != 0 {
			t.Fatalf("close left %d entries after concurrent requests", got)
		}
		if err := s.Set(context.Background(), "after", []byte("v"), time.Minute); err != ErrStoreClosed {
			t.Fatalf("post-close Set should fail: %v", err)
		}
	}
}

func BenchmarkPhase08CShardedCacheHit(b *testing.B) {
	s := NewMemoryStore(DefaultMemoryStoreOptions())
	defer s.Close()
	ctx := context.Background()
	key := "uap:cache:v1:weather:benchmark"
	if err := s.Set(ctx, key, make([]byte, 768), time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, hit, err := s.Get(ctx, key)
		if err != nil || !hit || len(data) != 768 {
			b.Fatal("cache hit failure", err)
		}
	}
}
func BenchmarkPhase08CShardedCacheHitParallel(b *testing.B) {
	s := NewMemoryStore(DefaultMemoryStoreOptions())
	defer s.Close()
	ctx := context.Background()
	key := "uap:cache:v1:weather:parallel"
	if err := s.Set(ctx, key, make([]byte, 768), time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, hit, err := s.Get(ctx, key)
			if err != nil || !hit {
				b.Errorf("cache failure: %v", err)
			}
		}
	})
}
