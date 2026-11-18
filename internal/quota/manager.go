// Package quota provides opt-in, fail-closed quota admission before outbound
// provider attempts. It counts conservative upstream request units, not user
// HTTP requests, and never promises accurate vendor billing for record- or
// weight-based products (those require provider-specific cost calculators).
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/quotactx"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/cache"
)

// PerSecond/PerMinute/PerDay are whole vendor accounting units.
// CostMilli and ProductCostsMilli are thousandths of one unit, allowing
// conservative fractional weighted charges without floating point drift.
// Group joins provider IDs sharing one upstream allowance/credential.
type Budget struct {
	PerSecond         int64            `json:"per_second,omitempty"`
	PerMinute         int64            `json:"per_minute"`
	PerDay            int64            `json:"per_day"`
	Unit              string           `json:"unit,omitempty"` // requests, credits, records, weighted_calls
	Group             string           `json:"group,omitempty"`
	CostMilli         int64            `json:"cost_milli,omitempty"`
	ProductCostsMilli map[string]int64 `json:"product_costs_milli,omitempty"`
}

type State struct {
	second     int64
	secondSlot int64
	minute     int64
	day        int64
	minuteSlot int64
	daySlot    int64
}

type Manager struct {
	redis         *cache.RedisStore
	budgets       map[string]Budget
	mu            sync.Mutex
	local         map[string]State
	strictUnknown bool
}

const unitScale int64 = 1000
const maxBudgetUnits int64 = 1_000_000_000_000

var safeIdentity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var safeProduct = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func quotaGroup(providerID string, b Budget) string {
	if b.Group != "" {
		return b.Group
	}
	return providerID
}

func budgetCost(b Budget, product string) int64 {
	if v, ok := b.ProductCostsMilli[product]; ok {
		return v
	}
	if b.CostMilli > 0 {
		return b.CostMilli
	}
	if b.Unit == "records" || b.Unit == "weighted_calls" {
		return 0
	}
	return unitScale
}

func ParseBudgets(value string) (map[string]Budget, error) {
	out := map[string]Budget{}
	if strings.TrimSpace(value) == "" {
		return out, nil
	}
	if len(value) > 65536 {
		return nil, errors.New("provider quota JSON exceeds 64KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("provider quota JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("provider quota JSON contains trailing data")
	}
	// A group is one shared upstream allowance, and must not be given
	// conflicting window limits by different adapters.
	groups := map[string]Budget{}
	for id, b := range out {
		if b.Unit == "" {
			b.Unit = "requests"
			out[id] = b
		}
		if !safeIdentity.MatchString(id) || (b.Group != "" && !safeIdentity.MatchString(b.Group)) ||
			(b.PerSecond <= 0 && b.PerMinute <= 0 && b.PerDay <= 0) ||
			b.PerSecond < 0 || b.PerMinute < 0 || b.PerDay < 0 ||
			b.PerSecond > maxBudgetUnits || b.PerMinute > maxBudgetUnits || b.PerDay > maxBudgetUnits ||
			b.CostMilli < 0 || b.CostMilli > maxBudgetUnits ||
			(b.Unit != "" && b.Unit != "requests" && b.Unit != "credits" && b.Unit != "records" && b.Unit != "weighted_calls") {
			return nil, fmt.Errorf("invalid provider budget for %q", id)
		}
		if (b.Unit == "records" || b.Unit == "weighted_calls") && b.CostMilli == 0 && len(b.ProductCostsMilli) == 0 {
			return nil, fmt.Errorf("%s must define an operator-reviewed charge in milli-units", id)
		}
		if (b.Unit == "requests" || b.Unit == "credits") && b.CostMilli > 0 && b.CostMilli < unitScale {
			return nil, fmt.Errorf("%s requires at least one request/credit per admission", id)
		}
		for product, cost := range b.ProductCostsMilli {
			if (b.Unit == "requests" || b.Unit == "credits") && cost < unitScale {
				return nil, fmt.Errorf("%s undercounts request/credit cost for %q", id, product)
			}
			if !safeProduct.MatchString(product) || cost <= 0 || cost > maxBudgetUnits {
				return nil, fmt.Errorf("invalid per-product quota cost for %q", id)
			}
		}
		group := quotaGroup(id, b)
		if old, exists := groups[group]; exists {
			if old.PerSecond != b.PerSecond || old.PerMinute != b.PerMinute || old.PerDay != b.PerDay || old.Unit != b.Unit {
				return nil, fmt.Errorf("conflicting quotas for shared group %q", group)
			}
		} else {
			groups[group] = b
		}
	}
	return out, nil
}

func NewManager(budgets map[string]Budget, redis *cache.RedisStore) *Manager {
	copyBudgets := make(map[string]Budget, len(budgets))
	for k, b := range budgets {
		// A free Geoapify key must not exceed five upstream requests/sec.
		// Four is a conservative gateway-local cap allowing other consumers.
		if k == "geoapify" && b.PerSecond == 0 {
			b.PerSecond = 4
		}
		copyBudgets[k] = b
	}
	return &Manager{redis: redis, budgets: copyBudgets, local: map[string]State{}}
}

// SetStrictUnknown closes the dangerous implicit-unlimited provider path. Use
// this for any production/staging gateway; development embeddings can opt in.
func (m *Manager) SetStrictUnknown(strict bool) {
	if m != nil {
		m.strictUnknown = strict
	}
}

// HasBudget checks explicit configured budget presence without inspecting quota state.
func (m *Manager) HasBudget(id string) bool {
	if m == nil {
		return false
	}
	_, ok := m.budgets[id]
	return ok
}

// AllowAttempt is invoked before every outbound provider attempt including
// retries. Unknown providers are not throttled; production operators must
// configure budgets for every service they want hard limited.
func (m *Manager) AllowAttempt(ctx context.Context, providerID string) (bool, error) {
	if m == nil {
		return true, nil // Development only; production always injects a strict gate.
	}
	b, ok := m.budgets[providerID]
	if !ok {
		if m.strictUnknown {
			return false, ErrBudgetUnavailable
		}
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	cost := budgetCost(b, quotactx.Product(ctx))
	if cost <= 0 {
		return false, fmt.Errorf("invalid quota cost for %s", providerID)
	}
	group := quotaGroup(providerID, b)
	if m.redis != nil {
		// The scale and namespace are versioned: old request counters cannot
		// accidentally be reused as new fractional charge counters.
		return m.redis.ReserveBudgetWithSecond(ctx, "uap:providerquota:v2:"+group,
			b.PerSecond*unitScale, b.PerMinute*unitScale, b.PerDay*unitScale, cost)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	second, minute, day := now.Unix(), now.Unix()/60, now.Unix()/86400
	state := m.local[group]
	if state.secondSlot != second {
		state.secondSlot = second
		state.second = 0
	}
	if state.minuteSlot != minute {
		state.minuteSlot = minute
		state.minute = 0
	}
	if state.daySlot != day {
		state.daySlot = day
		state.day = 0
	}
	if (b.PerSecond > 0 && state.second > b.PerSecond*unitScale-cost) || (b.PerMinute > 0 && state.minute > b.PerMinute*unitScale-cost) || (b.PerDay > 0 && state.day > b.PerDay*unitScale-cost) {
		return false, nil
	}
	state.second += cost
	state.minute += cost
	state.day += cost
	m.local[group] = state
	return true, nil
}

var ErrBudgetExhausted = errors.New("provider request budget exhausted")
var ErrBudgetUnavailable = errors.New("provider request budget backend unavailable")
