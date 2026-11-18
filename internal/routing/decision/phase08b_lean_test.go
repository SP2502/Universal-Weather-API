package decision

import (
	"context"
	"testing"
)

func TestPhase08BLeanPriorityUsesRealConfig(t *testing.T) {
	// Existing synthetic adapter registry supplies real manifest priorities.
	reg, _ := setupTestRegistry()
	e := NewDecisionEngine(reg, reg.HealthEvaluator())
	e.SetLeanRanking(true)
	ps, ex, err := e.SelectProviders(context.Background(), RequestContext{Capability: "weather", PolicyMode: PolicyCostFirst})
	if err != nil || len(ps) < 2 {
		t.Fatalf("missing providers %v", err)
	}
	if ps[0].Manifest().FallbackPriority > ps[1].Manifest().FallbackPriority {
		t.Fatalf("lean cost-mode used fictional price: %v", ex.OrderedPlan)
	}
}
