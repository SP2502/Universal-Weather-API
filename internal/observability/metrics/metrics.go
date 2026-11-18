package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType defines the Prometheus metric type.
type MetricType string

const (
	TypeCounter   MetricType = "counter"
	TypeGauge     MetricType = "gauge"
	TypeHistogram MetricType = "histogram"
)

// Label represents a key-value metric label.
type Label struct {
	Name  string
	Value string
}

func sanitizeLabel(s string) string {
	// Most labels are fixed identifiers. Avoid three replacement passes and
	// intermediary allocations for those common safe values.
	if !strings.ContainsAny(s, "\\\"\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func labelsKey(labels []Label) string {
	if len(labels) == 0 {
		return ""
	}
	// Common metrics carry at most four labels. Use a fixed stack buffer and
	// a stable insertion sort; keep the general fallback for custom metrics.
	var small [8]Label
	var sorted []Label
	if len(labels) <= len(small) {
		sorted = small[:len(labels)]
	} else {
		sorted = make([]Label, len(labels))
	}
	copy(sorted, labels)
	for i := 1; i < len(sorted); i++ {
		v := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j].Name > v.Name {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = v
	}
	capacity := 0
	for _, label := range sorted {
		capacity += len(label.Name) + len(label.Value) + 5
	}
	var sb strings.Builder
	sb.Grow(capacity)
	for i, l := range sorted {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(l.Name)
		sb.WriteString("=\"")
		sb.WriteString(sanitizeLabel(l.Value))
		sb.WriteByte('"')
	}
	return sb.String()
}

// Counter is a monotonically increasing cumulative metric.
type Counter struct {
	name   string
	help   string
	values map[string]*atomic.Uint64
	mu     sync.RWMutex
}

func (c *Counter) Inc(labels ...Label) {
	c.Add(1, labels...)
}

func (c *Counter) Add(delta uint64, labels ...Label) {
	key := labelsKey(labels)
	c.mu.RLock()
	val, ok := c.values[key]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		val, ok = c.values[key]
		if !ok {
			val = &atomic.Uint64{}
			c.values[key] = val
		}
		c.mu.Unlock()
	}

	val.Add(delta)
}

func (c *Counter) Value(labels ...Label) uint64 {
	key := labelsKey(labels)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if val, ok := c.values[key]; ok {
		return val.Load()
	}
	return 0
}

// Gauge represents a numerical value that can arbitrarily go up and down.
type Gauge struct {
	name   string
	help   string
	values map[string]*int64
	mu     sync.RWMutex
}

func (g *Gauge) Set(val int64, labels ...Label) {
	key := labelsKey(labels)
	g.mu.Lock()
	defer g.mu.Unlock()
	v := val
	g.values[key] = &v
}

func (g *Gauge) Inc(labels ...Label) {
	g.Add(1, labels...)
}

func (g *Gauge) Dec(labels ...Label) {
	g.Add(-1, labels...)
}

func (g *Gauge) Add(delta int64, labels ...Label) {
	key := labelsKey(labels)
	g.mu.Lock()
	defer g.mu.Unlock()
	if curr, ok := g.values[key]; ok {
		*curr += delta
	} else {
		v := delta
		g.values[key] = &v
	}
}

func (g *Gauge) Value(labels ...Label) int64 {
	key := labelsKey(labels)
	g.mu.RLock()
	defer g.mu.RUnlock()
	if val, ok := g.values[key]; ok {
		return *val
	}
	return 0
}

// Histogram tracks the size and distribution of events with cumulative buckets.
type Histogram struct {
	name    string
	help    string
	buckets []float64
	counts  map[string][]uint64
	sums    map[string]float64
	totals  map[string]uint64
	mu      sync.RWMutex
}

func (h *Histogram) Observe(val float64, labels ...Label) {
	key := labelsKey(labels)
	h.mu.Lock()
	defer h.mu.Unlock()

	bucketCounts, ok := h.counts[key]
	if !ok {
		bucketCounts = make([]uint64, len(h.buckets))
		h.counts[key] = bucketCounts
	}

	for i, upper := range h.buckets {
		if val <= upper {
			bucketCounts[i]++
		}
	}

	h.sums[key] += val
	h.totals[key]++
}

// Registry manages all platform metrics and renders them in Prometheus text format.
type Registry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

func (r *Registry) NewCounter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.counters[name]; ok {
		return c
	}

	c := &Counter{
		name:   name,
		help:   help,
		values: make(map[string]*atomic.Uint64),
	}
	r.counters[name] = c
	return c
}

func (r *Registry) NewGauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()

	if g, ok := r.gauges[name]; ok {
		return g
	}

	g := &Gauge{
		name:   name,
		help:   help,
		values: make(map[string]*int64),
	}
	r.gauges[name] = g
	return g
}

func (r *Registry) NewHistogram(name, help string, buckets []float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.histograms[name]; ok {
		return h
	}

	if len(buckets) == 0 {
		// Default standard latency buckets in seconds
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}
	}

	sortedBuckets := make([]float64, len(buckets))
	copy(sortedBuckets, buckets)
	sort.Float64s(sortedBuckets)

	h := &Histogram{
		name:    name,
		help:    help,
		buckets: sortedBuckets,
		counts:  make(map[string][]uint64),
		sums:    make(map[string]float64),
		totals:  make(map[string]uint64),
	}
	r.histograms[name] = h
	return h
}

// Gather gathers and renders all metrics in Prometheus text exposition format (0.0.4).
func (r *Registry) Gather() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb strings.Builder

	// Render Counters
	var counterNames []string
	for name := range r.counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)

	for _, name := range counterNames {
		c := r.counters[name]
		c.mu.RLock()
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", c.name, c.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n", c.name))

		var labelKeys []string
		for k := range c.values {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)

		if len(labelKeys) == 0 {
			sb.WriteString(fmt.Sprintf("%s 0\n", c.name))
		} else {
			for _, k := range labelKeys {
				val := c.values[k].Load()
				if k != "" {
					sb.WriteString(fmt.Sprintf("%s{%s} %d\n", c.name, k, val))
				} else {
					sb.WriteString(fmt.Sprintf("%s %d\n", c.name, val))
				}
			}
		}
		c.mu.RUnlock()
	}

	// Render Gauges
	var gaugeNames []string
	for name := range r.gauges {
		gaugeNames = append(gaugeNames, name)
	}
	sort.Strings(gaugeNames)

	for _, name := range gaugeNames {
		g := r.gauges[name]
		g.mu.RLock()
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", g.name, g.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", g.name))

		var labelKeys []string
		for k := range g.values {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)

		if len(labelKeys) == 0 {
			sb.WriteString(fmt.Sprintf("%s 0\n", g.name))
		} else {
			for _, k := range labelKeys {
				val := *g.values[k]
				if k != "" {
					sb.WriteString(fmt.Sprintf("%s{%s} %d\n", g.name, k, val))
				} else {
					sb.WriteString(fmt.Sprintf("%s %d\n", g.name, val))
				}
			}
		}
		g.mu.RUnlock()
	}

	// Render Histograms
	var histNames []string
	for name := range r.histograms {
		histNames = append(histNames, name)
	}
	sort.Strings(histNames)

	for _, name := range histNames {
		h := r.histograms[name]
		h.mu.RLock()
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", h.name, h.help))
		sb.WriteString(fmt.Sprintf("# TYPE %s histogram\n", h.name))

		var labelKeys []string
		for k := range h.counts {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)

		for _, k := range labelKeys {
			bucketCounts := h.counts[k]
			sum := h.sums[k]
			count := h.totals[k]

			prefix := h.name + "_bucket"
			for i, upper := range h.buckets {
				var leLabel string
				if k != "" {
					leLabel = fmt.Sprintf("%s,le=\"%g\"", k, upper)
				} else {
					leLabel = fmt.Sprintf("le=\"%g\"", upper)
				}
				sb.WriteString(fmt.Sprintf("%s{%s} %d\n", prefix, leLabel, bucketCounts[i]))
			}

			// +Inf bucket
			var infLabel string
			if k != "" {
				infLabel = fmt.Sprintf("%s,le=\"+Inf\"", k)
			} else {
				infLabel = "le=\"+Inf\""
			}
			sb.WriteString(fmt.Sprintf("%s{%s} %d\n", prefix, infLabel, count))

			// Sum and Count
			if k != "" {
				sb.WriteString(fmt.Sprintf("%s_sum{%s} %g\n", h.name, k, sum))
				sb.WriteString(fmt.Sprintf("%s_count{%s} %d\n", h.name, k, count))
			} else {
				sb.WriteString(fmt.Sprintf("%s_sum %g\n", h.name, sum))
				sb.WriteString(fmt.Sprintf("%s_count %d\n", h.name, count))
			}
		}
		h.mu.RUnlock()
	}

	return sb.String()
}

// Global metric collectors
type PlatformMetrics struct {
	Registry *Registry

	// Gateway level
	HTTPRequestsTotal   *Counter
	HTTPRequestDuration *Histogram

	// Provider level
	ProviderRequestsTotal   *Counter
	ProviderRequestDuration *Histogram
	CircuitBreakerState     *Gauge
	BulkheadActiveSlots     *Gauge

	// Normalization
	NormalizationErrorsTotal *Counter

	// Security
	AuthFailuresTotal    *Counter
	RateLimitEventsTotal *Counter

	// Routing & Decision Engine
	ProviderRoutingDecisionsTotal *Counter
	ProviderRoutingFailuresTotal  *Counter

	// Cache
	CacheRequestsTotal       *Counter
	CacheHitsTotal           *Counter
	CacheMissesTotal         *Counter
	CacheStaleHitsTotal      *Counter
	CacheErrorsTotal         *Counter
	CacheEvictionsTotal      *Counter
	CacheEntries             *Gauge
	CacheRefreshTotal        *Counter
	CacheRefreshErrorsTotal  *Counter
	CacheSingleflightTotal   *Counter
	CacheSingleflightWaiters *Counter
}

func NewPlatformMetrics() *PlatformMetrics {
	reg := NewRegistry()

	latencyBuckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0}

	return &PlatformMetrics{
		Registry: reg,
		HTTPRequestsTotal: reg.NewCounter(
			"uap_http_requests_total",
			"Total number of HTTP requests processed by the gateway",
		),
		HTTPRequestDuration: reg.NewHistogram(
			"uap_http_request_duration_seconds",
			"Duration of HTTP requests processed by the gateway",
			latencyBuckets,
		),
		ProviderRequestsTotal: reg.NewCounter(
			"uap_provider_requests_total",
			"Total number of upstream provider execution attempts",
		),
		ProviderRequestDuration: reg.NewHistogram(
			"uap_provider_request_duration_seconds",
			"Duration of upstream provider execution attempts",
			latencyBuckets,
		),
		CircuitBreakerState: reg.NewGauge(
			"uap_circuit_breaker_state",
			"Circuit breaker state per provider (0=CLOSED, 1=HALF_OPEN, 2=OPEN)",
		),
		BulkheadActiveSlots: reg.NewGauge(
			"uap_bulkhead_active_slots",
			"Currently occupied bulkhead worker slots per provider",
		),
		NormalizationErrorsTotal: reg.NewCounter(
			"uap_normalization_errors_total",
			"Total number of response normalization failures",
		),
		AuthFailuresTotal: reg.NewCounter(
			"uap_auth_failures_total",
			"Total number of API key authentication failures",
		),
		RateLimitEventsTotal: reg.NewCounter(
			"uap_rate_limit_events_total",
			"Total number of rate limiting 429 events",
		),
		ProviderRoutingDecisionsTotal: reg.NewCounter(
			"uap_provider_routing_decisions_total",
			"Total number of provider routing decisions by capability and policy",
		),
		ProviderRoutingFailuresTotal: reg.NewCounter(
			"uap_provider_routing_failures_total",
			"Total number of routing decision failures due to ineligibility or constraints",
		),
		CacheRequestsTotal: reg.NewCounter(
			"uap_cache_requests_total",
			"Total number of capability cache lookups",
		),
		CacheHitsTotal: reg.NewCounter(
			"uap_cache_hits_total",
			"Total number of capability fresh cache hits",
		),
		CacheMissesTotal: reg.NewCounter(
			"uap_cache_misses_total",
			"Total number of capability cache misses",
		),
		CacheStaleHitsTotal: reg.NewCounter(
			"uap_cache_stale_hits_total",
			"Total number of stale cache hits served under SWR or Stale-If-Error",
		),
		CacheErrorsTotal: reg.NewCounter(
			"uap_cache_errors_total",
			"Total number of cache read/write errors (fail-open)",
		),
		CacheEvictionsTotal: reg.NewCounter(
			"uap_cache_evictions_total",
			"Total number of cache item evictions due to TTL or LRU capacity",
		),
		CacheEntries: reg.NewGauge(
			"uap_cache_entries",
			"Current number of cached entries",
		),
		CacheRefreshTotal: reg.NewCounter(
			"uap_cache_refresh_total",
			"Total number of asynchronous stale-while-revalidate background refreshes triggered",
		),
		CacheRefreshErrorsTotal: reg.NewCounter(
			"uap_cache_refresh_errors_total",
			"Total number of asynchronous background refresh execution errors",
		),
		CacheSingleflightTotal: reg.NewCounter(
			"uap_cache_singleflight_total",
			"Total number of distinct in-flight singleflight coalesced executions",
		),
		CacheSingleflightWaiters: reg.NewCounter(
			"uap_cache_singleflight_waiters",
			"Total number of concurrent requests coalesced to an in-flight execution",
		),
	}
}

// Default global platform metrics singleton
var DefaultMetrics = NewPlatformMetrics()

// Helper to record gateway duration
func SinceInSeconds(start time.Time) float64 {
	return time.Since(start).Seconds()
}
