package metnorway

import (
	"net/http"
	"testing"
	"time"
)

func TestMissingExpiryRespectsGlobalLocationforecastInterval(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if got := expiry(http.Header{}, now); got.Sub(now) != 3*time.Hour {
		t.Fatalf("missing headers: next request after %s, want three hours", got.Sub(now))
	}
	h := http.Header{"Cache-Control": []string{"no-store"}}
	if got := expiry(h, now); got.After(now) {
		t.Fatal("no-store response must not be retained")
	}
}
