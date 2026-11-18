package quota

import (
	"context"
	"sync"
	"testing"
)

func TestPhase04_QuotaConfigValidation(t *testing.T) {
	for _, value := range []string{`{`, `{"a":{"per_minute":-1}}`, `{"a":{}}`} {
		if _, err := ParseBudgets(value); err == nil {
			t.Fatalf("accepted invalid budget %s", value)
		}
	}
	budgets, err := ParseBudgets(`{"openweather":{"per_minute":2,"per_day":5}}`)
	if err != nil || budgets["openweather"].PerMinute != 2 {
		t.Fatal(err)
	}
}
func TestPhase04_LocalProviderBudgetConcurrentAdmission(t *testing.T) {
	m := NewManager(map[string]Budget{"ow": {PerMinute: 5, PerDay: 10}}, nil)
	var wg sync.WaitGroup
	results := make(chan bool, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := m.AllowAttempt(context.Background(), "ow")
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	n := 0
	for ok := range results {
		if ok {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("granted %d attempts with a 5/minute limit", n)
	}
	ok, err := m.AllowAttempt(context.Background(), "unknown")
	if err != nil || !ok {
		t.Fatal("unknown providers should preserve existing behavior; operator must configure budget")
	}
}
