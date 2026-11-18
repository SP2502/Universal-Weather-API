// Package openmeteoairquality supplies optional, noncommercial hosted CAMS
// modeled pollutant forecasts. Operators must affirm deployment permission.
package openmeteoairquality

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
		baseURL = "https://air-quality-api.open-meteo.com/v1"
	}
	return &Adapter{manifest: provider.Manifest{ID: "open_meteo_air_quality", Name: "Open-Meteo CAMS Air Quality Forecast", Version: "v1", Capabilities: []string{"air_quality"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 5 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Air quality model data from Open-Meteo / CAMS", URL: "https://open-meteo.com/en/docs/air-quality-api"}, TermsURL: "https://open-meteo.com/en/licence", DocumentationURL: "https://open-meteo.com/en/docs/air-quality-api"}, httpClient: client.NewHTTPClient(5 * time.Second)}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) FetchAirQuality(ctx context.Context, latitude, longitude float64) (*provider.RawAirQualityResponse, error) {
	endpoint, err := url.Parse(a.manifest.BaseURL + "/air-quality")
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q := endpoint.Query()
	q.Set("latitude", strconv.FormatFloat(latitude, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(longitude, 'f', 4, 64))
	q.Set("hourly", "pm2_5,pm10,ozone,nitrogen_dioxide,sulphur_dioxide,carbon_monoxide")
	q.Set("forecast_days", "2")
	q.Set("timeformat", "unixtime")
	q.Set("timezone", "UTC")
	endpoint.RawQuery = q.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "UniversalWeatherAPI/1.0")
	resp, err := a.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: air-quality transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	default:
		return nil, fmt.Errorf("%w: upstream air-quality status %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
	raw, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, provider.ErrMalformedResponse
	}
	return &provider.RawAirQualityResponse{ProviderID: a.manifest.ID, RawJSON: raw, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.AirQualityProvider = (*Adapter)(nil)
