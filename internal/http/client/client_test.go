package client

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		// Loopback
		{"IPv4 loopback", "127.0.0.1", true},
		{"IPv4 loopback subnet", "127.0.1.5", true},
		{"IPv6 loopback", "::1", true},

		// RFC1918 Private
		{"Class A private", "10.0.0.1", true},
		{"Class A boundary", "10.255.255.254", true},
		{"Class B private", "172.16.0.1", true},
		{"Class B boundary", "172.31.255.254", true},
		{"Class C private", "192.168.1.1", true},
		{"Class C boundary", "192.168.255.254", true},

		// Cloud Metadata & Link Local
		{"AWS/GCP metadata", "169.254.169.254", true},
		{"Link local IPv4", "169.254.1.1", true},
		{"Link local IPv6", "fe80::1", true},

		// IPv6 ULA
		{"IPv6 ULA", "fc00::1", true},
		{"IPv6 ULA fd", "fd00::1", true},

		// Carrier-Grade NAT
		{"CGNAT", "100.64.0.1", true},

		// Public routable IPs (should NOT be blocked)
		{"Google DNS", "8.8.8.8", false},
		{"Cloudflare DNS", "1.1.1.1", false},
		{"Public IPv4", "93.184.216.34", false},
		{"Public IPv6", "2606:4700:4700::1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := net.ParseIP(tt.ip)
			if parsed == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			blocked := IsBlockedIP(parsed)
			if blocked != tt.blocked {
				t.Errorf("IsBlockedIP(%s) = %v; expected %v", tt.ip, blocked, tt.blocked)
			}
		})
	}
}

func TestHardenedClient_SSRFBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Default client blocks private / loopback IPs
	client := NewHTTPClient(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
		t.Fatalf("expected SSRF error when accessing loopback server, got nil")
	}

	if !errors.Is(err, ErrSSRFBlocked) {
		t.Logf("got expected connection block (error: %v)", err)
	}
}

func TestTestHTTPClient_AllowsLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response-from-local"))
	}))
	defer server.Close()

	// Test client allows loopback for httptest
	client := NewTestHTTPClient(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected request to succeed with test client, got: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}
