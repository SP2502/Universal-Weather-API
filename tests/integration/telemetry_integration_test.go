package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing"
	"github.com/universal-api/platform/internal/usage"
	"github.com/universal-api/platform/providers/openweather"
)

func TestGateway_TelemetryAndMetrics_E2E(t *testing.T) {
	// 1. Mock upstream OpenWeather server
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1257, "lat": 51.5085},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 288.15, "humidity": 75},
			"name": "London",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))
	defer upstreamServer.Close()

	// 2. Setup Gateway with Telemetry & Metrics
	store := database.NewMemoryStore()
	testAPIKey := "uap_live_test_telemetry_key_123"
	_ = store.CreateAPIKey(context.Background(), &database.APIKey{
		ID:        "key-telemetry-1",
		Name:      "Telemetry Test Key",
		KeyPrefix: "uap_live_",
		KeyHash:   database.HashKey(testAPIKey),
		Scopes:    []string{"weather:read"},
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	})

	registry := provider.NewRegistry()
	registry.Register(openweather.NewAdapter("test-key", upstreamServer.URL))

	weatherNormalizer := normalization.NewWeatherNormalizer()
	authenticator := auth.NewAuthenticator(store)
	authMiddleware := auth.Middleware(authenticator, store)
	usageTracker := usage.NewTracker(store)

	weatherHandler := capability.NewWeatherHandler(registry, nil, weatherNormalizer, usageTracker, nil, nil)
	healthHandler := health.NewHandler("1.0.0")

	pMetrics := metrics.DefaultMetrics
	tracer := tracing.DefaultTracer

	appRouter := routing.NewRouter(routing.RouterDependencies{
		HealthHandler:  healthHandler,
		MetricsHandler: metrics.Handler(pMetrics.Registry),
		WeatherHandler: weatherHandler,
		AuthMiddleware: authMiddleware,
	})

	logger := logging.NewLogger("DEBUG", io.Discard)

	var handler http.Handler = appRouter
	handler = middleware.Telemetry(tracer, pMetrics)(handler)
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 3. Make request with W3C traceparent header
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/weather?location=London", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	incomingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("traceparent", "00-"+incomingTraceID+"-00f067aa0ba902b7-01")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	// 4. Verify Trace Response Headers
	respTraceID := resp.Header.Get("X-Trace-ID")
	if respTraceID != incomingTraceID {
		t.Errorf("expected response X-Trace-ID %s, got %s", incomingTraceID, respTraceID)
	}

	traceparentHeader := resp.Header.Get("traceparent")
	if !strings.HasPrefix(traceparentHeader, "00-"+incomingTraceID) {
		t.Errorf("expected response traceparent header starting with 00-%s, got %s", incomingTraceID, traceparentHeader)
	}

	// 5. Scrape /metrics endpoint
	metricsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/metrics", nil)
	metricsResp, err := client.Do(metricsReq)
	if err != nil {
		t.Fatalf("failed to query /metrics: %v", err)
	}
	defer metricsResp.Body.Close()

	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /metrics HTTP 200, got %d", metricsResp.StatusCode)
	}

	metricsBody, err := io.ReadAll(metricsResp.Body)
	if err != nil {
		t.Fatalf("failed to read /metrics body: %v", err)
	}
	metricsText := string(metricsBody)

	// 6. Assert expected metrics in scrape output
	expectedMetrics := []string{
		"# TYPE uap_http_requests_total counter",
		`uap_http_requests_total{capability="weather",method="GET",status_class="2xx"}`,
		"# TYPE uap_provider_requests_total counter",
		`uap_provider_requests_total{capability="weather",provider="openweather",status_class="2xx"}`,
		"# TYPE uap_circuit_breaker_state gauge",
		`uap_circuit_breaker_state{provider="openweather"} 0`,
		"# TYPE uap_http_request_duration_seconds histogram",
		`uap_http_request_duration_seconds_count{capability="weather",method="GET"}`,
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(metricsText, expected) {
			t.Errorf("metrics output missing expected metric line: %s\nGot:\n%s", expected, metricsText)
		}
	}

	// 7. Cardinality Safety Assertion: ensure no sensitive/high-cardinality tokens leak into labels
	forbiddenTokens := []string{
		testAPIKey,
		"key-telemetry-1",
		"London",
		"-0.1257",
		"51.5085",
	}

	for _, token := range forbiddenTokens {
		if strings.Contains(metricsText, token) {
			t.Errorf("CARDINALITY VIOLATION: metrics output contains high-cardinality / sensitive token %q", token)
		}
	}
}
