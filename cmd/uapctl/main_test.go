package main

import "testing"

func TestProvisioningArgs(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		valid bool
	}{
		{"standard", []string{"create-key", "--name", "production-operator"}, true},
		{"single scope", []string{"create-key", "--name", "read-only", "--scopes", "weather:read"}, true},
		{"forecast scope", []string{"create-key", "--name", "read-only", "--scopes", "forecast:read"}, true},
		{"no name", []string{"create-key"}, false},
		{"bad scope", []string{"create-key", "--name", "x", "--scopes", "admin:*"}, false},
		{"duplicate scopes", []string{"create-key", "--name", "x", "--scopes", "weather:read,weather:read"}, false},
		{"indefinite key", []string{"create-key", "--name", "x", "--expires-in", "0"}, false},
		{"unexpected arguments", []string{"create-key", "--name", "x", "extra"}, false},
		{"unknown command", []string{"remove-all-keys"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := parseKeyArgs(tc.args)
			if (err == nil) != tc.valid {
				t.Fatalf("expected valid=%v, got err=%v", tc.valid, err)
			}
		})
	}
}
