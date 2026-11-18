package usage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/database"
)

type brokenBatchStore struct{ database.Store }

func (s brokenBatchStore) RecordUsageBatch(context.Context, []*database.UsageRecord) error {
	return errors.New("synthetic storage failure")
}

func TestPhase04_BatchFailureIsCountedAndCloseReportsLoss(t *testing.T) {
	store := brokenBatchStore{Store: database.NewMemoryStore()}
	tracker := NewBatchTracker(store, BatchTrackerOptions{BatchSize: 5, FlushInterval: time.Millisecond * 10, QueueCapacity: 50})
	for i := 0; i < 7; i++ {
		tracker.Record(context.Background(), "k", "weather", "ow", "r", 200, 1)
	}
	if err := tracker.Close(); err == nil {
		t.Fatal("usage persistence loss was silently ignored")
	}
	if tracker.FailedCount() != 7 || tracker.DroppedCount() != 0 {
		t.Fatalf("failed=%d dropped=%d", tracker.FailedCount(), tracker.DroppedCount())
	}
}

func TestPhase04_ConcurrentRecordAndCloseAccountsForEveryEnqueue(t *testing.T) {
	store := database.NewMemoryStore()
	tracker := NewBatchTracker(store, BatchTrackerOptions{BatchSize: 20, FlushInterval: time.Millisecond * 20, QueueCapacity: 200})
	var wg sync.WaitGroup
	const total = 120
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tracker.Record(context.Background(), "key", "weather", "ow", "id", 200, 1) }()
	}
	done := make(chan struct{})
	go func() { _ = tracker.Close(); close(done) }()
	wg.Wait()
	<-done
	actual := store.GetUsageCount()
	if actual+int(tracker.DroppedCount())+int(tracker.FailedCount()) != total {
		t.Fatalf("lost unaccounted records: stored=%d dropped=%d failed=%d total=%d", actual, tracker.DroppedCount(), tracker.FailedCount(), total)
	}
}
