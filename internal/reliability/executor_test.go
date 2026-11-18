package reliability

import (
	"context"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

type mockProv struct {
	m provider.Manifest
}

func TestExecutorAttemptCapReservesFallback(t *testing.T) {
	exec := NewExecutor(4)
	providers := []provider.Provider{
		&mockProv{m: provider.Manifest{ID: "primary", Timeout: time.Second}},
		&mockProv{m: provider.Manifest{ID: "fallback", Timeout: time.Second}},
	}
	policy := RetryPolicy{MaxAttempts: 10, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
	counts := map[string]int{}
	got, meta, err := ExecuteWithFallback[string](context.Background(), exec, providers, policy, func(_ context.Context, p provider.Provider) (string, error) {
		id := p.Manifest().ID
		counts[id]++
		if id == "primary" {
			return "", provider.ErrProviderUnavailable
		}
		return "ok", nil
	})
	if err != nil || got != "ok" || meta == nil || meta.Attempts != 4 || !meta.FallbackUsed || counts["primary"] != 3 || counts["fallback"] != 1 {
		t.Fatalf("got=%q meta=%+v counts=%v err=%v", got, meta, counts, err)
	}
}

func TestExecutorRateLimitDoesNotRetrySameProvider(t *testing.T) {
	exec := NewExecutor(4)
	providers := []provider.Provider{
		&mockProv{m: provider.Manifest{ID: "primary", Timeout: time.Second}},
		&mockProv{m: provider.Manifest{ID: "fallback", Timeout: time.Second}},
	}
	counts := map[string]int{}
	got, meta, err := ExecuteWithFallback[string](context.Background(), exec, providers, DefaultRetryPolicy(), func(_ context.Context, p provider.Provider) (string, error) {
		id := p.Manifest().ID
		counts[id]++
		if id == "primary" {
			return "", provider.ErrRateLimited
		}
		return "ok", nil
	})
	if err != nil || got != "ok" || meta == nil || meta.Attempts != 2 || counts["primary"] != 1 || counts["fallback"] != 1 {
		t.Fatalf("got=%q meta=%+v counts=%v err=%v", got, meta, counts, err)
	}
}

func (m *mockProv) Manifest() provider.Manifest { return m.m }

func TestExecutor_FallbackChainExecution(t *testing.T) {
	exec := NewExecutor(5)

	p1 := &mockProv{m: provider.Manifest{ID: "p1", Timeout: time.Second}}
	p2 := &mockProv{m: provider.Manifest{ID: "p2", Timeout: time.Second}}

	policy := RetryPolicy{MaxAttempts: 1, InitialBackoff: 10 * time.Millisecond, Jitter: false}

	// Case 1: Primary (p1) fails -> Fallback to p2
	providers := []provider.Provider{p1, p2}
	res, meta, err := ExecuteWithFallback[string](context.Background(), exec, providers, policy, func(ctx context.Context, p provider.Provider) (string, error) {
		if p.Manifest().ID == "p1" {
			return "", provider.ErrProviderUnavailable
		}
		return "success-p2", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "success-p2" {
		t.Errorf("expected success-p2, got %s", res)
	}
	if meta.ProviderID != "p2" || !meta.FallbackUsed {
		t.Errorf("expected fallback metadata, got %+v", meta)
	}
}
