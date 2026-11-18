package reliability

import (
	"context"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/quota"
	"testing"
	"time"
)

func TestPhase07B_WeightedOverBudgetProviderFallsBack(t *testing.T) {
	manager := quota.NewManager(map[string]quota.Budget{"a": {PerDay: 1, Unit: "weighted_calls", CostMilli: 2000}}, nil)
	exec := NewExecutor(3)
	exec.SetQuotaGate(manager)
	ctx := WithQuotaProduct(context.Background(), "hourly_forecast")
	actualCalls := 0
	result, meta, err := ExecuteWithFallback[string](ctx, exec, []provider.Provider{
		&mockProv{m: provider.Manifest{ID: "a", Timeout: time.Second}},
		&mockProv{m: provider.Manifest{ID: "b", Timeout: time.Second}},
	}, DefaultRetryPolicy(), func(_ context.Context, p provider.Provider) (string, error) {
		actualCalls++
		return p.Manifest().ID, nil
	})
	if err != nil || result != "b" || meta == nil || !meta.FallbackUsed || actualCalls != 1 {
		t.Fatalf("result=%q meta=%+v calls=%d err=%v", result, meta, actualCalls, err)
	}
}
