package decision

import (
	"context"
	"testing"

	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

func setupTestRegistry() (*provider.Registry, *provider.HealthEvaluator) {
	registry := provider.NewRegistry()
	healthEvaluator := provider.NewHealthEvaluator()

	ow := openweather.NewAdapter("dummy-key", "http://ow.mock")
	wapi := weatherapi.NewAdapter("dummy-key", "http://wapi.mock")
	nom := nominatim.NewAdapter("http://nom.mock")
	liq := locationiq.NewAdapter("dummy-key", "http://liq.mock")

	registry.Register(ow)
	registry.Register(wapi)
	registry.Register(nom)
	registry.Register(liq)

	return registry, healthEvaluator
}

func TestDecisionEngine_HardFiltering(t *testing.T) {
	registry, healthEvaluator := setupTestRegistry()
	engine := NewDecisionEngine(registry, healthEvaluator)
	ctx := context.Background()

	// OPEN state is not a hard filter: the executor owns timed recovery.
	healthEvaluator.UpdateCircuitState("openweather", "OPEN")
	providers, explanation, err := engine.SelectProviders(ctx, RequestContext{
		Capability: "weather", PolicyMode: PolicyAvailabilityFirst,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, p := range providers {
		if p.Manifest().ID == "openweather" {
			found = true
		}
	}
	if !found {
		t.Error("OPEN candidate must reach executor for eventual HALF_OPEN recovery")
	}
	if explanation.ExclusionReasons["openweather"] != "" {
		t.Fatal("OPEN is not a permanent exclusion")
	}

	// 2. Preferred provider constraint
	providers, _, err = engine.SelectProviders(ctx, RequestContext{
		Capability:        "weather",
		PreferredProvider: "weatherapi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 1 || providers[0].Manifest().ID != "weatherapi" {
		t.Errorf("expected only weatherapi, got %d providers", len(providers))
	}
}

func TestDecisionEngine_GeographicCoverage(t *testing.T) {
	registry, healthEvaluator := setupTestRegistry()
	engine := NewDecisionEngine(registry, healthEvaluator)
	ctx := context.Background()

	// Configure nominatim as France-only, locationiq as Global
	engine.SetProviderMetadata("nominatim", ProviderMetadata{
		CostClass:       CostClassFree,
		CoverageRegions: []string{"FR"},
	})
	engine.SetProviderMetadata("locationiq", ProviderMetadata{
		CostClass:       CostClassStandard,
		CoverageRegions: []string{"GLOBAL"},
	})

	// Request for US -> Nominatim should be excluded
	providers, explanation, err := engine.SelectProviders(ctx, RequestContext{
		Capability:  "geocode",
		CountryCode: "US",
		PolicyMode:  PolicyAvailabilityFirst,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range providers {
		if p.Manifest().ID == "nominatim" {
			t.Errorf("nominatim should be excluded for country US")
		}
	}
	if explanation.ExclusionReasons["nominatim"] == "" {
		t.Errorf("expected exclusion reason for nominatim on country mismatch")
	}

	// Request for FR -> Nominatim should be included
	providersFR, _, err := engine.SelectProviders(ctx, RequestContext{
		Capability:  "geocode",
		CountryCode: "FR",
		PolicyMode:  PolicyAvailabilityFirst,
	})
	if err != nil {
		t.Fatalf("unexpected error for FR: %v", err)
	}
	foundNom := false
	for _, p := range providersFR {
		if p.Manifest().ID == "nominatim" {
			foundNom = true
		}
	}
	if !foundNom {
		t.Errorf("expected nominatim to be included for country FR")
	}
}

func TestDecisionEngine_PolicyModes(t *testing.T) {
	registry, healthEvaluator := setupTestRegistry()
	engine := NewDecisionEngine(registry, healthEvaluator)
	ctx := context.Background()

	// Simulate health & latency:
	// OpenWeather: fast latency (20ms), lower health (0.6)
	// WeatherAPI: slow latency (300ms), perfect health (1.0)
	healthEvaluator.RecordSuccess("openweather", 20.0)
	healthEvaluator.RecordFailure("openweather", false) // drops score
	healthEvaluator.RecordSuccess("weatherapi", 300.0)

	// 1. AvailabilityFirst -> WeatherAPI wins (higher health)
	providersAvail, _, _ := engine.SelectProviders(ctx, RequestContext{
		Capability: "weather",
		PolicyMode: PolicyAvailabilityFirst,
	})
	if len(providersAvail) < 2 || providersAvail[0].Manifest().ID != "weatherapi" {
		t.Errorf("expected weatherapi to win on AVAILABILITY_FIRST, got %s", providersAvail[0].Manifest().ID)
	}

	// 2. LatencyFirst -> OpenWeather wins (much faster latency: 20ms vs 300ms)
	providersLatency, _, _ := engine.SelectProviders(ctx, RequestContext{
		Capability: "weather",
		PolicyMode: PolicyLatencyFirst,
	})
	if len(providersLatency) < 2 || providersLatency[0].Manifest().ID != "openweather" {
		t.Errorf("expected openweather to win on LATENCY_FIRST, got %s", providersLatency[0].Manifest().ID)
	}
}

func TestDecisionEngine_DeterministicOrdering(t *testing.T) {
	registry, healthEvaluator := setupTestRegistry()
	engine := NewDecisionEngine(registry, healthEvaluator)
	ctx := context.Background()

	req := RequestContext{
		Capability: "geocode",
		PolicyMode: PolicyAvailabilityFirst,
	}

	var firstPlan []string
	for i := 0; i < 20; i++ {
		_, explanation, err := engine.SelectProviders(ctx, req)
		if err != nil {
			t.Fatalf("iteration %d error: %v", i, err)
		}
		if i == 0 {
			firstPlan = explanation.OrderedPlan
		} else {
			if len(explanation.OrderedPlan) != len(firstPlan) {
				t.Fatalf("iteration %d: plan length changed", i)
			}
			for j := range firstPlan {
				if explanation.OrderedPlan[j] != firstPlan[j] {
					t.Fatalf("iteration %d: non-deterministic ordering: %v vs %v", i, explanation.OrderedPlan, firstPlan)
				}
			}
		}
	}
}

func TestRoutingScoreQuantizationStrictOrdering(t *testing.T) {
	// Near-equal scores used to violate transitivity with an epsilon
	// comparator. Quantizing to the public two-decimal precision cannot.
	a, b, c := 74.0000, 74.0007, 74.0014
	qa, qb, qc := quantizeRoutingScore(a), quantizeRoutingScore(b), quantizeRoutingScore(c)
	if qa != qb || qb != qc {
		t.Fatalf("expected identical two-decimal scores: %d %d %d", qa, qb, qc)
	}
	if quantizeRoutingScore(74.006) <= qc {
		t.Fatal("distinct two-decimal score must sort above tied candidates")
	}
}
