package health

import (
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/openweather"
	"testing"
)

func TestReadinessChecksProviderPolicy(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(openweather.NewAdapter("fake", "http://ow.mock"))
	checker := NewConfiguredProviderHealthChecker(reg).WithPolicyEligibility(func(id string) bool { return false })
	if _, _, ok := checker.Check(); ok {
		t.Fatal("readiness should fail when configured providers are policy-excluded")
	}
	checker.WithPolicyEligibility(func(id string) bool { return id == "openweather" })
	if _, _, ok := checker.Check(); !ok {
		t.Fatal("readiness should allow an eligible configured weather provider")
	}
}
