package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

// Telemetry returns a middleware that manages distributed trace context and records gateway HTTP metrics.
func Telemetry(tracer *tracing.Tracer, pMetrics *metrics.PlatformMetrics) func(http.Handler) http.Handler {
	if tracer == nil {
		tracer = tracing.DefaultTracer
	}
	if pMetrics == nil {
		pMetrics = metrics.DefaultMetrics
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Parse incoming W3C traceparent header if available
			ctx := r.Context()
			traceparentHeader := r.Header.Get("traceparent")
			if traceparentHeader != "" {
				if tp, err := tracing.ParseTraceParent(traceparentHeader); err == nil {
					ctx = tracing.WithTraceParent(ctx, tp)
				}
			}

			// Start root gateway span
			spanName := "HTTP request"
			ctx, span := tracer.StartSpan(ctx, spanName)
			defer span.End()

			method := safeMethod(r.Method)
			span.SetAttribute("http.method", method)
			span.SetAttribute("http.target", normalizedPath(r.URL.Path))

			// Set response trace headers
			w.Header().Set("X-Trace-ID", span.TraceID())
			w.Header().Set("traceparent", fmt.Sprintf("00-%s-%s-01", span.TraceID(), span.SpanID()))

			recorder := &statusRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(recorder, r.WithContext(ctx))

			durationSec := time.Since(start).Seconds()

			// Determine capability for bounded metric label
			capability := "gateway"
			path := strings.ToLower(r.URL.Path)
			if strings.Contains(path, "/forecast") {
				capability = "forecast"
			} else if strings.Contains(path, "/weather") {
				capability = "weather"
			} else if strings.Contains(path, "/geocode") {
				capability = "geocode"
			} else if strings.Contains(path, "/health") {
				capability = "health"
			} else if strings.Contains(path, "/metrics") {
				capability = "metrics"
			}

			// Bounded status class (e.g. 2xx, 4xx, 5xx)
			statusClass := fmt.Sprintf("%dxx", recorder.statusCode/100)

			// Record Prometheus metrics
			pMetrics.HTTPRequestsTotal.Inc(
				metrics.Label{Name: "capability", Value: capability},
				metrics.Label{Name: "method", Value: method},
				metrics.Label{Name: "status_class", Value: statusClass},
			)

			pMetrics.HTTPRequestDuration.Observe(
				durationSec,
				metrics.Label{Name: "capability", Value: capability},
				metrics.Label{Name: "method", Value: method},
			)

			if recorder.statusCode >= 400 {
				span.SetStatus(tracing.StatusError, fmt.Sprintf("HTTP %d", recorder.statusCode))
			} else {
				span.SetStatus(tracing.StatusOK, "OK")
			}
			span.SetAttribute("http.status_code", fmt.Sprintf("%d", recorder.statusCode))
		})
	}
}

// Metrics must not create unbounded label sets from arbitrary methods/paths.
func safeMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}
func normalizedPath(path string) string {
	switch path {
	case "/v1/weather", "/v1/forecast", "/v1/geocode", "/health", "/readiness", "/liveness", "/metrics":
		return path
	default:
		return "unmatched"
	}
}
