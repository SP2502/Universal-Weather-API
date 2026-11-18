package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPhase08RequestIDIgnoresUntrustedStrings(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, bad := range []string{strings.Repeat("A", 1000), "Bearer abcdefgh123456789", "http://bad.example/token", "a b", "_", "a\tb"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", bad)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		got := w.Header().Get("X-Request-ID")
		if !validRequestID(got) || got == bad {
			t.Errorf("unsafe ID %q retained as %q", bad, got)
		}
	}
}

func TestPhase08HSTSOnlyOnActualTLS(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "http://localhost:8080/health", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS advertised over HTTP from untrusted forwarded header")
	}
	r.TLS = &tls.ConnectionState{}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("TLS response missing HSTS")
	}
}

func TestPhase08CORSPreflightSafety(t *testing.T) {
	calls := 0
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://allowed.test"}, AllowedMethods: []string{"GET", "OPTIONS"}, AllowedHeaders: []string{"X-API-Key"}, MaxAge: 10})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	r := httptest.NewRequest("OPTIONS", "/v1/weather", nil)
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || calls != 0 {
		t.Fatalf("unapproved origin got %d, calls=%d", w.Code, calls)
	}
	r.Header.Set("Origin", "https://allowed.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://allowed.test" {
		t.Fatalf("allowed preflight failed: %d", w.Code)
	}
	if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Origin") {
		t.Fatal("missing Vary: Origin")
	}
}
