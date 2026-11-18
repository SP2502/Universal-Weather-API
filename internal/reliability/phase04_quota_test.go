package reliability

import (
	"context"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/quota"
	"testing"
	"time"
)

func TestPhase04_QuotaExhaustedPrimaryFallsBackWithoutUpstreamCall(t *testing.T) {
	e := NewExecutor(4)
	gate := quota.NewManager(map[string]quota.Budget{"a": {PerMinute: 1, PerDay: 1}}, nil)
	_, err := gate.AllowAttempt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	e.SetQuotaGate(gate)
	n := 0
	data, meta, err := ExecuteWithFallback[string](context.Background(), e, []provider.Provider{
		&mockProv{m: provider.Manifest{ID: "a", Timeout: time.Second}},
		&mockProv{m: provider.Manifest{ID: "b", Timeout: time.Second}},
	}, DefaultRetryPolicy(), func(ctx context.Context, p provider.Provider) (string, error) { n++; return p.Manifest().ID, nil })
	if err != nil || data != "b" || meta == nil || !meta.FallbackUsed || n != 1 {
		t.Fatalf("data=%q meta=%+v calls=%d err=%v", data, meta, n, err)
	}
}
