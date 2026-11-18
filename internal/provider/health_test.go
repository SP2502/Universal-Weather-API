package provider

import (
	"testing"
)

func TestHealthEvaluator_Scoring(t *testing.T) {
	eval := NewHealthEvaluator()

	// Initial score should be 1.0
	initHealth := eval.GetHealth("provider-a")
	if initHealth.Score != 1.0 {
		t.Fatalf("expected initial score 1.0, got %f", initHealth.Score)
	}

	// Record success
	eval.RecordSuccess("provider-a", 45.0)
	h := eval.GetHealth("provider-a")
	if h.SuccessCount != 1 || h.Score != 1.0 {
		t.Fatalf("expected 1 success and score 1.0, got %d, score %f", h.SuccessCount, h.Score)
	}

	// Record failures
	eval.RecordFailure("provider-a", false)
	eval.RecordFailure("provider-a", true)

	h = eval.GetHealth("provider-a")
	if h.FailureCount != 2 || h.TimeoutCount != 1 {
		t.Fatalf("expected 2 failures and 1 timeout")
	}

	// Update Circuit state to OPEN
	eval.UpdateCircuitState("provider-a", "OPEN")
	hOpen := eval.GetHealth("provider-a")
	if hOpen.Score >= 0.5 {
		t.Fatalf("expected degraded score for OPEN circuit breaker, got %f", hOpen.Score)
	}
}

type dummyProvider struct {
	manifest Manifest
}

func (d *dummyProvider) Manifest() Manifest {
	return d.manifest
}

func TestPolicy_FilterEligible(t *testing.T) {
	eval := NewHealthEvaluator()

	pActive := &dummyProvider{
		manifest: Manifest{ID: "active-1", Status: StatusActive},
	}
	pDegraded := &dummyProvider{
		manifest: Manifest{ID: "degraded-1", Status: StatusDegraded},
	}
	pDisabled := &dummyProvider{
		manifest: Manifest{ID: "disabled-1", Status: StatusDisabled},
	}

	eval.RecordSuccess("active-1", 10.0)
	eval.RecordFailure("degraded-1", false)

	providers := []Provider{pActive, pDegraded, pDisabled}

	// Allow degraded
	policy := Policy{AllowDegraded: true, MinHealthScore: 0.1}
	filtered := FilterEligible(providers, eval, policy)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 eligible providers, got %d", len(filtered))
	}

	// Disallow degraded
	policyStrict := Policy{AllowDegraded: false, MinHealthScore: 0.1}
	filteredStrict := FilterEligible(providers, eval, policyStrict)
	if len(filteredStrict) != 1 || filteredStrict[0].Manifest().ID != "active-1" {
		t.Fatalf("expected only active-1 provider")
	}
}
