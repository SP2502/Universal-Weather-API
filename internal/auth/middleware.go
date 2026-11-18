package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/database"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
)

// Middleware is the compatibility wrapper for embedders. For a high-traffic
// gateway, use MiddlewareWithLastUsed with a bounded, lifecycle-managed writer.
func Middleware(authenticator *Authenticator, store database.Store) func(http.Handler) http.Handler {
	return MiddlewareWithLastUsed(authenticator, func(id string) {
		// No worker is created for embedded/test routers. Preserve LastUsedAt via
		// a bounded synchronous update; the production entrypoint uses the writer.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = store.UpdateAPIKeyLastUsed(ctx, id, time.Now().UTC())
	})
}

func MiddlewareWithLastUsed(authenticator *Authenticator, record func(string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := logging.RequestIDFromContext(r.Context())

			// Extract API Key from Header: X-API-Key or Authorization: Bearer <key>
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if apiKey == "" {
				metrics.DefaultMetrics.AuthFailuresTotal.Inc(metrics.Label{Name: "reason", Value: "missing_key"})
				api.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "API key missing or empty", reqID, false)
				return
			}

			caller, err := authenticator.AuthenticateKey(r.Context(), apiKey)
			if err != nil {
				if errors.Is(err, database.ErrKeyRevoked) {
					metrics.DefaultMetrics.AuthFailuresTotal.Inc(metrics.Label{Name: "reason", Value: "revoked_key"})
					api.WriteError(w, http.StatusUnauthorized, "KEY_REVOKED", "The provided API key has been revoked", reqID, false)
					return
				}
				if errors.Is(err, database.ErrKeyExpired) {
					metrics.DefaultMetrics.AuthFailuresTotal.Inc(metrics.Label{Name: "reason", Value: "expired_key"})
					api.WriteError(w, http.StatusUnauthorized, "KEY_EXPIRED", "The provided API key has expired", reqID, false)
					return
				}
				// Authentication must fail closed without disguising an unavailable
				// authoritative key store as a bad credential. Do not expose database
				// errors or connection strings to API callers.
				if !errors.Is(err, database.ErrNotFound) {
					metrics.DefaultMetrics.AuthFailuresTotal.Inc(metrics.Label{Name: "reason", Value: "store_unavailable"})
					w.Header().Set("Retry-After", "1")
					api.WriteError(w, http.StatusServiceUnavailable, "AUTH_BACKEND_UNAVAILABLE", "Authentication temporarily unavailable", reqID, true)
					return
				}
				metrics.DefaultMetrics.AuthFailuresTotal.Inc(metrics.Label{Name: "reason", Value: "invalid_key"})
				api.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid API key", reqID, false)
				return
			}

			// Bounded/coalesced in the production gateway; no per-request goroutine.
			if record != nil {
				record(caller.KeyID)
			}

			ctx := WithCaller(r.Context(), caller)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
