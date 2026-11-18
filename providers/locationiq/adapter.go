package locationiq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
)

type LocationIQRawPlace struct {
	PlaceID     string  `json:"place_id"`
	Lat         string  `json:"lat"`
	Lon         string  `json:"lon"`
	DisplayName string  `json:"display_name"`
	Class       string  `json:"class"`
	Type        string  `json:"type"`
	Importance  float64 `json:"importance"`
	Address     struct {
		Name        string `json:"name,omitempty"`
		HouseNumber string `json:"house_number,omitempty"`
		Road        string `json:"road,omitempty"`
		County      string `json:"county,omitempty"`
		Suburb      string `json:"suburb,omitempty"`
		City        string `json:"city,omitempty"`
		State       string `json:"state,omitempty"`
		Postcode    string `json:"postcode,omitempty"`
		Country     string `json:"country,omitempty"`
		CountryCode string `json:"country_code,omitempty"`
	} `json:"address"`
}

type LocationIQAdapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
	apiKey     string
}

func NewAdapter(apiKey string, baseURL string) *LocationIQAdapter {
	isTestURL := client.IsLoopbackTestEndpoint(baseURL)
	if baseURL == "" {
		baseURL = "https://us1.locationiq.com/v1"
	}

	var httpClient *http.Client
	if isTestURL {
		httpClient = client.NewTestHTTPClient(5 * time.Second)
	} else {
		httpClient = client.NewHTTPClient(5 * time.Second)
	}

	return &LocationIQAdapter{
		manifest: provider.Manifest{
			ID:               "locationiq",
			Name:             "LocationIQ Geocoding API",
			Version:          "v1",
			Capabilities:     []string{"geocode"},
			BaseURL:          baseURL,
			Timeout:          5 * time.Second,
			FallbackPriority: 2,
			Attribution: provider.Attribution{
				Required: true,
				Text:     "Search powered by LocationIQ",
				URL:      "https://locationiq.com",
			},
			Status: provider.StatusActive,
		},
		httpClient: httpClient,
		apiKey:     apiKey,
	}
}

func (a *LocationIQAdapter) WithHTTPClient(c *http.Client) *LocationIQAdapter {
	a.httpClient = c
	return a
}

func (a *LocationIQAdapter) Manifest() provider.Manifest {
	return a.manifest
}

func (a *LocationIQAdapter) ForwardGeocode(ctx context.Context, req provider.ForwardGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	if req.Query == "" {
		return nil, errors.New("locationiq: forward geocode query is required")
	}

	endpoint, err := url.Parse(a.manifest.BaseURL + "/search.php")
	if err != nil {
		return nil, fmt.Errorf("locationiq: invalid base url: %w", err)
	}

	q := endpoint.Query()
	q.Set("q", req.Query)
	q.Set("format", "json")
	if a.apiKey != "" {
		q.Set("key", a.apiKey)
	}
	if req.CountryCode != "" {
		q.Set("countrycodes", req.CountryCode)
	}
	if req.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", req.Limit))
	}
	endpoint.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("locationiq: failed to create http request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "UniversalAPIPlatform/1.0")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}

	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read body", provider.ErrMalformedResponse)
	}

	var items []LocationIQRawPlace
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}

	return &provider.RawGeocodeResponse{
		ProviderID:  a.manifest.ID,
		Mode:        "forward",
		RawPayload:  items,
		RetrievedAt: time.Now().UTC(),
	}, nil
}

func (a *LocationIQAdapter) ReverseGeocode(ctx context.Context, req provider.ReverseGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	endpoint, err := url.Parse(a.manifest.BaseURL + "/reverse.php")
	if err != nil {
		return nil, fmt.Errorf("locationiq: invalid base url: %w", err)
	}

	q := endpoint.Query()
	q.Set("lat", fmt.Sprintf("%f", req.Latitude))
	q.Set("lon", fmt.Sprintf("%f", req.Longitude))
	q.Set("format", "json")
	if a.apiKey != "" {
		q.Set("key", a.apiKey)
	}
	endpoint.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("locationiq: failed to create http request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "UniversalAPIPlatform/1.0")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}

	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read body", provider.ErrMalformedResponse)
	}

	var item LocationIQRawPlace
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}

	return &provider.RawGeocodeResponse{
		ProviderID:  a.manifest.ID,
		Mode:        "reverse",
		RawPayload:  item,
		RetrievedAt: time.Now().UTC(),
	}, nil
}
