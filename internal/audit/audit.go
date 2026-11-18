package audit

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

type EventType string

const (
	EventAPIKeyCreated     EventType = "API_KEY_CREATED"
	EventAPIKeyRevoked     EventType = "API_KEY_REVOKED"
	EventAPIKeyRotated     EventType = "API_KEY_ROTATED"
	EventAuthFailure       EventType = "AUTH_FAILURE"
	EventAuthzFailure      EventType = "AUTHORIZATION_FAILURE"
	EventRateLimitExceeded EventType = "RATE_LIMIT_EXCEEDED"
	EventSSRFBlocked       EventType = "SSRF_BLOCKED"
	EventSuspiciousRequest EventType = "SUSPICIOUS_REQUEST"
)

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"event_type"`
	RequestID string    `json:"request_id,omitempty"`
	ActorID   string    `json:"actor_id,omitempty"`
	ClientIP  string    `json:"client_ip,omitempty"`
	Details   string    `json:"details,omitempty"`
	Severity  string    `json:"severity"` // INFO, WARN, CRITICAL
}

type AuditLogger struct {
	logger *slog.Logger
}

func NewAuditLogger(w io.Writer) *AuditLogger {
	if w == nil {
		w = os.Stdout
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	return &AuditLogger{
		logger: slog.New(handler),
	}
}

func (al *AuditLogger) Log(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Severity == "" {
		event.Severity = "INFO"
	}

	al.logger.InfoContext(ctx, "security_audit_event",
		"event_type", string(event.Type),
		"severity", event.Severity,
		"request_id", event.RequestID,
		"actor_id", event.ActorID,
		"client_ip", event.ClientIP,
		"details", event.Details,
		"audit_timestamp", event.Timestamp.Format(time.RFC3339),
	)
}
