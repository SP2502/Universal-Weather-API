package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/database"
)

// LastUsedWriter coalesces high-frequency key touches into bounded periodic
// writes. API-key authentication itself still reads authoritative storage on
// every request, preserving immediate revocation and expiry semantics.
type LastUsedWriter struct {
	store      database.Store
	workCtx    context.Context
	cancelWork context.CancelFunc
	mu         sync.Mutex
	pending    map[string]time.Time
	maxPending int
	interval   time.Duration
	timeout    time.Duration
	stop       chan struct{}
	done       chan struct{}
	once       sync.Once
	closed     bool
	dropped    uint64
	flushErr   error
}

func NewLastUsedWriter(store database.Store, interval time.Duration, maxPending int) *LastUsedWriter {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if maxPending <= 0 {
		maxPending = 2048
	}
	workCtx, cancelWork := context.WithCancel(context.Background())
	w := &LastUsedWriter{workCtx: workCtx, cancelWork: cancelWork, store: store, pending: make(map[string]time.Time), interval: interval, maxPending: maxPending, timeout: 2 * time.Second, stop: make(chan struct{}), done: make(chan struct{})}
	go w.run()
	return w
}

func (w *LastUsedWriter) Record(id string) {
	if w == nil || id == "" {
		return
	}
	now := time.Now().UTC()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if _, ok := w.pending[id]; !ok && len(w.pending) >= w.maxPending {
		w.dropped++
		return
	}
	w.pending[id] = now
}

// Stats exposes bounded diagnostics without revealing key IDs.
func (w *LastUsedWriter) Stats() (pending int, dropped uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending), w.dropped
}

func (w *LastUsedWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.flush()
		case <-w.stop:
			w.flush()
			return
		}
	}
}

func (w *LastUsedWriter) flush() {
	w.mu.Lock()
	batch := w.pending
	w.pending = make(map[string]time.Time)
	w.mu.Unlock()
	// A full batch must never keep shutdown blocked for minutes when the
	// database is stalled. LastUsedAt is best-effort, not an auth decision.
	deadline, cancelAll := context.WithTimeout(w.workCtx, 5*time.Second)
	defer cancelAll()
	for id, when := range batch {
		if deadline.Err() != nil {
			break
		}
		oneCtx, cancel := context.WithTimeout(deadline, w.timeout)
		err := w.store.UpdateAPIKeyLastUsed(oneCtx, id, when)
		cancel()
		if err != nil {
			w.mu.Lock()
			if w.flushErr == nil {
				w.flushErr = err
			}
			w.mu.Unlock()
		}
	}
	if deadline.Err() != nil {
		w.mu.Lock()
		if w.flushErr == nil {
			w.flushErr = errors.New("last-used flush deadline exceeded")
		}
		w.mu.Unlock()
	}
}

// Close drains pending key touches before the caller closes its database store.
func (w *LastUsedWriter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.CloseContext(ctx)
}
func (w *LastUsedWriter) CloseContext(ctx context.Context) error {
	if w == nil {
		return nil
	}
	stop := context.AfterFunc(ctx, w.cancelWork)
	defer stop()
	defer w.cancelWork()
	w.once.Do(func() { w.mu.Lock(); w.closed = true; w.mu.Unlock(); close(w.stop) })
	select {
	case <-w.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushErr
}
