package reliability

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/quotactx"
)

type ExecutionResult struct {
	ProviderID   string
	Payload      interface{}
	RetrievedAt  time.Time
	FallbackUsed bool
	Attempts     int
}

type Executor struct {
	mu              sync.RWMutex
	breakers        map[string]*CircuitBreaker
	bulkheads       map[string]*Bulkhead
	maxAttempts     int
	healthEvaluator *provider.HealthEvaluator
	quotaGate       QuotaGate
}

// QuotaGate is deliberately small so the reliability engine never depends on
// vendor-specific licensing metadata. It is called before each actual attempt.
type QuotaGate interface {
	AllowAttempt(ctx context.Context, providerID string) (bool, error)
}

// WithQuotaProduct carries a validated operation name into per-attempt quota
// admission. It never affects provider routing or introduces a model/LLM.
func WithQuotaProduct(ctx context.Context, product string) context.Context {
	return quotactx.WithProduct(ctx, product)
}
func QuotaProduct(ctx context.Context) string { return quotactx.Product(ctx) }

func (e *Executor) SetQuotaGate(gate QuotaGate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.quotaGate = gate
}
func (e *Executor) getQuotaGate() QuotaGate {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.quotaGate
}

func NewExecutor(maxAttempts int) *Executor {
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	return &Executor{breakers: make(map[string]*CircuitBreaker), bulkheads: make(map[string]*Bulkhead), maxAttempts: maxAttempts}
}
func (e *Executor) SetHealthEvaluator(he *provider.HealthEvaluator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthEvaluator = he
}
func (e *Executor) getBreaker(id string) *CircuitBreaker {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cb, ok := e.breakers[id]; ok {
		return cb
	}
	cb := NewCircuitBreaker(5, 2, 5*time.Second)
	e.breakers[id] = cb
	return cb
}
func (e *Executor) getBulkhead(id string) *Bulkhead {
	e.mu.Lock()
	defer e.mu.Unlock()
	if bh, ok := e.bulkheads[id]; ok {
		return bh
	}
	bh := NewBulkhead(20)
	e.bulkheads[id] = bh
	return bh
}
func (e *Executor) getHealthEvaluator() *provider.HealthEvaluator {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.healthEvaluator
}

// boundedAttemptTimeout reserves a portion of the parent budget for later
// providers. It never extends the caller's deadline. Without an overall
// deadline the manifest timeout is the bound.
func boundedAttemptTimeout(ctx context.Context, preferred time.Duration, laterProviders int) time.Duration {
	if preferred <= 0 {
		preferred = 5 * time.Second
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return preferred
	}
	remaining := time.Until(deadline)
	if laterProviders > 0 {
		// Reserve at least one useful alternative-provider window where possible.
		reserve := remaining / 2
		if reserve > 2*time.Second {
			reserve = 2 * time.Second
		}
		remaining -= reserve
	}
	if remaining < preferred {
		return remaining
	}
	return preferred
}

func metricOutcome(err error) string {
	if errors.Is(err, provider.ErrMalformedResponse) {
		return "invalid"
	}
	if errors.Is(err, provider.ErrRateLimited) {
		return "429"
	}
	if errors.Is(err, provider.ErrProviderNotFound) {
		return "404"
	}
	if errors.Is(err, provider.ErrProviderDisabled) {
		return "auth"
	}
	return "5xx"
}

// providerFailure distinguishes systemic provider failures from legitimate
// no-match and credential/configuration failures. Neither should open a global
// breaker. Canonically invalid upstream data IS a provider failure.
func providerFailure(err error) bool {
	return !errors.Is(err, provider.ErrAmbiguousLocation) && !errors.Is(err, provider.ErrProviderNotFound) &&
		!errors.Is(err, provider.ErrProviderDisabled) &&
		!errors.Is(err, provider.ErrRateLimited) &&
		!errors.Is(err, context.Canceled)
}

// ExecuteWithFallback accepts an operation that includes fetch + normalization
// + semantic validation. Only a fully valid result may close the circuit or
// finish the fallback chain. The total attempt cap also reserves one attempt
// for each later candidate where capacity allows.
func ExecuteWithFallback[T any](ctx context.Context, executor *Executor, providers []provider.Provider, retryPolicy RetryPolicy, op func(context.Context, provider.Provider) (T, error)) (T, *ExecutionResult, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if len(providers) == 0 {
		return zero, nil, provider.ErrProviderNotFound
	}
	if executor == nil {
		return zero, nil, errors.New("nil reliability executor")
	}
	if retryPolicy.MaxAttempts <= 0 {
		retryPolicy.MaxAttempts = 1
	}
	attempts := 0
	var lastErr error = provider.ErrProviderUnavailable
	he := executor.getHealthEvaluator()

	for idx, p := range providers {
		if err := ctx.Err(); err != nil {
			return zero, nil, err
		}
		if attempts >= executor.maxAttempts {
			break
		}
		manifest := p.Manifest()
		id := manifest.ID
		cb := executor.getBreaker(id)
		bh := executor.getBulkhead(id)
		metrics.DefaultMetrics.CircuitBreakerState.Set(cb.StateNumeric(), metrics.Label{Name: "provider", Value: id})
		if err := cb.Allow(); err != nil {
			lastErr = fmt.Errorf("provider %s: %w", id, err)
			continue
		}
		probe := cb.State() == StateHalfOpen
		if he != nil {
			he.UpdateCircuitStateWithProbe(id, cb.State().String(), cb.NextProbeAt())
		}
		if err := bh.Acquire(); err != nil {
			cb.ReleaseProbe()
			lastErr = fmt.Errorf("provider %s: %w", id, err)
			continue
		}
		metrics.DefaultMetrics.BulkheadActiveSlots.Set(int64(bh.ActiveCount()), metrics.Label{Name: "provider", Value: id})
		release := func() {
			bh.Release()
			metrics.DefaultMetrics.BulkheadActiveSlots.Set(int64(bh.ActiveCount()), metrics.Label{Name: "provider", Value: id})
		}

		remainingProviders := len(providers) - idx - 1
		maxHere := retryPolicy.MaxAttempts
		// Attempt budget is shared, but don't let primary retries consume the one
		// slot needed for every other candidate.
		available := executor.maxAttempts - attempts - remainingProviders
		if available < 1 {
			available = 1
		}
		if maxHere > available {
			maxHere = available
		}
		if probe {
			maxHere = 1
		}
		attempted := false
		capName := "unknown"
		if len(manifest.Capabilities) > 0 {
			capName = manifest.Capabilities[0]
		}

		for n := 1; n <= maxHere; n++ {
			if err := ctx.Err(); err != nil {
				cb.ReleaseProbe()
				release()
				return zero, nil, err
			}
			timeout := boundedAttemptTimeout(ctx, manifest.Timeout, remainingProviders)
			if timeout <= 0 {
				break
			}
			// Enforce configured upstream allowances on EVERY attempted request,
			// including retries. Redis failure fails closed, never per-replica.
			if gate := executor.getQuotaGate(); gate != nil {
				allowed, quotaErr := gate.AllowAttempt(ctx, id)
				if quotaErr != nil {
					lastErr = fmt.Errorf("provider %s quota backend: %w", id, quotaErr)
					break
				}
				if !allowed {
					lastErr = fmt.Errorf("provider %s: %w", id, provider.ErrRateLimited)
					break
				}
			}
			attempted = true
			attempts++
			spanCtx, span := tracing.DefaultTracer.StartSpan(ctx, fmt.Sprintf("Provider %s", id))
			span.SetAttribute("provider.id", id)
			span.SetAttribute("provider.attempt", fmt.Sprintf("%d", n))
			opCtx, cancel := context.WithTimeout(spanCtx, timeout)
			began := time.Now()
			result, err := op(opCtx, p)
			// A provider returning after its deadline cannot be accepted as success.
			if err == nil && opCtx.Err() != nil {
				err = opCtx.Err()
			}
			elapsed := time.Since(began).Seconds()
			cancel()
			metrics.DefaultMetrics.ProviderRequestDuration.Observe(elapsed,
				metrics.Label{Name: "provider", Value: id}, metrics.Label{Name: "capability", Value: capName})

			if err == nil {
				cb.RecordSuccess()
				if he != nil {
					he.RecordSuccess(id, elapsed*1000)
					he.UpdateCircuitStateWithProbe(id, cb.State().String(), cb.NextProbeAt())
				}
				metrics.DefaultMetrics.ProviderRequestsTotal.Inc(metrics.Label{Name: "provider", Value: id}, metrics.Label{Name: "capability", Value: capName}, metrics.Label{Name: "status_class", Value: "2xx"})
				metrics.DefaultMetrics.CircuitBreakerState.Set(cb.StateNumeric(), metrics.Label{Name: "provider", Value: id})
				span.SetStatus(tracing.StatusOK, "OK")
				span.End()
				release()
				return result, &ExecutionResult{ProviderID: id, Payload: result, RetrievedAt: time.Now().UTC(), FallbackUsed: idx > 0, Attempts: attempts}, nil
			}

			// Ambiguity is a client-side resolution conflict. Trying another
			// geocoder could return a different first result and silently
			// hide the ambiguity; abort without penalizing provider health.
			if errors.Is(err, provider.ErrAmbiguousLocation) {
				cb.ReleaseProbe()
				span.SetStatus(tracing.StatusError, "ambiguous user location")
				span.End()
				release()
				return zero, nil, err
			}
			lastErr = err
			span.SetStatus(tracing.StatusError, "provider attempt failed")
			span.End()
			metrics.DefaultMetrics.ProviderRequestsTotal.Inc(metrics.Label{Name: "provider", Value: id}, metrics.Label{Name: "capability", Value: capName}, metrics.Label{Name: "status_class", Value: metricOutcome(err)})
			if ctx.Err() != nil {
				cb.ReleaseProbe()
				release()
				return zero, nil, ctx.Err()
			}
			if providerFailure(err) {
				cb.RecordFailure()
				if he != nil {
					he.RecordFailure(id, errors.Is(err, context.DeadlineExceeded))
				}
			} else {
				cb.ReleaseProbe()
			}
			if he != nil {
				he.UpdateCircuitStateWithProbe(id, cb.State().String(), cb.NextProbeAt())
			}
			metrics.DefaultMetrics.CircuitBreakerState.Set(cb.StateNumeric(), metrics.Label{Name: "provider", Value: id})
			// On quota exhaustion choose another provider, never retry 429 in a burst.
			if probe || errors.Is(err, provider.ErrRateLimited) || !IsRetryableError(err) || n == maxHere {
				break
			}
			backoff := CalculateBackoff(n, retryPolicy)
			// Don't sleep away time reserved for the fallback candidate.
			deadlineBudget := boundedAttemptTimeout(ctx, manifest.Timeout, remainingProviders)
			if deadlineBudget <= backoff {
				break
			}
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				cb.ReleaseProbe()
				release()
				return zero, nil, ctx.Err()
			}
		}
		if !attempted {
			cb.ReleaseProbe()
		}
		release()
	}
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	return zero, nil, fmt.Errorf("all providers failed after %d attempts: %w", attempts, lastErr)
}
