package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/observability/logging"
)

func TestIntegration_ProbesAndRequestID(t *testing.T) {
	logger := logging.NewLogger("INFO", io.Discard)
	healthHandler := health.NewHandler("0.1.0")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler.Health)

	var handler http.Handler = mux
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := ts.Client()

	// 1. GET /health
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
	req.Header.Set("X-Request-ID", "test-req-trace-123")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("X-Request-ID") != "test-req-trace-123" {
		t.Errorf("expected propagated X-Request-ID, got %s", resp.Header.Get("X-Request-ID"))
	}

	var payload api.ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response envelope: %v", err)
	}

	if payload.Meta.RequestID != "test-req-trace-123" {
		t.Errorf("expected meta request_id test-req-trace-123, got %s", payload.Meta.RequestID)
	}
}
