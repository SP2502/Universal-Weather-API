package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoggerRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("INFO", &buf)

	ctx := context.Background()
	logger.InfoContext(ctx, "test login", "api_key", "secret-key-12345", "user", "alice")

	output := buf.String()
	if strings.Contains(output, "secret-key-12345") {
		t.Errorf("expected secret to be redacted, got: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %s", output)
	}
}

func TestLoggerContextRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("INFO", &buf)

	ctx := WithRequestID(context.Background(), "req-uuid-999")
	logger.InfoContext(ctx, "testing context req id")

	var logMap map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("failed to parse log json: %v", err)
	}

	if logMap["request_id"] != "req-uuid-999" {
		t.Errorf("expected request_id req-uuid-999, got %v", logMap["request_id"])
	}
}
