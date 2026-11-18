package decision

import (
	"github.com/universal-api/platform/internal/provider"
)

type CostClass string

const (
	CostClassFree     CostClass = "FREE"
	CostClassStandard CostClass = "STANDARD"
	CostClassPremium  CostClass = "PREMIUM"
)

// ProviderCandidate encapsulates all routing and health signals for an upstream candidate.
type ProviderCandidate struct {
	Provider         provider.Provider
	ID               string
	Name             string
	Capability       string
	Status           provider.Status
	FallbackPriority int
	HealthScore      float64
	CircuitState     string
	RecoveryReady    bool // OPEN cooldown elapsed or HALF_OPEN probe outstanding
	AverageLatencyMs float64
	FailureRate      float64
	IsRateLimited    bool
	CostClass        CostClass
	CoverageRegions  []string // e.g. ["GLOBAL"] or ISO country codes ["GB", "US", "DE"]
}
