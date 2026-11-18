package security

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/universal-api/platform/internal/http/middleware"
	"github.com/universal-api/platform/internal/observability/logging"
)

func TestSecurityHeaders_Middleware(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.SecurityHeaders(dummy)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headers := rec.Header()
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing or incorrect X-Content-Type-Options: %s", headers.Get("X-Content-Type-Options"))
	}
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Errorf("missing or incorrect X-Frame-Options: %s", headers.Get("X-Frame-Options"))
	}
	if headers.Get("Content-Security-Policy") != "default-src 'none'" {
		t.Errorf("missing or incorrect CSP: %s", headers.Get("Content-Security-Policy"))
	}
	if headers.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("missing or incorrect Referrer-Policy: %s", headers.Get("Referrer-Policy"))
	}
}

func TestCORS_PreflightAndHeaders(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cfg := middleware.CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"X-API-Key"},
		MaxAge:         3600,
	}

	handler := middleware.CORS(cfg)(dummy)

	// Preflight request
	req := httptest.NewRequest(http.MethodOptions, "/v1/weather", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content for OPTIONS, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected allowed origin https://example.com, got %s", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestMaxBodySize_LimitsOversizedPayloads(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// 1 KB limit for testing
	handler := middleware.MaxBodySize(1024)(dummy)

	// Payload of 2 KB
	oversizedData := bytes.Repeat([]byte("A"), 2048)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(oversizedData))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Payload Too Large for oversized body, got %d", rec.Code)
	}
}

func TestRateLimiter_EnforcesThrottling(t *testing.T) {
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Rate of 5 req/sec, burst of 3
	limiter := middleware.NewRateLimiter(5, 3)
	handler := limiter.Middleware()(dummy)

	clientKey := "uap_live_test_throttle_key"

	// Fire 3 allowed burst requests
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
		req.Header.Set("X-API-Key", clientKey)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, rec.Code)
		}
	}

	// 4th request should be throttled (429)
	req := httptest.NewRequest(http.MethodGet, "/v1/weather", nil)
	req.Header.Set("X-API-Key", clientKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests after burst exhaustion, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}
}

func TestLogRedaction_SanitizesSensitiveFields(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger("INFO", &buf)

	logger.Info("tenant request logged",
		"api_key", "uap_live_super_secret_key_12345",
		"authorization", "Bearer top-secret-token",
		"password", "hunter2",
		"secret", "db_password_val",
		"public_field", "visible-data",
	)

	logOutput := buf.String()

	if strings.Contains(logOutput, "uap_live_super_secret_key_12345") {
		t.Errorf("api_key was not redacted from log: %s", logOutput)
	}
	if strings.Contains(logOutput, "top-secret-token") {
		t.Errorf("authorization was not redacted from log: %s", logOutput)
	}
	if strings.Contains(logOutput, "hunter2") {
		t.Errorf("password was not redacted from log: %s", logOutput)
	}
	if strings.Contains(logOutput, "db_password_val") {
		t.Errorf("secret was not redacted from log: %s", logOutput)
	}
	if !strings.Contains(logOutput, "visible-data") {
		t.Errorf("public_field was erroneously redacted: %s", logOutput)
	}
}
