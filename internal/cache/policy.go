package cache

import (
	"sync"
	"time"
)

type CacheabilityType string

const (
	Cacheable              CacheabilityType = "CACHEABLE"
	ConditionallyCacheable CacheabilityType = "CONDITIONALLY_CACHEABLE"
	NonCacheable           CacheabilityType = "NON_CACHEABLE"
)

// CapabilityPolicy defines the caching policy and stale boundaries for a specific capability.
type CapabilityPolicy struct {
	Capability                 string
	Type                       CacheabilityType
	DefaultTTL                 time.Duration
	MaxTTL                     time.Duration
	StaleWhileRevalidateWindow time.Duration
	StaleIfErrorWindow         time.Duration
	DisabledProviders          map[string]bool // Explicitly disallowed providers
}

// PolicyEvaluation contains the evaluated cache parameters for a request.
type PolicyEvaluation struct {
	IsCacheable                bool
	FreshTTL                   time.Duration
	StaleTTL                   time.Duration
	StaleWhileRevalidateWindow time.Duration
	StaleIfErrorWindow         time.Duration
	AllowStaleWhileRevalidate  bool
	AllowStaleIfError          bool
}

// PolicyEngine manages capability-aware and provider-aware caching rules.
type PolicyEngine struct {
	mu       sync.RWMutex
	policies map[string]*CapabilityPolicy
}

// NewPolicyEngine creates a policy engine initialized with production default policies.
func NewPolicyEngine() *PolicyEngine {
	pe := &PolicyEngine{
		policies: make(map[string]*CapabilityPolicy),
	}

	// Default Weather Policy: 10-minute fresh TTL, 5-min stale-while-revalidate, 30-min stale-if-error
	pe.RegisterPolicy(&CapabilityPolicy{
		Capability:                 "weather",
		Type:                       ConditionallyCacheable,
		DefaultTTL:                 10 * time.Minute,
		MaxTTL:                     30 * time.Minute,
		StaleWhileRevalidateWindow: 5 * time.Minute,
		StaleIfErrorWindow:         30 * time.Minute,
		DisabledProviders:          make(map[string]bool),
	})

	// Default Geocode Policy: 24-hour fresh TTL, 6-hour stale-while-revalidate, 72-hour stale-if-error
	pe.RegisterPolicy(&CapabilityPolicy{
		Capability:                 "geocode",
		Type:                       Cacheable,
		DefaultTTL:                 24 * time.Hour,
		MaxTTL:                     72 * time.Hour,
		StaleWhileRevalidateWindow: 6 * time.Hour,
		StaleIfErrorWindow:         72 * time.Hour,
		DisabledProviders:          make(map[string]bool),
	})

	// Forecasts are time-sensitive predictions, not durable observations.
	// For now, fresh-only storage prevents stale forecasts from bypassing
	// provider changes. A provider's explicit origin TTL is a further ceiling.
	pe.RegisterPolicy(&CapabilityPolicy{
		Capability: "forecast", Type: ConditionallyCacheable,
		DefaultTTL: 2 * time.Minute, MaxTTL: 10 * time.Minute,
		DisabledProviders: make(map[string]bool),
	})
	return pe
}

// RegisterPolicy registers or updates a capability caching policy.
func (pe *PolicyEngine) RegisterPolicy(policy *CapabilityPolicy) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.policies[policy.Capability] = policy
}

// Evaluate determines whether a capability request is cacheable, returning (isCacheable, freshTTL).
func (pe *PolicyEngine) Evaluate(capability string, providerID string) (bool, time.Duration) {
	eval := pe.EvaluatePolicy(capability, providerID)
	return eval.IsCacheable, eval.FreshTTL
}

// EvaluatePolicy returns full policy evaluation including stale boundaries.
func (pe *PolicyEngine) EvaluatePolicy(capability string, providerID string) PolicyEvaluation {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	policy, exists := pe.policies[capability]
	if !exists || policy.Type == NonCacheable {
		return PolicyEvaluation{IsCacheable: false}
	}

	// Check if this specific provider has caching disabled
	if policy.DisabledProviders != nil && policy.DisabledProviders[providerID] {
		return PolicyEvaluation{IsCacheable: false}
	}

	freshTTL := policy.DefaultTTL
	if freshTTL <= 0 {
		freshTTL = 5 * time.Minute
	}
	if policy.MaxTTL > 0 && freshTTL > policy.MaxTTL {
		freshTTL = policy.MaxTTL
	}

	// Stale window is the maximum of SWR window and Stale-If-Error window added to freshTTL
	maxStaleWindow := policy.StaleWhileRevalidateWindow
	if policy.StaleIfErrorWindow > maxStaleWindow {
		maxStaleWindow = policy.StaleIfErrorWindow
	}
	staleTTL := freshTTL + maxStaleWindow

	return PolicyEvaluation{
		IsCacheable:                true,
		FreshTTL:                   freshTTL,
		StaleTTL:                   staleTTL,
		StaleWhileRevalidateWindow: policy.StaleWhileRevalidateWindow,
		StaleIfErrorWindow:         policy.StaleIfErrorWindow,
		AllowStaleWhileRevalidate:  policy.StaleWhileRevalidateWindow > 0,
		AllowStaleIfError:          policy.StaleIfErrorWindow > 0,
	}
}

// SetProviderDisabled disables caching for a specific provider within a capability.
func (pe *PolicyEngine) SetProviderDisabled(capability string, providerID string, disabled bool) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if policy, exists := pe.policies[capability]; exists {
		if policy.DisabledProviders == nil {
			policy.DisabledProviders = make(map[string]bool)
		}
		if disabled {
			policy.DisabledProviders[providerID] = true
		} else {
			delete(policy.DisabledProviders, providerID)
		}
	}
}

// CanServeSWR checks the SWR deadline separately from the longer SIE retention
// deadline. The underlying Store holds an item until the later deadline.
func (p PolicyEvaluation) CanServeSWR(entry *Entry, now time.Time) bool {
	return entry != nil && entry.State == StateStale && !entry.FreshUntil.IsZero() &&
		p.StaleWhileRevalidateWindow > 0 && now.Before(entry.FreshUntil.Add(p.StaleWhileRevalidateWindow))
}

// CanServeSIE serves retained stale data only after an upstream error, within
// the independently configured stale-if-error window.
func (p PolicyEvaluation) CanServeSIE(entry *Entry, now time.Time) bool {
	return entry != nil && entry.State == StateStale && !entry.FreshUntil.IsZero() &&
		p.StaleIfErrorWindow > 0 && now.Before(entry.FreshUntil.Add(p.StaleIfErrorWindow))
}
