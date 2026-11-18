package health

import (
	"context"
	"fmt"
	"time"

	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/provider"
)

type DatabaseChecker struct {
	store database.Store
}

func NewDatabaseChecker(store database.Store) *DatabaseChecker {
	return &DatabaseChecker{store: store}
}

func (c *DatabaseChecker) Check() (string, map[string]string, bool) {
	if c.store == nil {
		return "DOWN", map[string]string{"database": "DOWN", "database_reason": "store is nil"}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := c.store.Ping(ctx); err != nil {
		return "DOWN", map[string]string{"database": "DOWN", "database_error": err.Error()}, false
	}

	return "UP", map[string]string{"database": "UP"}, true
}

type CacheChecker struct {
	store cache.Store
}

func NewCacheChecker(store cache.Store) *CacheChecker {
	return &CacheChecker{store: store}
}

func (c *CacheChecker) Check() (string, map[string]string, bool) {
	if c.store == nil {
		return "DOWN", map[string]string{"cache": "DOWN", "cache_reason": "cache store is nil"}, false
	}

	// For RedisStore, verify connection if possible
	if rs, ok := c.store.(*cache.RedisStore); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := rs.Ping(ctx); err != nil {
			if rs.AllowsFallback() {
				return "DEGRADED", map[string]string{"cache": "DEGRADED (FAIL-OPEN)"}, true
			}
			return "DOWN", map[string]string{"cache": "DOWN (REQUIRED)"}, false
		}
	}

	return "UP", map[string]string{"cache": "UP", "cache_entries": fmt.Sprintf("%d", c.store.Len())}, true
}

type ProviderHealthChecker struct {
	evaluator *provider.HealthEvaluator
}

func NewProviderHealthChecker(evaluator *provider.HealthEvaluator) *ProviderHealthChecker {
	return &ProviderHealthChecker{evaluator: evaluator}
}

func (c *ProviderHealthChecker) Check() (string, map[string]string, bool) {
	if c.evaluator == nil {
		return "UP", map[string]string{"providers": "UP"}, true
	}

	allHealth := c.evaluator.GetAllHealth()
	details := make(map[string]string)
	allHealthy := true

	for pID, state := range allHealth {
		details[fmt.Sprintf("provider_%s_circuit", pID)] = state.CircuitState
		details[fmt.Sprintf("provider_%s_score", pID)] = fmt.Sprintf("%.2f", state.Score)
		if state.CircuitState == "OPEN" {
			allHealthy = false
		}
	}

	if !allHealthy {
		details["providers"] = "DEGRADED"
		return "DEGRADED", details, true // Still allow readiness if at least fallback providers exist
	}

	details["providers"] = "UP"
	return "UP", details, true
}

// ConfiguredProviderHealthChecker verifies the service's required weather
// capability, rather than merely checking whether a passive evaluator exists.
// This does not replace a real upstream availability probe: it indicates that
// the gateway has at least one configured, non-OPEN weather candidate.
// ConfiguredProviderHealthChecker checks every required *capability* without
// making upstream requests (which may consume quotas). Forecast-only servers
// need not masquerade as having current observations.
type ConfiguredProviderHealthChecker struct {
	registry       *provider.Registry
	allowed        func(string) bool // legacy weather-only predicate
	productAllowed func(string, string) bool
	required       []string
}

func NewConfiguredProviderHealthChecker(reg *provider.Registry) *ConfiguredProviderHealthChecker {
	return &ConfiguredProviderHealthChecker{registry: reg, required: []string{"weather"}}
}

func (c *ConfiguredProviderHealthChecker) WithPolicyEligibility(fn func(string) bool) *ConfiguredProviderHealthChecker {
	c.allowed = fn
	return c
}

// WithRequiredCapabilities replaces the default weather requirement. Values
// are validated at gateway configuration ingress; this helper keeps other
// programmatic callers backward compatible.
func (c *ConfiguredProviderHealthChecker) WithRequiredCapabilities(required ...string) *ConfiguredProviderHealthChecker {
	c.required = append([]string(nil), required...)
	return c
}

func (c *ConfiguredProviderHealthChecker) WithProductEligibility(fn func(providerID, capability string) bool) *ConfiguredProviderHealthChecker {
	c.productAllowed = fn
	return c
}

func (c *ConfiguredProviderHealthChecker) Check() (string, map[string]string, bool) {
	details := map[string]string{}
	if c.registry == nil || len(c.required) == 0 {
		return "DOWN", map[string]string{"required_capabilities": "NOT_CONFIGURED"}, false
	}
	allReady := true
	for _, capability := range c.required {
		var candidates []provider.Provider
		switch capability {
		case "weather":
			ps, _ := c.registry.GetWeatherProviders(context.Background())
			for _, p := range ps {
				candidates = append(candidates, p)
			}
		case "forecast":
			ps, _ := c.registry.GetForecastProviders(context.Background())
			for _, p := range ps {
				candidates = append(candidates, p)
			}
		case "air_quality":
			ps, _ := c.registry.GetAirQualityProviders(context.Background())
			for _, p := range ps {
				candidates = append(candidates, p)
			}
		case "history", "astronomy", "marine", "alerts":
			candidates = c.registry.GetExtendedProviders(capability)
		default:
			details[capability+"_providers"] = "UNKNOWN_CAPABILITY"
			allReady = false
			continue
		}
		available := 0
		for _, p := range candidates {
			id := p.Manifest().ID
			if c.productAllowed != nil && !c.productAllowed(id, capability) {
				continue
			}
			if c.allowed != nil && !c.allowed(id) {
				continue
			}
			h := c.registry.HealthEvaluator().GetHealth(id)
			if h.CircuitState != "OPEN" {
				available++
			}
		}
		if available == 0 {
			details[capability+"_providers"] = "NOT_CONFIGURED_OR_UNAVAILABLE"
			allReady = false
		} else {
			details[capability+"_providers"] = "CONFIGURED"
			details[capability+"_candidates"] = fmt.Sprintf("%d", available)
		}
	}
	if !allReady {
		return "DOWN", details, false
	}
	return "UP", details, true
}
