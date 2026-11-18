package producthttp

import (
	"context"
	"fmt"
	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
	"net/http"
	"net/url"
	"time"
)

func Fetch(ctx context.Context, httpClient *http.Client, base, path string, query url.Values, identifyingAgent string, accept ...string) ([]byte, error) {
	u, err := url.Parse(base + path)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if len(accept) > 0 && accept[0] == "application/geo+json" {
		req.Header.Set("Accept", accept[0])
	}
	if identifyingAgent == "" {
		identifyingAgent = "UniversalWeatherAPI/1.0"
	}
	req.Header.Set("User-Agent", identifyingAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream transport failure", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusBadRequest:
		return nil, provider.ErrProviderNotFound
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	default:
		return nil, fmt.Errorf("%w: upstream status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	raw, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, provider.ErrMalformedResponse
	}
	return raw, nil
}
func Client() *http.Client { return client.NewHTTPClient(6 * time.Second) }
