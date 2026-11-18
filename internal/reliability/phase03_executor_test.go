package reliability

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

func TestPhase03_BudgetLeavesTimeForFallback(t *testing.T) {
	e := NewExecutor(4)
	p1 := &mockProv{m: provider.Manifest{ID: "slow", Timeout: 5 * time.Second, Capabilities: []string{"weather"}}}
	p2 := &mockProv{m: provider.Manifest{ID: "fast", Timeout: 5 * time.Second, Capabilities: []string{"weather"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	out, meta, err := ExecuteWithFallback[string](ctx, e, []provider.Provider{p1, p2}, RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, func(ctx context.Context, p provider.Provider) (string, error) {
		if p.Manifest().ID == "slow" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "healthy", nil
	})
	if err != nil || out != "healthy" || meta == nil || !meta.FallbackUsed {
		t.Fatalf("fallback starved, out=%s meta=%+v err=%v", out, meta, err)
	}
	if elapsed := time.Since(started); elapsed >= 300*time.Millisecond {
		t.Fatalf("fallback exceeded overall budget: %s", elapsed)
	}
}

func TestPhase03_HalfOpenOnlyOneConcurrentProbe(t *testing.T) {
	cb := NewCircuitBreaker(1, 2, 20*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(30 * time.Millisecond)
	if err := cb.Allow(); err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.Allow() == nil {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 0 {
		t.Fatalf("allowed %d concurrent half-open probes", admitted.Load())
	}
	cb.RecordSuccess()
	if cb.State() != StateHalfOpen {
		t.Fatalf("first probe should not close threshold-2 breaker: %s", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Fatal(err)
	}
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("second probe should close breaker: %s", cb.State())
	}
}

func TestPhase03_QuotaAndNoMatchDoNotTripSystemCircuit(t *testing.T) {
	for _, failure := range []error{provider.ErrRateLimited, provider.ErrProviderNotFound, provider.ErrProviderDisabled} {
		e := NewExecutor(4)
		p1 := &mockProv{m: provider.Manifest{ID: "primary", Timeout: time.Second}}
		p2 := &mockProv{m: provider.Manifest{ID: "secondary", Timeout: time.Second}}
		n := 0
		for i := 0; i < 8; i++ {
			out, meta, err := ExecuteWithFallback[string](context.Background(), e, []provider.Provider{p1, p2}, DefaultRetryPolicy(), func(ctx context.Context, p provider.Provider) (string, error) {
				if p.Manifest().ID == "primary" {
					n++
					return "", failure
				}
				return "yes", nil
			})
			if err != nil || out != "yes" || !meta.FallbackUsed {
				t.Fatalf("err=%v meta=%+v", err, meta)
			}
		}
		if n != 8 {
			t.Fatalf("non-systemic failure %v should not make primary unavailable; got %d calls", failure, n)
		}
		if e.getBreaker("primary").State() != StateClosed {
			t.Fatalf("non-systemic failure %v opened circuit", failure)
		}
	}
}

func TestPhase03_AlreadyCancelledDoesNotAttemptProviders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, _, err := ExecuteWithFallback[string](ctx, NewExecutor(2), []provider.Provider{&mockProv{m: provider.Manifest{ID: "one"}}}, DefaultRetryPolicy(), func(context.Context, provider.Provider) (string, error) { calls++; return "", nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
