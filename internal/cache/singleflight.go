package cache

import (
	"context"
	"sync"
	"time"
)

type call struct {
	done    chan struct{}
	cancel  context.CancelFunc
	val     []byte
	err     error
	waiters int
	shared  bool
}

// SingleFlightGroup deduplicates work only while it has active consumers.
// Cancellation of the last waiter cancels upstream work; cancellation of a
// leader does not abandon followers. The leader's context values (including
// trace correlation) survive without its cancellation/deadline.
type SingleFlightGroup struct {
	mu    sync.Mutex
	calls map[string]*call
}

func NewSingleFlightGroup() *SingleFlightGroup {
	return &SingleFlightGroup{calls: make(map[string]*call)}
}
func (g *SingleFlightGroup) Do(ctx context.Context, key string, timeout time.Duration, fn func(context.Context) ([]byte, error)) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	g.mu.Lock()
	c, exists := g.calls[key]
	if exists {
		c.shared = true
		c.waiters++
	} else {
		// Use the first caller's trace and request attributes, but a flight-wide
		// deadline and cancellation independent of any one waiter.
		sharedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		c = &call{done: make(chan struct{}), cancel: cancel, waiters: 1}
		g.calls[key] = c
		go func() {
			defer cancel()
			val, err := fn(sharedCtx)
			g.mu.Lock()
			c.val, c.err = val, err
			if g.calls[key] == c {
				delete(g.calls, key)
			}
			close(c.done)
			g.mu.Unlock()
		}()
	}
	g.mu.Unlock()

	// Always decrement consumer references. When none remain, cancel the
	// backend operation and remove its key so a future request starts anew.
	defer func() {
		g.mu.Lock()
		c.waiters--
		if c.waiters == 0 {
			c.cancel()
			if g.calls[key] == c {
				delete(g.calls, key)
			}
		}
		g.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, exists, ctx.Err()
	case <-c.done:
		if err := ctx.Err(); err != nil {
			return nil, exists, err
		}
		// c.done is closed under the mutex after val/err are written; the close
		// provides synchronization for readers that have observed it.
		return c.val, c.shared, c.err
	}
}
func (g *SingleFlightGroup) InFlightCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}
