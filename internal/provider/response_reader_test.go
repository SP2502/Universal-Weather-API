package provider

import (
	"bytes"
	"errors"
	"testing"
)

func TestPhase02UpstreamPayloadLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		content   []byte
		wantError bool
	}{
		{"empty", nil, true},
		{"small", []byte(`{"foo":"bar"}`), false},
		{"limit", bytes.Repeat([]byte(" "), MaxUpstreamJSONBytes), false},
		{"over-limit", append([]byte(`{}`), bytes.Repeat([]byte(" "), MaxUpstreamJSONBytes)...), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadLimitedJSON(bytes.NewReader(tc.content))
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%t", err, tc.wantError)
			}
			if err != nil && !errors.Is(err, ErrMalformedResponse) {
				t.Fatalf("wrong error type: %v", err)
			}
		})
	}
}
