package metrics

import (
	"fmt"
	"net/http"
)

// Handler returns an http.Handler that renders Prometheus formatted metrics from the registry.
func Handler(reg *Registry) http.Handler {
	if reg == nil {
		reg = DefaultMetrics.Registry
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := reg.Gather()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, output)
	})
}
