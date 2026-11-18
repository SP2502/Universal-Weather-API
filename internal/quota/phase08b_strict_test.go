package quota

import (
	"context"
	"errors"
	"testing"
)

func TestPhase08BStrictUnknownFailsClosed(t *testing.T) {
	m := NewManager(map[string]Budget{"known": {PerDay: 1}}, nil)
	m.SetStrictUnknown(true)
	if ok, err := m.AllowAttempt(context.Background(), "unknown"); ok || !errors.Is(err, ErrBudgetUnavailable) {
		t.Fatalf("unknown allowed: %v %v", ok, err)
	}
	if ok, err := m.AllowAttempt(context.Background(), "known"); !ok || err != nil {
		t.Fatalf("known first denied: %v %v", ok, err)
	}
	if ok, err := m.AllowAttempt(context.Background(), "known"); ok || err != nil {
		t.Fatalf("quota overrun: %v %v", ok, err)
	}
	if !m.HasBudget("known") || m.HasBudget("unknown") {
		t.Fatal("budget inventory broken")
	}
}
