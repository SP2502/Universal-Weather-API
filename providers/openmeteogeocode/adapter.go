// Package openmeteogeocode implements the hosted Open-Meteo forward geocoder.
// The hosted free tier must only be enabled for an eligible non-commercial operator.
package openmeteogeocode

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
		baseURL = "https://geocoding-api.open-meteo.com/v1"
	}
	return &Adapter{manifest: provider.Manifest{ID: "open_meteo_geocoding", Name: "Open-Meteo Hosted Geocoding", Version: "v1", Capabilities: []string{"geocode"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 5 * time.Second, FallbackPriority: 5, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Geocoding by Open-Meteo / GeoNames", URL: "https://open-meteo.com/"}, TermsURL: "https://open-meteo.com/en/licence", DocumentationURL: "https://open-meteo.com/en/docs/geocoding-api"}, httpClient: client.NewHTTPClient(5 * time.Second)}
}
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) ForwardGeocode(ctx context.Context, req provider.ForwardGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, provider.ErrProviderNotFound
	}
	endpoint, err := url.Parse(a.manifest.BaseURL + "/search")
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q := endpoint.Query()
	q.Set("name", req.Query)
	q.Set("format", "json")
	q.Set("count", strconv.Itoa(req.Limit))
	if req.Limit < 1 || req.Limit > 50 {
		q.Set("count", "10")
	}
	if req.CountryCode != "" {
		q.Set("countryCode", strings.ToUpper(req.CountryCode))
	}
	endpoint.RawQuery = q.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "UniversalWeatherAPI/1.0")
	response, err := a.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: geocoding transport", provider.ErrProviderUnavailable)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
	case 400, 404:
		return nil, provider.ErrProviderNotFound
	case 429:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: upstream status %d", provider.ErrProviderUnavailable, response.StatusCode)
	}
	body, err := provider.ReadLimitedJSON(response.Body)
	if err != nil {
		return nil, provider.ErrMalformedResponse
	}
	return &provider.RawGeocodeResponse{ProviderID: a.manifest.ID, Mode: "forward", RawPayload: body, RetrievedAt: time.Now().UTC()}, nil
}
func (a *Adapter) ReverseGeocode(context.Context, provider.ReverseGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	return nil, provider.ErrProviderNotFound
}

var _ provider.GeocodingProvider = (*Adapter)(nil)
