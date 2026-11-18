// Package geoapify implements the Geoapify v1 forward/reverse geocoder.
// Query credentials must never be included in logged request URLs or errors.
package geoapify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
)

type Adapter struct {
	manifest   provider.Manifest
	key        string
	httpClient *http.Client
}

func NewAdapter(key, baseURL string) *Adapter {
	if baseURL == "" {
		baseURL = "https://api.geoapify.com/v1/geocode"
	}
	return &Adapter{key: key, httpClient: client.NewHTTPClient(5 * time.Second), manifest: provider.Manifest{
		ID: "geoapify", Name: "Geoapify Geocoding", Version: "v1", Capabilities: []string{"geocode"}, BaseURL: strings.TrimRight(baseURL, "/"), Timeout: 5 * time.Second, FallbackPriority: 3,
		CredentialsRequired: []string{"GEOAPIFY_API_KEY"}, DocumentationURL: "https://apidocs.geoapify.com/docs/geocoding/forward-geocoding/", TermsURL: "https://www.geoapify.com/terms-and-conditions/",
		Attribution: provider.Attribution{Required: true, Text: "Powered by Geoapify; © OpenStreetMap contributors", URL: "https://www.geoapify.com/"}, Status: provider.StatusActive,
	}}
}
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.httpClient = c; return a }
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) ForwardGeocode(ctx context.Context, req provider.ForwardGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, provider.ErrProviderNotFound
	}
	q := url.Values{}
	q.Set("text", req.Query)
	q.Set("format", "json")
	q.Set("limit", strconv.Itoa(req.Limit))
	if req.Limit < 1 || req.Limit > 50 {
		q.Set("limit", "10")
	}
	if req.CountryCode != "" {
		q.Set("filter", "countrycode:"+strings.ToLower(req.CountryCode))
	}
	return a.fetch(ctx, "search", q, "forward")
}
func (a *Adapter) ReverseGeocode(ctx context.Context, req provider.ReverseGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(req.Latitude, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(req.Longitude, 'f', 6, 64))
	q.Set("limit", "1")
	q.Set("format", "json")
	return a.fetch(ctx, "reverse", q, "reverse")
}
func (a *Adapter) fetch(ctx context.Context, operation string, q url.Values, mode string) (*provider.RawGeocodeResponse, error) {
	if a.key == "" {
		return nil, provider.ErrProviderDisabled
	}
	endpoint, err := url.Parse(a.manifest.BaseURL + "/" + operation)
	if err != nil {
		return nil, provider.ErrProviderUnavailable
	}
	q.Set("apiKey", a.key)
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
		return nil, fmt.Errorf("%w: geoapify transport failure", provider.ErrProviderUnavailable)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	default:
		return nil, fmt.Errorf("%w: geoapify upstream status %d", provider.ErrProviderUnavailable, response.StatusCode)
	}
	body, err := provider.ReadLimitedJSON(response.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid geocoding response size", provider.ErrMalformedResponse)
	}
	if len(body) == 0 || !strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		return nil, provider.ErrMalformedResponse
	}
	// Leave field-presence and coordinate validation to the canonical normalizer.
	return &provider.RawGeocodeResponse{ProviderID: a.manifest.ID, Mode: mode, RawPayload: body, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.GeocodingProvider = (*Adapter)(nil)
