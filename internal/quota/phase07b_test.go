package quota

import (
	"context"
	"github.com/universal-api/platform/internal/quotactx"
	"testing"
)

func TestPhase07B_SharedFractionalProviderQuota(t *testing.T) {
	raw := `{
   "open_meteo_forecast":{"group":"open_meteo_shared","unit":"weighted_calls","per_day":3,"cost_milli":1600,"product_costs_milli":{"hourly_forecast":1600}},
   "open_meteo_geocoding":{"group":"open_meteo_shared","unit":"weighted_calls","per_day":3,"cost_milli":1000}
 }`
	budgets, err := ParseBudgets(raw)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(budgets, nil)
	ctx := quotactx.WithProduct(context.Background(), "hourly_forecast")
	if yes, err := manager.AllowAttempt(ctx, "open_meteo_forecast"); err != nil || !yes {
		t.Fatalf("first weighted unit: %v %v", yes, err)
	}
	if yes, err := manager.AllowAttempt(ctx, "open_meteo_forecast"); err != nil || yes {
		t.Fatalf("should reject second 1.6 call: %v %v", yes, err)
	}
	if yes, err := manager.AllowAttempt(context.Background(), "open_meteo_geocoding"); err != nil || !yes {
		t.Fatalf("shared geocoding cost: %v %v", yes, err)
	}
	if yes, err := manager.AllowAttempt(context.Background(), "open_meteo_geocoding"); err != nil || yes {
		t.Fatalf("shared group exceeded 3 units: %v %v", yes, err)
	}
}
func TestPhase07B_QuotaConfigSafety(t *testing.T) {
	for _, raw := range []string{
		`{"a":{"unit":"weighted_calls","per_day":100}}`,
		`{"a":{"group":"q","per_day":3},"b":{"group":"q","per_day":5}}`,
		`{"a":{"per_day":1,"cost_milli":-1}}`,
		`{"a":{"per_day":1,"product_costs_milli":{"":1000}}}`,
		`{"a":{"per_day":1,"per_second":10000000000000000}}`,
		`{"a":{"per_day":1,"cost_mili":1500}}`,
		`{"a":{"per_day":1,"unit":"requests","cost_milli":200}}`,
		`{"a":{"per_day":1,"unit":"credits","product_costs_milli":{"forward_geocoding":300}}}`,
		`{"a":{"per_day":1}} {"b":{"per_day":2}}`,
	} {
		if _, err := ParseBudgets(raw); err == nil {
			t.Fatalf("accepted invalid quota %s", raw)
		}
	}
}
func TestPhase07B_CreditsPerAttempt(t *testing.T) {
	m := NewManager(map[string]Budget{"geoapify": {Unit: "credits", PerSecond: 2, PerDay: 2}}, nil)
	for i := 0; i < 3; i++ {
		ok, err := m.AllowAttempt(context.Background(), "geoapify")
		if err != nil || ok != (i < 2) {
			t.Fatalf("iteration %d allowed=%v err=%v", i, ok, err)
		}
	}
}
