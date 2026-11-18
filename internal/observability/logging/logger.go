package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/universal-api/platform/internal/observability/tracing"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

// RedactingHandler protects both structured attributes and the common query
// credential forms embedded in transport-error messages. Never log raw HTTP
// requests or secret values as a substitute for upstream diagnostics.
type RedactingHandler struct{ slog.Handler }

var sensitiveKeys = map[string]bool{"authorization": true, "x-api-key": true, "x_api_key": true, "api_key": true, "apikey": true, "appid": true, "key": true, "token": true, "secret": true, "password": true, "redis_password": true, "database_url": true, "access_token": true}
var secretQuery = regexp.MustCompile(`(?i)([?&](?:appid|key|api_key|apikey|access_token|token|password|secret)=)[^&#\s"']+`)
var bearerToken = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)

func scrubText(s string) string {
	return bearerToken.ReplaceAllString(secretQuery.ReplaceAllString(s, "${1}[REDACTED]"), "${1}[REDACTED]")
}
func scrub(a slog.Attr) slog.Attr {
	if sensitiveKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, "[REDACTED]")
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		out := make([]slog.Attr, 0, len(group))
		for _, item := range group {
			out = append(out, scrub(item))
		}
		return slog.Group(a.Key, attrsToArgs(out)...)
	}
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, scrubText(a.Value.String()))
	}
	if a.Value.Kind() == slog.KindAny {
		if e, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, scrubText(e.Error()))
		}
	}
	return a
}
func attrsToArgs(attrs []slog.Attr) []any {
	values := make([]any, 0, len(attrs))
	for _, a := range attrs {
		values = append(values, a)
	}
	return values
}
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, scrub(a))
	}
	return &RedactingHandler{Handler: h.Handler.WithAttrs(out)}
}
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{Handler: h.Handler.WithGroup(name)}
}
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	newRecord := slog.NewRecord(r.Time, r.Level, scrubText(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool { newRecord.AddAttrs(scrub(a)); return true })
	if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
		newRecord.AddAttrs(slog.String("request_id", scrubText(reqID)))
	}
	if span := tracing.SpanFromContext(ctx); span != nil {
		newRecord.AddAttrs(slog.String("trace_id", span.TraceID()), slog.String("span_id", span.SpanID()))
	} else if tid := tracing.TraceIDFromContext(ctx); tid != "" {
		newRecord.AddAttrs(slog.String("trace_id", tid))
	}
	return h.Handler.Handle(ctx, newRecord)
}
func NewLogger(levelStr string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := slog.LevelInfo
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	return slog.New(&RedactingHandler{Handler: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})})
}
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}
func RequestIDFromContext(ctx context.Context) string {
	if val, ok := ctx.Value(RequestIDKey).(string); ok {
		return val
	}
	return ""
}
