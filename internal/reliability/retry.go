package reliability

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

type RetryPolicy struct {
	MaxAttempts    int           `json:"max_attempts"`
	InitialBackoff time.Duration `json:"initial_backoff"`
	MaxBackoff     time.Duration `json:"max_backoff"`
	BackoffFactor  float64       `json:"backoff_factor"`
	Jitter         bool          `json:"jitter"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
		BackoffFactor:  2.0,
		Jitter:         true,
	}
}

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, provider.ErrProviderUnavailable) || errors.Is(err, provider.ErrRateLimited) {
		return true
	}
	return false
}

func CalculateBackoff(attempt int, policy RetryPolicy) time.Duration {
	if attempt <= 0 {
		return 0
	}
	backoff := float64(policy.InitialBackoff) * mathPow(policy.BackoffFactor, attempt-1)
	if backoff > float64(policy.MaxBackoff) {
		backoff = float64(policy.MaxBackoff)
	}
	if policy.Jitter {
		// Randomize backoff between 50% and 100% of calculated value
		jitterFactor := 0.5 + rand.Float64()*0.5
		backoff = backoff * jitterFactor
	}
	return time.Duration(backoff)
}

func mathPow(base float64, exp int) float64 {
	res := 1.0
	for i := 0; i < exp; i++ {
		res *= base
	}
	return res
}
