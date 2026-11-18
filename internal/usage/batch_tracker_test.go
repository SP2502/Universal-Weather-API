package usage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/database"
)

func TestBatchTracker_ConcurrentFlushing(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemoryStore()

	opts := BatchTrackerOptions{
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
		QueueCapacity: 1000,
	}

	tracker := NewBatchTracker(store, opts)

	const totalRecords = 55
	var wg sync.WaitGroup
	for i := 0; i < totalRecords; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tracker.Record(ctx, "key-1", "weather", "openweather", fmt.Sprintf("req-%d", idx), 200, 45)
		}(i)
	}

	wg.Wait()

	// Wait for background worker to flush via ticker or batch size
	time.Sleep(120 * time.Millisecond)

	count := store.GetUsageCount()
	if count != totalRecords {
		t.Errorf("expected %d usage records in store, got %d", totalRecords, count)
	}

	_ = tracker.Close()
}

func TestBatchTracker_GracefulDrainOnClose(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemoryStore()

	opts := BatchTrackerOptions{
		BatchSize:     100,
		FlushInterval: 10 * time.Second, // long ticker, relies on Close() drain
		QueueCapacity: 500,
	}

	tracker := NewBatchTracker(store, opts)

	for i := 0; i < 25; i++ {
		tracker.Record(ctx, "key-1", "geocode", "nominatim", fmt.Sprintf("req-%d", i), 200, 30)
	}

	// Immediately close, should drain all 25 records
	if err := tracker.Close(); err != nil {
		t.Fatalf("failed to close tracker: %v", err)
	}

	count := store.GetUsageCount()
	if count != 25 {
		t.Errorf("expected 25 usage records after close drain, got %d", count)
	}
}
