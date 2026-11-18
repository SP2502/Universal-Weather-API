package reliability

import (
	"testing"
	"time"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, 50*time.Millisecond)

	if cb.State() != StateClosed {
		t.Errorf("expected CLOSED, got %s", cb.State())
	}

	// 1 Failure -> CLOSED
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Errorf("expected CLOSED, got %s", cb.State())
	}

	// 2 Failures -> OPEN
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Errorf("expected OPEN, got %s", cb.State())
	}

	if err := cb.Allow(); err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}

	// Reset Timeout expires -> HALF_OPEN
	time.Sleep(60 * time.Millisecond)
	if err := cb.Allow(); err != nil {
		t.Errorf("expected allowed in HALF_OPEN, got %v", err)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("expected HALF_OPEN, got %s", cb.State())
	}

	// Success -> CLOSED
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Errorf("expected CLOSED, got %s", cb.State())
	}
}
