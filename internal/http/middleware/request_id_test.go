package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Header generated when absent
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resID := rec.Header().Get(HeaderXRequestID)
	if resID == "" {
		t.Error("expected X-Request-ID to be generated in response")
	}

	// Case 2: Header propagated when supplied
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderXRequestID, "custom-id-42")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get(HeaderXRequestID) != "custom-id-42" {
		t.Errorf("expected propagated ID custom-id-42, got %s", rec.Header().Get(HeaderXRequestID))
	}
}
