package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	meta := Meta{
		RequestID: "req-123",
		Provider:  "mock-provider",
		Cached:    false,
		Fallback:  false,
	}

	data := map[string]string{"status": "ok"}
	WriteSuccess(rec, 200, data, meta)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Meta.RequestID != "req-123" {
		t.Errorf("expected req-123, got %s", resp.Meta.RequestID)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, 504, "UPSTREAM_TIMEOUT", "Upstream timed out", "req-456", true)

	if rec.Code != 504 {
		t.Errorf("expected 504, got %d", rec.Code)
	}

	var errResp ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}

	if errResp.Error.Code != "UPSTREAM_TIMEOUT" || !errResp.Error.Retryable {
		t.Errorf("unexpected error payload: %+v", errResp.Error)
	}
}
