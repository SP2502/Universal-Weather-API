package health

import (
	"github.com/universal-api/platform/internal/provider"
	"testing"
)

func TestReadinessRequiresConfiguredWeather(t *testing.T) {
	reg := provider.NewRegistry()
	ch := NewConfiguredProviderHealthChecker(reg)
	_, _, ok := ch.Check()
	if ok {
		t.Fatal("empty registry must not be ready")
	}
}
