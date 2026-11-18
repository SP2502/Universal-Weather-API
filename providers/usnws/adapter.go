// Package usnws implements NWS /points -> /gridpoints/.../forecast/hourly.
// It returns forecast predictions, never observed current-weather data.
package usnws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
)

type grid struct {
	path  string
	until time.Time
}
type Adapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
	userAgent  string
	mu         sync.Mutex
	points     map[string]grid
}

func NewAdapter(userAgent, baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.weather.gov"
	}
	return &Adapter{userAgent: userAgent, httpClient: client.NewHTTPClient(6 * time.Second), points: make(map[string]grid), manifest: provider.Manifest{
		ID: "us_nws", Name: "US National Weather Service Hourly Forecast", Version: "v1", Capabilities: []string{"forecast"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 6 * time.Second, FallbackPriority: 3, Status: provider.StatusActive,
		Attribution: provider.Attribution{Required: true, Text: "Weather forecast from the US National Weather Service (NOAA)", URL: "https://www.weather.gov/"}, DocumentationURL: "https://www.weather.gov/documentation/services-web-api", TermsURL: "https://www.weather.gov/disclaimer",
	}}
}
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) FetchForecast(ctx context.Context, lat, lon float64) (*provider.RawForecastResponse, error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil, provider.ErrProviderNotFound
	}
	if len(strings.TrimSpace(a.userAgent)) < 12 || (!strings.Contains(a.userAgent, "@") && !strings.Contains(a.userAgent, "https://")) {
		return nil, provider.ErrProviderDisabled
	}
	// The NWS API is regional. The gateway policy must prove US location
	// eligibility before calling this method; never infer it from a bounding box.
	identity := strconv.FormatFloat(lat, 'f', 4, 64) + "," + strconv.FormatFloat(lon, 'f', 4, 64)
	a.mu.Lock()
	cached, ok := a.points[identity]
	a.mu.Unlock()
	path := cached.path
	if !ok || time.Now().After(cached.until) {
		b, pointTTL, err := a.getWithFreshness(ctx, "/points/"+identity)
		if err != nil {
			return nil, err
		}
		var location struct {
			Properties struct {
				ForecastHourly string `json:"forecastHourly"`
			} `json:"properties"`
		}
		if json.Unmarshal(b, &location) != nil {
			return nil, provider.ErrMalformedResponse
		}
		path, err = a.safePath(location.Properties.ForecastHourly)
		if err != nil {
			return nil, provider.ErrMalformedResponse
		}
		a.mu.Lock()
		if len(a.points) >= 128 {
			a.points = make(map[string]grid)
		}
		ttl := 6 * time.Hour
		if pointTTL != nil && *pointTTL < ttl {
			ttl = *pointTTL
		}
		if ttl > 0 {
			a.points[identity] = grid{path: path, until: time.Now().Add(ttl)}
		}
		a.mu.Unlock()
	}
	b, freshness, err := a.getWithFreshness(ctx, path)
	if err != nil {
		return nil, err
	}
	return &provider.RawForecastResponse{ProviderID: "us_nws", RawJSON: b, RetrievedAt: time.Now().UTC(), Latitude: &lat, Longitude: &lon, FreshTTL: freshness}, nil
}
func (a *Adapter) safePath(reference string) (string, error) {
	base, err := url.Parse(a.manifest.BaseURL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(reference)
	if err != nil {
		return "", err
	}
	if ref.Scheme != "https" && ref.Scheme != "http" && ref.Scheme != "" {
		return "", errors.New("unexpected scheme")
	}
	full := base.ResolveReference(ref)
	if !strings.EqualFold(full.Host, base.Host) || full.Scheme != base.Scheme || full.User != nil || full.RawQuery != "" || full.Fragment != "" || !strings.HasPrefix(full.EscapedPath(), "/gridpoints/") || !strings.HasSuffix(full.EscapedPath(), "/forecast/hourly") || strings.Contains(full.EscapedPath(), "..") {
		return "", errors.New("unexpected NWS grid URL")
	}
	return full.EscapedPath(), nil
}
func (a *Adapter) getWithFreshness(ctx context.Context, path string) (json.RawMessage, *time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.manifest.BaseURL+path, nil)
	if err != nil {
		return nil, nil, provider.ErrProviderUnavailable
	}
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Accept", "application/geo+json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("%w: NWS transport failure", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 404:
		return nil, nil, provider.ErrProviderNotFound
	case 401, 403:
		return nil, nil, provider.ErrProviderDisabled
	case 429:
		return nil, nil, provider.ErrRateLimited
	default:
		return nil, nil, fmt.Errorf("%w: NWS status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil || !json.Valid(body) {
		return nil, nil, provider.ErrMalformedResponse
	}
	return body, provider.ForecastFreshTTL(resp.Header, time.Now()), nil
}

var _ provider.ForecastProvider = (*Adapter)(nil)
