// Package brightsky requests DWD-backed hourly forecast records. It is not
// registered as a current-weather observation source.
package brightsky

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Adapter struct {
	manifest    provider.Manifest
	httpClient  *http.Client
	maxDistance int
}

func NewAdapter(baseURL string, maxDistanceMeters int) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.brightsky.dev"
	}
	if maxDistanceMeters <= 0 || maxDistanceMeters > 50000 {
		maxDistanceMeters = 25000
	}
	return &Adapter{httpClient: client.NewHTTPClient(6 * time.Second), maxDistance: maxDistanceMeters, manifest: provider.Manifest{ID: "bright_sky", Name: "Bright Sky DWD Hourly Forecast", Version: "v2", Capabilities: []string{"forecast"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 6 * time.Second, FallbackPriority: 4, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Data from Deutscher Wetterdienst (DWD) via Bright Sky", URL: "https://brightsky.dev/"}, DocumentationURL: "https://brightsky.dev/docs/", TermsURL: "https://www.dwd.de/EN/service/copyright/copyright_node.html"}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) FetchForecast(ctx context.Context, lat, lon float64) (*provider.RawForecastResponse, error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil, provider.ErrProviderNotFound
	}
	u, err := url.Parse(a.manifest.BaseURL + "/weather")
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q := u.Query()
	q.Set("lat", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("max_dist", strconv.Itoa(a.maxDistance))
	q.Set("date", time.Now().UTC().Format("2006-01-02"))
	q.Set("last_date", time.Now().UTC().Add(48*time.Hour).Format("2006-01-02"))
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "UniversalWeatherAPI/1.0 (self-hosted gateway)")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: Bright Sky transport failure", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 400, 404:
		return nil, provider.ErrProviderNotFound
	case 401, 403:
		return nil, provider.ErrProviderDisabled
	case 429:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: Bright Sky status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	b, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil || !json.Valid(b) {
		return nil, provider.ErrMalformedResponse
	}
	return &provider.RawForecastResponse{ProviderID: "bright_sky", RawJSON: b, RetrievedAt: time.Now().UTC(), Latitude: &lat, Longitude: &lon, MaxStationDistanceMeters: a.maxDistance, FreshTTL: provider.ForecastFreshTTL(resp.Header, time.Now())}, nil
}

var _ provider.ForecastProvider = (*Adapter)(nil)
