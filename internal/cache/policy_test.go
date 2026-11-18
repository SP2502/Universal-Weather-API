package cache

import (
	"testing"
	"time"
)

func TestPolicyEngine_Evaluation(t *testing.T) {
	pe := NewPolicyEngine()

	// 1. Weather default
	cacheable, ttl := pe.Evaluate("weather", "openweather")
	if !cacheable || ttl != 10*time.Minute {
		t.Errorf("expected weather/openweather to be cacheable with 10m TTL, got (%v, %v)", cacheable, ttl)
	}

	// 2. Disable provider for weather
	pe.SetProviderDisabled("weather", "openweather", true)
	cacheable, ttl = pe.Evaluate("weather", "openweather")
	if cacheable || ttl != 0 {
		t.Errorf("expected weather/openweather to be disabled, got (%v, %v)", cacheable, ttl)
	}

	// 3. Other provider still cacheable
	cacheable, ttl = pe.Evaluate("weather", "weatherapi")
	if !cacheable || ttl != 10*time.Minute {
		t.Errorf("expected weather/weatherapi to remain cacheable, got (%v, %v)", cacheable, ttl)
	}

	// 4. Non-existent capability
	cacheable, _ = pe.Evaluate("unknown_cap", "any")
	if cacheable {
		t.Errorf("expected unknown capability to be non-cacheable")
	}
}
