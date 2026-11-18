package usage

import (
	"context"
	"github.com/universal-api/platform/internal/database"
	"testing"
	"time"
)

type stalledUsage struct {
	database.Store
	started chan struct{}
}

func (s stalledUsage) RecordUsageBatch(ctx context.Context, _ []*database.UsageRecord) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestShutdownSharesOneBudgetAcrossUsageQueue(t *testing.T) {
	s := stalledUsage{Store: database.NewMemoryStore(), started: make(chan struct{}, 1)}
	bt := NewBatchTracker(s, BatchTrackerOptions{BatchSize: 1, QueueCapacity: 20, FlushInterval: time.Hour})
	for i := 0; i < 20; i++ {
		bt.Record(context.Background(), "k", "weather", "openweather", "r", 200, 0)
	}
	select {
	case <-s.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if e := bt.CloseContext(ctx); e == nil {
		t.Fatal("expected shutdown timeout/loss")
	}
	if time.Since(start) > time.Second {
		t.Fatal("shutdown exceeded total budget")
	}
	select {
	case <-bt.done:
	case <-time.After(time.Second):
		t.Fatal("cancelled worker leaked")
	}
	if bt.FailedCount()+bt.DroppedCount() == 0 {
		t.Fatal("lost records not counted")
	}
}
