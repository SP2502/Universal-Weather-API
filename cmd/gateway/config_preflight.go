package main

import (
	"errors"
	"fmt"

	"github.com/universal-api/platform/internal/config"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/quota"
	"github.com/universal-api/platform/internal/secrets"
	"os"
)

// configuredSecretStore matches startup resolution and is safe in offline
// preflight: merely constructing stores never connects to external services.
func configuredSecretStore(cfg *config.Config) (secrets.SecretStore, error) {
	fileStore, err := secrets.NewFileSecretStore(cfg.SecretsDir)
	if err != nil {
		return nil, err
	}
	envStore := secrets.NewEnvSecretStore("")
	memoryStore := secrets.NewMemorySecretStore()
	switch cfg.SecretDriver {
	case "file":
		return fileStore, nil
	case "env":
		return envStore, nil
	case "memory":
		return memoryStore, nil
	default:
		return secrets.NewChainedSecretStore(fileStore, envStore, memoryStore), nil
	}
}

// validateProviderQuotaCoverage is shared by actual startup and --check-config.
// redisReady means configured Redis in offline checks, but a real connection
// in runtime checks. This distinction is never represented as live validation.
func validateProviderQuotaCoverage(cfg *config.Config, registry *provider.Registry, budgets map[string]quota.Budget, redisReady bool) error {
	if cfg.Environment != "production" && cfg.Environment != "staging" {
		return nil
	}
	if !redisReady {
		return errors.New("production/staging requires Redis for shared upstream quotas")
	}
	if len(budgets) == 0 {
		return errors.New("production/staging requires explicit APP_PROVIDER_QUOTAS_JSON")
	}
	for _, id := range registry.IDs() {
		if _, ok := budgets[id]; !ok {
			return fmt.Errorf("missing production upstream quota budget for configured provider %q", id)
		}
	}
	return nil
}

// configuredPolicies is shared by offline preflight and the runtime engine.
func configuredPolicies() (*providerpolicy.Catalog, error) {
	if path := os.Getenv("APP_PROVIDER_POLICY_FILE"); path != "" {
		p, err := providerpolicy.LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("provider policy configuration: %w", err)
		}
		return p, nil
	}
	return providerpolicy.NewCatalog(providerpolicy.DefaultDocument())
}

func deploymentUse() string {
	if v := os.Getenv("APP_DEPLOYMENT_USE"); v != "" {
		return v
	}
	return "unspecified"
}

func configuredProviderChecker(registry *provider.Registry, policies *providerpolicy.Catalog, use string, required []string) *health.ConfiguredProviderHealthChecker {
	return health.NewConfiguredProviderHealthChecker(registry).
		WithRequiredCapabilities(required...).WithProductEligibility(func(id, capability string) bool {
		product := providerpolicy.ProductCurrentWeather
		switch capability {
		case "forecast":
			product = providerpolicy.ProductHourlyForecast
		case "air_quality":
			product = providerpolicy.ProductAirQualityForecast
		case "history":
			product = providerpolicy.ProductHistory
		case "astronomy":
			product = providerpolicy.ProductAstronomy
		case "marine":
			product = providerpolicy.ProductMarine
		case "alerts":
			product = providerpolicy.ProductAlerts
		}
		return policies.PotentiallyEligible(id, product, use)
	})
}
