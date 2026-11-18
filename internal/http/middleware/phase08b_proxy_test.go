package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestPhase08BTrustedProxyResolution(t *testing.T) {
	r := NewRateLimiter(1, 2)
	networks, err := ParseTrustedProxies("10.0.0.0/8,127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	r.SetTrustedProxies(networks)
	direct := httptest.NewRequest("GET", "/", nil)
	direct.RemoteAddr = "203.0.113.10:9000"
	direct.Header.Set("X-Forwarded-For", "198.51.100.20")
	if got := r.clientRemote(direct); got != "203.0.113.10:9000" {
		t.Fatalf("spoofed untrusted XFF accepted: %s", got)
	}
	trusted := httptest.NewRequest("GET", "/", nil)
	trusted.RemoteAddr = "10.1.1.1:443"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.77, 203.0.113.7, 10.2.2.2")
	if got := r.clientRemote(trusted); got != "203.0.113.7" {
		t.Fatalf("wrong nearest untrusted hop: %s", got)
	}
	trusted.Header.Set("X-Forwarded-For", "not-an-ip, 10.2.2.2")
	if got := r.clientRemote(trusted); got != "10.1.1.1:443" {
		t.Fatalf("invalid forwarding chain trusted: %s", got)
	}
	trusted.Header.Set("X-Forwarded-For", "203.0.113.8, 10.2.2.2")
	if got := r.clientRemote(trusted); got != "203.0.113.8" {
		t.Fatalf("valid forwarded client excluded: %s", got)
	}
}
func TestPhase08BRejectsBroadTrustedProxyConfig(t *testing.T) {
	for _, v := range []string{"0.0.0.0/0", "::/0", "this-is-not-a-cidr"} {
		if _, err := ParseTrustedProxies(v); err == nil {
			t.Fatalf("accepted unsafe proxy range: %s", v)
		}
	}
}
