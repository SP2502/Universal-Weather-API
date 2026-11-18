package client

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestDirectEgressCannotUseEnvironmentProxy(t *testing.T) {
	c := NewHTTPClient(time.Second)
	transport := c.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("proxy CONNECT can bypass direct destination checks")
	}
}
func TestRedirectOriginAndSchemeRemainBound(t *testing.T) {
	c := NewHTTPClient(time.Second)
	start, _ := url.Parse("https://api.example.invalid/weather?key=fixture")
	for _, target := range []string{"http://api.example.invalid/weather", "https://other.example.invalid/weather", "https://user@api.example.invalid/weather"} {
		u, _ := url.Parse(target)
		if e := c.CheckRedirect(&http.Request{URL: u}, []*http.Request{{URL: start}}); e == nil {
			t.Fatalf("redirect allowed:%s", target)
		}
	}
}
func TestTestEndpointRequiresExactLoopbackHost(t *testing.T) {
	for _, tc := range []struct {
		u  string
		ok bool
	}{{"http://127.0.0.1:123", true}, {"http://[::1]:123", true}, {"http://localhost:123", true}, {"https://localhost.evil.invalid", false}, {"http://localhost@10.1.2.3", false}, {"https://api.example.invalid/?q=127.0.0.1", false}} {
		if IsLoopbackTestEndpoint(tc.u) != tc.ok {
			t.Errorf("unsafe test inference:%s", tc.u)
		}
	}
}
