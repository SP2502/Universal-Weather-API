package producthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
)

func TestFetchMapsUpstreamStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusBadRequest, provider.ErrProviderNotFound},
		{http.StatusNotFound, provider.ErrProviderNotFound},
		{http.StatusUnauthorized, provider.ErrProviderDisabled},
		{http.StatusForbidden, provider.ErrProviderDisabled},
		{http.StatusTooManyRequests, provider.ErrRateLimited},
		{http.StatusServiceUnavailable, provider.ErrProviderUnavailable},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			_, err := Fetch(context.Background(), client.NewTestHTTPClient(time.Second), server.URL, "/fixture", url.Values{"q": {"test"}}, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("status %d: got %v, want %v", tc.status, err, tc.want)
			}
		})
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}` + strings.Repeat(" ", provider.MaxUpstreamJSONBytes)))
	}))
	defer server.Close()
	_, err := Fetch(context.Background(), client.NewTestHTTPClient(time.Second), server.URL, "/fixture", nil, "")
	if !errors.Is(err, provider.ErrMalformedResponse) {
		t.Fatalf("oversized body: got %v, want malformed response", err)
	}
}

func TestFetchHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Fetch(ctx, client.NewTestHTTPClient(time.Second), server.URL, "/fixture", nil, "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled fetch: got %v, want context deadline", err)
	}
}
