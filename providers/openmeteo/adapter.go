// Package openmeteo implements the hosted free forecast endpoint.
// Its commercial-use restrictions are enforced at registration AND policy.
package openmeteo

import (
	"context"
	"fmt"
	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Adapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
}

func NewAdapter(baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.open-meteo.com/v1"
	}
	return &Adapter{httpClient: client.NewHTTPClient(5 * time.Second), manifest: provider.Manifest{ID: "open_meteo_forecast", Name: "Open-Meteo Hosted Forecast", Version: "v1", Capabilities: []string{"forecast"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 5 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Forecast by Open-Meteo; model sources per Open-Meteo", URL: "https://open-meteo.com/"}, TermsURL: "https://open-meteo.com/en/licence", DocumentationURL: "https://open-meteo.com/en/docs"}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) FetchForecast(ctx context.Context, lat, lon float64) (*provider.RawForecastResponse, error) {
	endpoint, err := url.Parse(a.manifest.BaseURL + "/forecast")
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q := endpoint.Query()
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("hourly", "temperature_2m,relative_humidity_2m,weather_code")
	q.Set("timezone", "UTC")
	q.Set("timeformat", "unixtime")
	q.Set("forecast_days", "7")
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	req.Header.Set("User-Agent", "UniversalWeatherAPI/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: Open-Meteo transport failure", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 400, 404:
		return nil, provider.ErrProviderNotFound
	case 429:
		return nil, provider.ErrRateLimited
	case 401, 403:
		return nil, provider.ErrProviderDisabled
	default:
		return nil, fmt.Errorf("%w: Open-Meteo status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, provider.ErrMalformedResponse
	}
	return &provider.RawForecastResponse{ProviderID: "open_meteo_forecast", RawJSON: body, RetrievedAt: time.Now().UTC(), FreshTTL: provider.ForecastFreshTTL(resp.Header, time.Now())}, nil
}

var _ provider.ForecastProvider = (*Adapter)(nil)
