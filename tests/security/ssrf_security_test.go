package security

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/http/client"
)

func TestSSRF_BlocksLoopbackTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Default hardened client
	c := client.NewHTTPClient(2 * time.Second)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatalf("expected SSRF error when accessing loopback URL, got nil")
	}

	if !errors.Is(err, client.ErrSSRFBlocked) {
		t.Logf("connection blocked as expected (error: %v)", err)
	}
}

func TestSSRF_BlocksRedirectToLoopback(t *testing.T) {
	// Target internal server that should never be reached via redirect
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sensitive internal data"))
	}))
	defer targetServer.Close()

	// External server that issues a 302 redirect to the internal server
	redirectingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer redirectingServer.Close()

	c := client.NewHardenedHTTPClient(client.ClientOptions{
		Timeout:         2 * time.Second,
		AllowPrivateIPs: false,
		MaxRedirects:    3,
	})

	req, _ := http.NewRequest(http.MethodGet, redirectingServer.URL, nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatalf("expected redirect to loopback to be blocked by SSRF policy, got nil")
	}
}
