// Package metnorway implements the Locationforecast/2.0 compact model forecast.
// The service requires an identifying User-Agent, respect for Expires and
// conditional requests using Last-Modified; this adapter maintains a bounded
// in-memory HTTP cache for that purpose.
package metnorway

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cached struct {
	body     json.RawMessage
	expires  time.Time
	modified string
}
type Adapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
	userAgent  string
	mu         sync.Mutex
	cache      map[string]cached
}

func NewAdapter(userAgent, baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.met.no/weatherapi/locationforecast/2.0"
	}
	return &Adapter{userAgent: userAgent, cache: make(map[string]cached), httpClient: client.NewHTTPClient(5 * time.Second), manifest: provider.Manifest{
		ID: "met_norway", Name: "MET Norway Locationforecast", Version: "2.0", Capabilities: []string{"forecast"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 5 * time.Second, FallbackPriority: 1, Status: provider.StatusActive,
		Attribution: provider.Attribution{Required: true, Text: "Weather forecast from MET Norway", URL: "https://api.met.no/"}, DocumentationURL: "https://api.met.no/weatherapi/locationforecast/2.0/documentation", TermsURL: "https://api.met.no/doc/License",
	}}
}
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) FetchForecast(ctx context.Context, lat, lon float64) (*provider.RawForecastResponse, error) {
	if !strings.Contains(a.userAgent, "@") && !strings.Contains(a.userAgent, "http") {
		return nil, provider.ErrProviderDisabled
	}
	endpoint, err := url.Parse(a.manifest.BaseURL + "/compact")
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q := endpoint.Query()
	q.Set("lat", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 4, 64))
	endpoint.RawQuery = q.Encode()
	identity := endpoint.RawQuery
	a.mu.Lock()
	item, found := a.cache[identity]
	a.mu.Unlock()
	now := time.Now()
	if found && now.Before(item.expires) {
		return &provider.RawForecastResponse{ProviderID: "met_norway", RawJSON: append(json.RawMessage(nil), item.body...), RetrievedAt: now.UTC(), FreshTTL: ttlPointer(item.expires, now)}, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	request.Header.Set("User-Agent", a.userAgent)
	request.Header.Set("Accept", "application/json")
	if found && item.modified != "" {
		request.Header.Set("If-Modified-Since", item.modified)
	}
	resp, err := a.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: MET Norway transport failure", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		if !found {
			return nil, provider.ErrMalformedResponse
		}
		item.expires = expiry(resp.Header, now)
		a.store(identity, item)
		return &provider.RawForecastResponse{ProviderID: "met_norway", RawJSON: append(json.RawMessage(nil), item.body...), RetrievedAt: now.UTC(), FreshTTL: ttlPointer(item.expires, now)}, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, provider.ErrProviderDisabled
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	default:
		return nil, fmt.Errorf("%w: MET Norway status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, provider.ErrMalformedResponse
	}
	if !json.Valid(body) {
		return nil, provider.ErrMalformedResponse
	}
	item = cached{body: append(json.RawMessage(nil), body...), expires: expiry(resp.Header, now), modified: resp.Header.Get("Last-Modified")}
	a.store(identity, item)
	return &provider.RawForecastResponse{ProviderID: "met_norway", RawJSON: body, RetrievedAt: now.UTC(), FreshTTL: ttlPointer(item.expires, now)}, nil
}
func expiry(h http.Header, now time.Time) time.Time {
	if ttl := provider.ForecastFreshTTL(h, now); ttl != nil {
		return now.Add(*ttl)
	}
	if when, err := http.ParseTime(h.Get("Expires")); err == nil && when.After(now) {
		return when
	}
	// Without usable freshness headers, avoid repeating an identical global
	// Locationforecast request sooner than MET Norway's three-hour guidance.
	return now.Add(3 * time.Hour)
}
func (a *Adapter) store(key string, it cached) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= 128 {
		a.cache = make(map[string]cached)
	}
	a.cache[key] = it
}

var _ provider.ForecastProvider = (*Adapter)(nil)

func ttlPointer(expires, now time.Time) *time.Duration {
	t := expires.Sub(now)
	if t < 0 {
		t = 0
	}
	return &t
}
