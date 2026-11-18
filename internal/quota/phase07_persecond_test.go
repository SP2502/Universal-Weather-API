package quota

import (
	"context"
	"testing"
)

func TestGeoapifyConservativePerSecondCeiling(t *testing.T) {
	m := NewManager(map[string]Budget{"geoapify": {PerMinute: 60, PerDay: 100}}, nil)
	for i := 0; i < 6; i++ {
		ok, e := m.AllowAttempt(context.Background(), "geoapify")
		if e != nil {
			t.Fatal(e)
		}
		if ok != (i < 4) {
			t.Fatalf("attempt %d was %v, expected %v", i, ok, i < 4)
		}
	}
}
func TestParseExplicitPerSecondBudget(t *testing.T) {
	b, e := ParseBudgets(`{"geoapify":{"per_second":4,"per_minute":100,"per_day":2000}}`)
	if e != nil || b["geoapify"].PerSecond != 4 {
		t.Fatalf("invalid second budget: %v %+v", e, b)
	}
}
