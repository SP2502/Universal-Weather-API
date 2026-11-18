package usage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/universal-api/platform/internal/database"
)

type BatchTrackerOptions struct {
	BatchSize     int           // Max number of records per DB insert batch (default 100)
	FlushInterval time.Duration // Max duration before flushing partial batch (default 250ms)
	QueueCapacity int           // Bounded queue capacity (default 10,000)
}

func DefaultBatchTrackerOptions() BatchTrackerOptions {
	return BatchTrackerOptions{
		BatchSize:     100,
		FlushInterval: 250 * time.Millisecond,
		QueueCapacity: 10000,
	}
}

type BatchTracker struct {
	store      database.Store
	workCtx    context.Context
	cancelWork context.CancelFunc
	opts       BatchTrackerOptions
	queue      chan *database.UsageRecord
	stopChan   chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	dropped    atomic.Int64
	failed     atomic.Int64
	mu         sync.RWMutex
	isClosed   bool
}

func NewBatchTracker(store database.Store, opts BatchTrackerOptions) *BatchTracker {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 250 * time.Millisecond
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 10000
	}

	workCtx, cancelWork := context.WithCancel(context.Background())
	bt := &BatchTracker{workCtx: workCtx, cancelWork: cancelWork,
		store:    store,
		opts:     opts,
		queue:    make(chan *database.UsageRecord, opts.QueueCapacity),
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
	}

	go bt.workerLoop()

	return bt
}

func (bt *BatchTracker) Record(ctx context.Context, keyID, capability, provider, requestID string, statusCode, durationMs int) {
	bt.mu.RLock()
	if bt.isClosed {
		bt.dropped.Add(1)
		bt.mu.RUnlock()
		return
	}

	rec := &database.UsageRecord{
		KeyID:      keyID,
		Capability: capability,
		Provider:   provider,
		StatusCode: statusCode,
		DurationMs: durationMs,
		RequestID:  requestID,
		CreatedAt:  time.Now().UTC(),
	}

	select {
	case bt.queue <- rec:
	default:
		// Queue full: fail-open by dropping to protect gateway latency
		bt.dropped.Add(1)
	}
	bt.mu.RUnlock()
}

func (bt *BatchTracker) workerLoop() {
	defer close(bt.done)

	ticker := time.NewTicker(bt.opts.FlushInterval)
	defer ticker.Stop()

	batch := make([]*database.UsageRecord, 0, bt.opts.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toWrite := make([]*database.UsageRecord, len(batch))
		copy(toWrite, batch)
		batch = batch[:0]

		ctx, cancel := context.WithTimeout(bt.workCtx, 3*time.Second)
		if err := bt.store.RecordUsageBatch(ctx, toWrite); err != nil {
			// An ambiguous database commit must not be retried blindly: duplicates
			// are possible. The failure remains visible to operators.
			bt.failed.Add(int64(len(toWrite)))
			slog.Error("usage batch persistence failed", "count", len(toWrite), "error", err)
		}
		cancel()
	}

	for {
		select {
		case <-bt.stopChan:
			// Drain remaining in queue
			for {
				if bt.workCtx.Err() != nil {
					bt.failed.Add(int64(len(batch) + len(bt.queue)))
					return
				}
				select {
				case r := <-bt.queue:
					batch = append(batch, r)
					if len(batch) >= bt.opts.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}

		case r := <-bt.queue:
			batch = append(batch, r)
			if len(batch) >= bt.opts.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (bt *BatchTracker) DroppedCount() int64 { return bt.dropped.Load() }
func (bt *BatchTracker) FailedCount() int64  { return bt.failed.Load() }
func (bt *BatchTracker) QueueLength() int    { return len(bt.queue) }

func (bt *BatchTracker) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return bt.CloseContext(ctx)
}
func (bt *BatchTracker) CloseContext(ctx context.Context) error {
	stop := context.AfterFunc(ctx, bt.cancelWork)
	defer stop()
	defer bt.cancelWork()
	bt.stopOnce.Do(func() {
		bt.mu.Lock()
		bt.isClosed = true
		close(bt.stopChan)
		bt.mu.Unlock()
	})
	select {
	case <-bt.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if lost := bt.DroppedCount() + bt.FailedCount(); lost > 0 {
		return fmt.Errorf("usage accounting lost %d records (queue=%d, persistence=%d)", lost, bt.DroppedCount(), bt.FailedCount())
	}
	return nil
}
