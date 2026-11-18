package reliability

import "testing"

func TestBulkhead_ConcurrencyLimit(t *testing.T) {
	bh := NewBulkhead(2)

	if err := bh.Acquire(); err != nil {
		t.Fatalf("expected 1st acquire success: %v", err)
	}
	if err := bh.Acquire(); err != nil {
		t.Fatalf("expected 2nd acquire success: %v", err)
	}

	// 3rd acquire -> Full
	if err := bh.Acquire(); err != ErrBulkheadFull {
		t.Errorf("expected ErrBulkheadFull, got %v", err)
	}

	bh.Release()
	if err := bh.Acquire(); err != nil {
		t.Errorf("expected acquire success after release: %v", err)
	}
}
