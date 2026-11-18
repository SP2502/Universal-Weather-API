package reliability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

func TestIsRetryableError(t *testing.T) {
	if !IsRetryableError(provider.ErrProviderUnavailable) {
		t.Error("expected ErrProviderUnavailable to be retryable")
	}
	if !IsRetryableError(context.DeadlineExceeded) {
		t.Error("expected context.DeadlineExceeded to be retryable")
	}
	if IsRetryableError(errors.New("generic error")) {
		t.Error("expected generic error to not be retryable")
	}
}

func TestCalculateBackoff(t *testing.T) {
	policy := RetryPolicy{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         false,
	}

	b1 := CalculateBackoff(1, policy)
	if b1 != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", b1)
	}

	b2 := CalculateBackoff(2, policy)
	if b2 != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", b2)
	}
}
