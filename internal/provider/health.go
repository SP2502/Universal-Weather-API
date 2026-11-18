package provider

import (
	"sync"
	"time"
)

// HealthState represents real-time operational metrics for a provider.
type HealthState struct {
	TotalRequests    int64     `json:"total_requests"`
	SuccessCount     int64     `json:"success_count"`
	FailureCount     int64     `json:"failure_count"`
	TimeoutCount     int64     `json:"timeout_count"`
	CircuitState     string    `json:"circuit_state"`           // CLOSED, HALF_OPEN, OPEN
	NextProbeAt      time.Time `json:"next_probe_at,omitempty"` // scheduling hint from executor
	AverageLatencyMs float64   `json:"average_latency_ms"`
	LastSeen         time.Time `json:"last_seen"`
	Score            float64   `json:"score"` // Computed health score: 0.0 (unhealthy) to 1.0 (optimal)
}

// HealthEvaluator computes and maintains real-time health metrics for registered providers.
type HealthEvaluator struct {
	mu     sync.RWMutex
	states map[string]*HealthState
}

func NewHealthEvaluator() *HealthEvaluator {
	return &HealthEvaluator{
		states: make(map[string]*HealthState),
	}
}

func (h *HealthEvaluator) getOrCreate(providerID string) *HealthState {
	s, exists := h.states[providerID]
	if !exists {
		s = &HealthState{
			CircuitState: "CLOSED",
			Score:        1.0,
			LastSeen:     time.Now().UTC(),
		}
		h.states[providerID] = s
	}
	return s
}

func (h *HealthEvaluator) RecordSuccess(providerID string, latencyMs float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.getOrCreate(providerID)
	s.TotalRequests++
	s.SuccessCount++
	s.LastSeen = time.Now().UTC()

	// Rolling average latency
	if s.AverageLatencyMs == 0 {
		s.AverageLatencyMs = latencyMs
	} else {
		s.AverageLatencyMs = (s.AverageLatencyMs * 0.8) + (latencyMs * 0.2)
	}

	h.recalculateScore(s)
}

func (h *HealthEvaluator) RecordFailure(providerID string, isTimeout bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.getOrCreate(providerID)
	s.TotalRequests++
	s.FailureCount++
	if isTimeout {
		s.TimeoutCount++
	}
	s.LastSeen = time.Now().UTC()

	h.recalculateScore(s)
}

func (h *HealthEvaluator) UpdateCircuitState(providerID string, circuitState string) {
	h.UpdateCircuitStateWithProbe(providerID, circuitState, time.Time{})
}

// Only the executor sets NextProbeAt from its real circuit timeout. This is a
// ranking hint; the decision engine must never grant admission itself.
func (h *HealthEvaluator) UpdateCircuitStateWithProbe(providerID string, circuitState string, nextProbeAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.getOrCreate(providerID)
	s.CircuitState = circuitState
	s.NextProbeAt = nextProbeAt
	h.recalculateScore(s)
}

func (h *HealthEvaluator) recalculateScore(s *HealthState) {
	if s.TotalRequests == 0 {
		s.Score = 1.0
		return
	}

	// 1. Success Ratio (weight 0.6)
	successRatio := float64(s.SuccessCount) / float64(s.TotalRequests)

	// 2. Circuit Breaker Weight (weight 0.4)
	circuitWeight := 1.0
	switch s.CircuitState {
	case "OPEN":
		circuitWeight = 0.0
	case "HALF_OPEN":
		circuitWeight = 0.5
	default:
		circuitWeight = 1.0
	}

	score := (successRatio * 0.6) + (circuitWeight * 0.4)
	if score < 0.0 {
		score = 0.0
	} else if score > 1.0 {
		score = 1.0
	}
	s.Score = score
}

func (h *HealthEvaluator) GetHealth(providerID string) HealthState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if s, ok := h.states[providerID]; ok {
		return *s
	}
	return HealthState{
		CircuitState: "CLOSED",
		Score:        1.0,
		LastSeen:     time.Now().UTC(),
	}
}

func (h *HealthEvaluator) GetAllHealth() map[string]HealthState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]HealthState, len(h.states))
	for id, s := range h.states {
		result[id] = *s
	}
	return result
}
