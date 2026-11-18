package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPhase02JSONFailureDoesNotCommitSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]any{"temp": math.NaN()})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invalid JSON must not return 200: %d", rec.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("failure response is not JSON: %v", err)
	}
	if _, ok := decoded["error"]; !ok {
		t.Fatalf("missing error envelope: %s", rec.Body.String())
	}
}
