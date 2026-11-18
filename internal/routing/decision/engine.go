package decision

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
)

var (
	ErrNoEligibleProviders = errors.New("decision engine: no eligible providers available for request")
)

type ProviderMetadata struct {
	CostClass       CostClass
	CoverageRegions []string // e.g. ["GLOBAL"], or ["GB", "US", "DE", "FR"]
}

// DecisionExplanation records why specific providers were selected or excluded for auditing and tracing.
type DecisionExplanation struct {
	Capability       string             `json:"capability"`
	PolicyMode       RoutingPolicyMode  `json:"policy_mode"`
	SelectedProvider string             `json:"selected_provider"`
	OrderedPlan      []string           `json:"ordered_plan"`
	CandidateScores  map[string]float64 `json:"candidate_scores"`
	ExclusionReasons map[string]string  `json:"exclusion_reasons"`
}

// DecisionEngine performs deterministic rule-based provider selection and ordering.
type DecisionEngine struct {
	mu               sync.RWMutex
	registry         *provider.Registry
	healthEvaluator  *provider.HealthEvaluator
	providerMetadata map[string]ProviderMetadata
	policyCatalog    *providerpolicy.Catalog
	deploymentUse    string
	countryIndex     *providerpolicy.CountryIndex
	leanRanking      bool
}

func NewDecisionEngine(registry *provider.Registry, healthEvaluator *provider.HealthEvaluator) *DecisionEngine {
	engine := &DecisionEngine{
		registry:         registry,
		healthEvaluator:  healthEvaluator,
		providerMetadata: make(map[string]ProviderMetadata),
	}

	// Initialize default provider metadata based on manifest profiles
	engine.providerMetadata["openweather"] = ProviderMetadata{
		CostClass:       CostClassStandard,
		CoverageRegions: []string{"GLOBAL"},
	}
	engine.providerMetadata["weatherapi"] = ProviderMetadata{
		CostClass:       CostClassStandard,
		CoverageRegions: []string{"GLOBAL"},
	}
	engine.providerMetadata["nominatim"] = ProviderMetadata{
		CostClass:       CostClassFree,
		CoverageRegions: []string{"GLOBAL"},
	}
	engine.providerMetadata["locationiq"] = ProviderMetadata{
		CostClass:       CostClassStandard,
		CoverageRegions: []string{"GLOBAL"},
	}

	return engine
}

// SetLeanRanking prioritizes operator priority and measured health only. No
// fictional latency/cost/rate-limit signals enter the v1 ranking.
func (e *DecisionEngine) SetLeanRanking(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.leanRanking = enabled
}

// SetProviderMetadata configures cost and geographic coverage attributes for a provider.
func (e *DecisionEngine) SetProviderMetadata(providerID string, meta ProviderMetadata) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providerMetadata[providerID] = meta
}

// SetPolicyCatalog enforces operator-reviewed product, deployment and geographic
// policy. Unknown providers are rejected when the catalog is installed.
func (e *DecisionEngine) SetPolicyCatalog(c *providerpolicy.Catalog, deploymentUse string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policyCatalog = c
	e.deploymentUse = deploymentUse
}

// SetCountryIndex installs an optional, operator-verified polygon lookup. It
// never infers a country from a city string, bounding box or IP address.
func (e *DecisionEngine) SetCountryIndex(index *providerpolicy.CountryIndex) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.countryIndex = index
}

func (e *DecisionEngine) policyRequest(req RequestContext) providerpolicy.Request {
	product := req.Product
	if product == "" {
		switch req.Capability {
		case "weather":
			product = providerpolicy.ProductCurrentWeather
		case "geocode":
			product = providerpolicy.ProductForwardGeocode
		}
	}
	country := req.CountryCode
	if country == "" && req.Latitude != nil && req.Longitude != nil && e.countryIndex != nil {
		country = e.countryIndex.Lookup(*req.Latitude, *req.Longitude)
	}
	return providerpolicy.Request{Product: product, DeploymentUse: e.deploymentUse, CountryCode: country, Latitude: req.Latitude, Longitude: req.Longitude}
}

// PolicyAllows is also used by cache reads so a warm response cannot evade
// geographic, product or deployment restrictions after policy changes.
func (e *DecisionEngine) PolicyAllows(req RequestContext, providerID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.registry == nil {
		return false
	}
	p, found := e.registry.Get(providerID)
	if !found {
		return false
	}
	m := p.Manifest()
	if m.Status == provider.StatusDisabled || m.Status == provider.StatusRemoved || (m.Status == provider.StatusDegraded && !req.AllowDegraded) {
		return false
	}
	supported := false
	for _, capability := range m.Capabilities {
		if capability == req.Capability {
			supported = true
		}
	}
	if !supported {
		return false
	}
	if req.PreferredProvider != "" && req.PreferredProvider != providerID {
		return false
	}
	if e.policyCatalog == nil {
		return true
	} // existing engine tests and in-memory embedding
	ok, _ := e.policyCatalog.Check(providerID, e.policyRequest(req))
	return ok
}

// SelectProviders evaluates candidates against request constraints and returns an ordered execution plan.
func (e *DecisionEngine) SelectProviders(ctx context.Context, req RequestContext) ([]provider.Provider, *DecisionExplanation, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	explanation := &DecisionExplanation{
		Capability:       req.Capability,
		PolicyMode:       req.PolicyMode,
		CandidateScores:  make(map[string]float64),
		ExclusionReasons: make(map[string]string),
	}

	if req.PolicyMode == "" {
		req.PolicyMode = PolicyAvailabilityFirst
		explanation.PolicyMode = req.PolicyMode
	}

	// 1. Gather all providers matching the capability
	var matching []provider.Provider
	if req.Capability == "weather" {
		wProviders, _ := e.registry.GetWeatherProviders(ctx)
		for _, wp := range wProviders {
			matching = append(matching, wp)
		}
	} else if req.Capability == "forecast" {
		fProviders, _ := e.registry.GetForecastProviders(ctx)
		for _, fp := range fProviders {
			matching = append(matching, fp)
		}
	} else if req.Capability == "air_quality" {
		aqProviders, _ := e.registry.GetAirQualityProviders(ctx)
		for _, aq := range aqProviders {
			matching = append(matching, aq)
		}
	} else if req.Capability == "history" || req.Capability == "astronomy" || req.Capability == "marine" || req.Capability == "alerts" {
		matching = e.registry.GetExtendedProviders(req.Capability)
	} else if req.Capability == "geocode" {
		gProviders, _ := e.registry.GetGeocodingProviders(ctx)
		for _, gp := range gProviders {
			matching = append(matching, gp)
		}
	} else {
		return nil, nil, fmt.Errorf("decision engine: unsupported capability %s", req.Capability)
	}

	if len(matching) == 0 {
		return nil, nil, ErrNoEligibleProviders
	}

	// 2. Hard Constraints & Eligibility Filtering
	var candidates []ProviderCandidate
	for _, p := range matching {
		m := p.Manifest()
		meta := e.providerMetadata[m.ID]
		if meta.CostClass == "" {
			meta.CostClass = CostClassStandard
		}
		if len(meta.CoverageRegions) == 0 {
			meta.CoverageRegions = []string{"GLOBAL"}
		}

		// (a) Preferred Provider Check (if specified, hard filter on ID)
		if req.PreferredProvider != "" && m.ID != req.PreferredProvider {
			explanation.ExclusionReasons[m.ID] = "excluded by preferred_provider request constraint"
			continue
		}

		// (b) Status Filter
		if m.Status == provider.StatusDisabled || m.Status == provider.StatusRemoved {
			explanation.ExclusionReasons[m.ID] = fmt.Sprintf("lifecycle status is %s", m.Status)
			continue
		}
		if m.Status == provider.StatusDegraded && !req.AllowDegraded {
			explanation.ExclusionReasons[m.ID] = "degraded status not allowed by request policy"
			continue
		}

		// Provider policy is evaluated before scoring: the first matching
		// provider is never allowed to override product/licence/coverage rules.
		if e.policyCatalog != nil {
			if allowed, reason := e.policyCatalog.Check(m.ID, e.policyRequest(req)); !allowed {
				explanation.ExclusionReasons[m.ID] = "policy: " + reason
				continue
			}
		}

		// (c) Health State & Circuit Breaker Filter
		health := provider.HealthState{Score: 1.0, CircuitState: "CLOSED"}
		if e.healthEvaluator != nil {
			health = e.healthEvaluator.GetHealth(m.ID)
		}

		// Circuit admission and cooldown belong exclusively to the executor.
		// Filtering stale OPEN health here prevents timed HALF_OPEN recovery forever.

		// (d) Geographic Coverage Filter
		if req.CountryCode != "" && !hasCoverage(meta.CoverageRegions, req.CountryCode) {
			explanation.ExclusionReasons[m.ID] = fmt.Sprintf("coverage does not include country %s", req.CountryCode)
			continue
		}

		// Calculate Failure Rate
		failureRate := 0.0
		if health.TotalRequests > 0 {
			failureRate = float64(health.FailureCount) / float64(health.TotalRequests)
		}

		candidates = append(candidates, ProviderCandidate{
			Provider:         p,
			ID:               m.ID,
			Name:             m.Name,
			Capability:       req.Capability,
			Status:           m.Status,
			FallbackPriority: m.FallbackPriority,
			HealthScore:      health.Score,
			CircuitState:     health.CircuitState,
			RecoveryReady:    health.CircuitState == "HALF_OPEN" || (health.CircuitState == "OPEN" && !health.NextProbeAt.IsZero() && !time.Now().Before(health.NextProbeAt)),
			AverageLatencyMs: health.AverageLatencyMs,
			FailureRate:      failureRate,
			IsRateLimited:    false, // updated dynamically on 429 feedback
			CostClass:        meta.CostClass,
			CoverageRegions:  meta.CoverageRegions,
		})
	}

	if len(candidates) == 0 {
		return nil, explanation, ErrNoEligibleProviders
	}

	// 3. Multi-Factor Scoring
	type scoredCandidate struct {
		candidate  ProviderCandidate
		score      float64
		rankPoints int64
	}

	scored := make([]scoredCandidate, len(candidates))
	for i, c := range candidates {
		score := e.calculateScore(c, req.PolicyMode)
		if e.leanRanking {
			// Real source priority and measured health only. A circuit nearing
			// recovery retains the existing executor-controlled probe bonus.
			score = 1000 - float64(c.FallbackPriority)*25
			if health := e.healthEvaluator; health != nil {
				s := health.GetHealth(c.ID)
				if s.TotalRequests >= 10 {
					score -= float64(s.FailureCount) / float64(s.TotalRequests) * 15
				}
			}
		}
		// Schedule a bounded recovery probe ahead of healthy fallbacks. Admission
		// still belongs to the executor: only it can grant a HALF_OPEN slot.
		if c.RecoveryReady {
			score += 200
		}
		// Compare fixed precision ranks rather than an epsilon tolerance.
		// Epsilon comparisons can be non-transitive (A ~= B, B ~= C,
		// but A != C), violating sort.Slice's strict ordering contract.
		rank := quantizeRoutingScore(score)
		scored[i] = scoredCandidate{candidate: c, score: score, rankPoints: rank}
		explanation.CandidateScores[c.ID] = float64(rank) / 100
	}

	// 4. Deterministic Sorting: quantized score descending ->
	// fallback priority ascending -> provider ID ascending.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].rankPoints != scored[j].rankPoints {
			return scored[i].rankPoints > scored[j].rankPoints
		}
		// Priority tie-breaker
		if scored[i].candidate.FallbackPriority != scored[j].candidate.FallbackPriority {
			return scored[i].candidate.FallbackPriority < scored[j].candidate.FallbackPriority
		}
		// Deterministic alphanumeric ID tie-breaker
		return scored[i].candidate.ID < scored[j].candidate.ID
	})

	// 5. Assemble final ordered provider execution plan
	result := make([]provider.Provider, len(scored))
	for i, s := range scored {
		result[i] = s.candidate.Provider
		explanation.OrderedPlan = append(explanation.OrderedPlan, s.candidate.ID)
	}

	if len(explanation.OrderedPlan) > 0 {
		explanation.SelectedProvider = explanation.OrderedPlan[0]
	}

	return result, explanation, nil
}

func (e *DecisionEngine) calculateScore(c ProviderCandidate, mode RoutingPolicyMode) float64 {
	// 1. Health Component [0, 100]
	healthComponent := c.HealthScore * 100.0

	// 2. Latency Component [0, 100]
	latencyComponent := 80.0 // neutral default
	if c.AverageLatencyMs > 0 {
		latencyComponent = 100.0 - math.Min(c.AverageLatencyMs/10.0, 100.0)
	}

	// 3. Priority Component [0, 100]: Lower priority number = higher score
	priorityComponent := 100.0 - float64((c.FallbackPriority-1)*20)
	if priorityComponent < 0 {
		priorityComponent = 0
	}

	// 4. Rate Limit Component [0, 100]
	rateLimitComponent := 100.0
	if c.IsRateLimited {
		rateLimitComponent = 0.0
	}

	// 5. Cost Component [0, 100]
	costComponent := 70.0
	switch c.CostClass {
	case CostClassFree:
		costComponent = 100.0
	case CostClassStandard:
		costComponent = 70.0
	case CostClassPremium:
		costComponent = 40.0
	}

	switch mode {
	case PolicyLatencyFirst:
		return (0.45 * latencyComponent) + (0.30 * healthComponent) + (0.15 * priorityComponent) + (0.10 * rateLimitComponent)
	case PolicyCostFirst:
		return (0.45 * costComponent) + (0.30 * healthComponent) + (0.15 * priorityComponent) + (0.10 * latencyComponent)
	case PolicyPriorityFirst:
		return (0.60 * priorityComponent) + (0.30 * healthComponent) + (0.10 * latencyComponent)
	case PolicyAvailabilityFirst:
		fallthrough
	default:
		return (0.45 * healthComponent) + (0.30 * priorityComponent) + (0.15 * rateLimitComponent) + (0.10 * latencyComponent)
	}
}

func hasCoverage(regions []string, countryCode string) bool {
	norm := strings.ToUpper(strings.TrimSpace(countryCode))
	for _, r := range regions {
		rUpper := strings.ToUpper(strings.TrimSpace(r))
		if rUpper == "GLOBAL" || rUpper == norm {
			return true
		}
	}
	return false
}

// quantizeRoutingScore makes the published two-decimal score the actual
// comparison value. This ensures a strict, transitive ordering without an
// epsilon comparison, independent of registry insertion order.
func quantizeRoutingScore(score float64) int64 {
	return int64(math.Round(score * 100))
}
