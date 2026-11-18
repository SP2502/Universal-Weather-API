package reliability

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is OPEN")

type State string

const (
	StateClosed   State = "CLOSED"
	StateOpen     State = "OPEN"
	StateHalfOpen State = "HALF_OPEN"
)

func (s State) String() string {
	return string(s)
}

type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failureThreshold int
	successThreshold int
	resetTimeout     time.Duration

	failures         int
	halfOpenSuccess  int
	halfOpenInFlight bool
	lastStateChange  time.Time
}

func NewCircuitBreaker(failureThreshold, successThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if successThreshold <= 0 {
		successThreshold = 2
	}
	if resetTimeout <= 0 {
		resetTimeout = 10 * time.Second
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
		lastStateChange:  time.Now(),
	}
}

// Allow is the sole admission gate. An expired OPEN circuit transitions to
// HALF_OPEN and admits exactly one probe. A second request cannot race into it.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen {
		if time.Since(cb.lastStateChange) < cb.resetTimeout {
			return ErrCircuitOpen
		}
		cb.state = StateHalfOpen
		cb.halfOpenSuccess = 0
		cb.halfOpenInFlight = false
		cb.lastStateChange = time.Now()
	}
	if cb.state == StateHalfOpen {
		if cb.halfOpenInFlight {
			return ErrCircuitOpen
		}
		cb.halfOpenInFlight = true
	}
	return nil
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateHalfOpen {
		cb.halfOpenInFlight = false
		cb.halfOpenSuccess++
		if cb.halfOpenSuccess >= cb.successThreshold {
			cb.state = StateClosed
			cb.failures = 0
			cb.lastStateChange = time.Now()
		}
	} else if cb.state == StateClosed {
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == StateHalfOpen || (cb.state == StateClosed && cb.failures >= cb.failureThreshold) {
		cb.state = StateOpen
		cb.halfOpenInFlight = false
		cb.halfOpenSuccess = 0
		cb.lastStateChange = time.Now()
	}
}

// ReleaseProbe relinquishes a half-open slot when no real provider outcome was
// observed (caller cancellation, bulkhead rejection, or exhausted deadline).
// It does not count a failure or permanently prevent recovery.
func (cb *CircuitBreaker) ReleaseProbe() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateHalfOpen {
		cb.halfOpenInFlight = false
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) StateNumeric() int64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case StateOpen:
		return 2
	case StateHalfOpen:
		return 1
	default:
		return 0
	}
}

// NextProbeAt is a scheduling hint, not an admission decision. Only Allow may
// actually transition an OPEN breaker or grant the half-open probe slot.
func (cb *CircuitBreaker) NextProbeAt() time.Time {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen {
		return cb.lastStateChange.Add(cb.resetTimeout)
	}
	return time.Time{}
}
