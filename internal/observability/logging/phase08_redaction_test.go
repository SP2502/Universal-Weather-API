package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretRedactionNestedAndMessage(t *testing.T) {
	var out bytes.Buffer
	logger := NewLogger("DEBUG", &out)
	logger.With("Authorization", "Bearer verysecret999", "context", slog.GroupValue(slog.String("token", "nestedsecret"), slog.String("error", "Get https://api.test/?appid=querysecret&x=1"))).ErrorContext(context.Background(), "Bearer messageSecret999", "x-api-key", "headersecret", "error", errors.New("Get https://host/?key=transportsecret"))
	msg := out.String()
	for _, secret := range []string{"verysecret999", "nestedsecret", "querysecret", "messageSecret999", "headersecret", "transportsecret"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("secret appeared in log: %s", secret)
		}
	}
	if !strings.Contains(msg, "[REDACTED]") {
		t.Fatalf("no redaction marker in %s", msg)
	}
}
