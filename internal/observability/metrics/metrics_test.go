package metrics

import (
	"strings"
	"testing"
)

func TestMetrics_Counter(t *testing.T) {
	reg := NewRegistry()
	c := reg.NewCounter("test_requests_total", "Test counter help")

	c.Inc(Label{Name: "method", Value: "GET"}, Label{Name: "status", Value: "200"})
	c.Add(5, Label{Name: "method", Value: "GET"}, Label{Name: "status", Value: "200"})

	val := c.Value(Label{Name: "method", Value: "GET"}, Label{Name: "status", Value: "200"})
	if val != 6 {
		t.Fatalf("expected counter value 6, got %d", val)
	}

	// Non-existent labels return 0
	valZero := c.Value(Label{Name: "method", Value: "POST"})
	if valZero != 0 {
		t.Fatalf("expected 0 for unset labels, got %d", valZero)
	}
}

func TestMetrics_Gauge(t *testing.T) {
	reg := NewRegistry()
	g := reg.NewGauge("test_active_connections", "Test gauge help")

	g.Set(10, Label{Name: "pool", Value: "primary"})
	g.Inc(Label{Name: "pool", Value: "primary"})
	g.Dec(Label{Name: "pool", Value: "primary"})
	g.Add(5, Label{Name: "pool", Value: "primary"})

	val := g.Value(Label{Name: "pool", Value: "primary"})
	if val != 15 {
		t.Fatalf("expected gauge value 15, got %d", val)
	}
}

func TestMetrics_Histogram(t *testing.T) {
	reg := NewRegistry()
	h := reg.NewHistogram("test_duration_seconds", "Test histogram help", []float64{0.1, 0.5, 1.0})

	h.Observe(0.05, Label{Name: "handler", Value: "weather"})
	h.Observe(0.25, Label{Name: "handler", Value: "weather"})
	h.Observe(0.75, Label{Name: "handler", Value: "weather"})
	h.Observe(2.5, Label{Name: "handler", Value: "weather"})

	out := reg.Gather()
	if !strings.Contains(out, "# HELP test_duration_seconds Test histogram help") {
		t.Errorf("missing help string in gather output: %s", out)
	}
	if !strings.Contains(out, "# TYPE test_duration_seconds histogram") {
		t.Errorf("missing type string in gather output")
	}
	if !strings.Contains(out, `test_duration_seconds_bucket{handler="weather",le="0.1"} 1`) {
		t.Errorf("expected bucket le=0.1 count 1 in: %s", out)
	}
	if !strings.Contains(out, `test_duration_seconds_bucket{handler="weather",le="0.5"} 2`) {
		t.Errorf("expected bucket le=0.5 count 2 in: %s", out)
	}
	if !strings.Contains(out, `test_duration_seconds_bucket{handler="weather",le="1"} 3`) {
		t.Errorf("expected bucket le=1 count 3 in: %s", out)
	}
	if !strings.Contains(out, `test_duration_seconds_bucket{handler="weather",le="+Inf"} 4`) {
		t.Errorf("expected bucket le=+Inf count 4 in: %s", out)
	}
	if !strings.Contains(out, `test_duration_seconds_count{handler="weather"} 4`) {
		t.Errorf("expected count 4 in: %s", out)
	}
}

func TestMetrics_PrometheusExpositionFormat(t *testing.T) {
	pm := NewPlatformMetrics()

	pm.HTTPRequestsTotal.Inc(Label{Name: "capability", Value: "weather"}, Label{Name: "method", Value: "GET"}, Label{Name: "status_class", Value: "2xx"})
	pm.CircuitBreakerState.Set(0, Label{Name: "provider", Value: "openweather"})
	pm.RateLimitEventsTotal.Inc()

	output := pm.Registry.Gather()

	expectedSubstrings := []string{
		"# HELP uap_http_requests_total",
		"# TYPE uap_http_requests_total counter",
		`uap_http_requests_total{capability="weather",method="GET",status_class="2xx"} 1`,
		"# HELP uap_circuit_breaker_state",
		"# TYPE uap_circuit_breaker_state gauge",
		`uap_circuit_breaker_state{provider="openweather"} 0`,
		"# HELP uap_rate_limit_events_total",
		"uap_rate_limit_events_total 1",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(output, expected) {
			t.Errorf("expected output to contain %q, got:\n%s", expected, output)
		}
	}
}
