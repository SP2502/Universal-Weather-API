package middleware

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/observability/logging"
)

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					err, ok := rec.(error)
					var errMsg string
					if ok {
						errMsg = err.Error()
					} else {
						errMsg = fmt.Sprintf("%v", rec)
					}

					reqID := logging.RequestIDFromContext(r.Context())
					logger.ErrorContext(r.Context(), "panic recovered",
						"error", errMsg,
						"path", r.URL.Path,
					)

					api.WriteError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "An unexpected error occurred", reqID, false)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
