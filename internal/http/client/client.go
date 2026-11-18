package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type ClientOptions struct {
	Timeout         time.Duration
	AllowPrivateIPs bool
	MaxRedirects    int
}

func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		Timeout:         10 * time.Second,
		AllowPrivateIPs: false,
		MaxRedirects:    3,
	}
}

// NewHardenedHTTPClient creates a secure, production-grade HTTP client with SSRF protection and TLS 1.2+ minimum.
func NewHardenedHTTPClient(opts ClientOptions) *http.Client {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = 3
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Custom DialContext with DNS resolution and SSRF IP validation
	customDialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("ssrf dialer: invalid address: %w", err)
		}

		// Resolve host IPs
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("ssrf dialer: dns resolution failed for %s: %w", host, err)
		}

		if !opts.AllowPrivateIPs {
			if err := ValidateIPs(ips); err != nil {
				return nil, err
			}
		}

		// Connect to the first valid resolved IP
		targetAddr := net.JoinHostPort(ips[0].String(), port)
		return dialer.DialContext(ctx, network, targetAddr)
	}

	transport := &http.Transport{
		Proxy:                 nil, // direct egress; unreviewed environment proxies cannot bypass destination checks
		DialContext:           customDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= opts.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", opts.MaxRedirects)
			}

			if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || !strings.EqualFold(req.URL.Host, via[0].URL.Host) || req.URL.User != nil) {
				return errors.New("cross-origin or scheme-changing provider redirect blocked")
			}
			if !opts.AllowPrivateIPs {
				host := req.URL.Hostname()
				if host != "" {
					ips, err := net.DefaultResolver.LookupIP(req.Context(), "ip", host)
					if err != nil {
						return fmt.Errorf("redirect dns lookup failed: %w", err)
					}
					if err := ValidateIPs(ips); err != nil {
						return fmt.Errorf("redirect target blocked: %w", err)
					}
				}
			}

			return nil
		},
	}

	return client
}

// NewHTTPClient creates a default hardened HTTP client.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return NewHardenedHTTPClient(ClientOptions{
		Timeout:         timeout,
		AllowPrivateIPs: false,
		MaxRedirects:    3,
	})
}

// NewTestHTTPClient creates an HTTP client for testing that allows loopback/private IPs for httptest.Server.
func NewTestHTTPClient(timeout time.Duration) *http.Client {
	return NewHardenedHTTPClient(ClientOptions{
		Timeout:         timeout,
		AllowPrivateIPs: true,
		MaxRedirects:    3,
	})
}
