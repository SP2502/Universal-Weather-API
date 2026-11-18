package authorization

import (
	"net/http"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/observability/logging"
)

func RequireScope(requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := logging.RequestIDFromContext(r.Context())
			caller := auth.CallerFromContext(r.Context())

			if caller == nil {
				api.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthenticated request", reqID, false)
				return
			}

			hasScope := false
			for _, s := range caller.Scopes {
				if s == "*" || s == requiredScope {
					hasScope = true
					break
				}
			}

			if !hasScope {
				api.WriteError(w, http.StatusForbidden, "FORBIDDEN", "Insufficient scope for this operation", reqID, false)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
