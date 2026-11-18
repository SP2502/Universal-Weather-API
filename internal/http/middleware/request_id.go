package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/universal-api/platform/internal/observability/logging"
)

const HeaderXRequestID = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderXRequestID)
		if !validRequestID(reqID) {
			reqID = generateRequestID()
		}

		ctx := logging.WithRequestID(r.Context(), reqID)
		w.Header().Set(HeaderXRequestID, reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(bytes)
}

// Request IDs are untrusted client input echoed in response headers and logs.
// Do not allow unbounded strings, control characters, bearer tokens or URLs.
func validRequestID(s string) bool {
	if len(s) < 1 || len(s) > 64 || strings.TrimSpace(s) != s {
		return false
	}
	hasAlphanumeric := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			hasAlphanumeric = true
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return hasAlphanumeric
}
