package nominatim

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

type NominatimRawPlace struct {
	PlaceID     int64   `json:"place_id"`
	Licence     string  `json:"licence"`
	OSMType     string  `json:"osm_type"`
	OSMID       int64   `json:"osm_id"`
	Lat         string  `json:"lat"`
	Lon         string  `json:"lon"`
	DisplayName string  `json:"display_name"`
	Class       string  `json:"class"`
	Type        string  `json:"type"`
	Importance  float64 `json:"importance"`
	Address     struct {
		HouseNumber string `json:"house_number,omitempty"`
		Road        string `json:"road,omitempty"`
		Suburb      string `json:"suburb,omitempty"`
		City        string `json:"city,omitempty"`
		County      string `json:"county,omitempty"`
		State       string `json:"state,omitempty"`
		Postcode    string `json:"postcode,omitempty"`
		Country     string `json:"country,omitempty"`
		CountryCode string `json:"country_code,omitempty"`
	} `json:"address"`
}

type NominatimAdapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
}

func NewAdapter(baseURL string) *NominatimAdapter {
	isTestURL := client.IsLoopbackTestEndpoint(baseURL)
	if baseURL == "" {
		baseURL = "https://nominatim.openstreetmap.org"
	}

	var httpClient *http.Client
	if isTestURL {
		httpClient = client.NewTestHTTPClient(5 * time.Second)
	} else {
		httpClient = client.NewHTTPClient(5 * time.Second)
	}

	return &NominatimAdapter{
		manifest: provider.Manifest{
			ID:               "nominatim",
			Name:             "OpenStreetMap Nominatim",
			Version:          "v1",
			Capabilities:     []string{"geocode"},
			BaseURL:          baseURL,
			Timeout:          5 * time.Second,
			FallbackPriority: 1,
			Attribution: provider.Attribution{
				Required: true,
				Text:     "© OpenStreetMap contributors",
				URL:      "https://www.openstreetmap.org/copyright",
			},
			Status: provider.StatusActive,
		},
		httpClient: httpClient,
	}
}

func (a *NominatimAdapter) WithHTTPClient(c *http.Client) *NominatimAdapter {
	a.httpClient = c
	return a
}

func (a *NominatimAdapter) Manifest() provider.Manifest {
	return a.manifest
}

func (a *NominatimAdapter) ForwardGeocode(ctx context.Context, req provider.ForwardGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	if req.Query == "" {
		return nil, errors.New("nominatim: forward geocode query is required")
	}

	endpoint, err := url.Parse(a.manifest.BaseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("nominatim: invalid base url: %w", err)
	}

	q := endpoint.Query()
	q.Set("q", req.Query)
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	if req.CountryCode != "" {
		q.Set("countrycodes", req.CountryCode)
	}
	if req.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", req.Limit))
	}
	endpoint.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("nominatim: failed to create http request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "UniversalAPIPlatform/1.0 (contact@universal-api.org)")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream geocoding transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}

	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read body", provider.ErrMalformedResponse)
	}

	var items []NominatimRawPlace
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

func (a *NominatimAdapter) ReverseGeocode(ctx context.Context, req provider.ReverseGeocodeRequest) (*provider.RawGeocodeResponse, error) {
	endpoint, err := url.Parse(a.manifest.BaseURL + "/reverse")
	if err != nil {
		return nil, fmt.Errorf("nominatim: invalid base url: %w", err)
	}

	q := endpoint.Query()
	q.Set("lat", fmt.Sprintf("%f", req.Latitude))
	q.Set("lon", fmt.Sprintf("%f", req.Longitude))
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "1")
	endpoint.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("nominatim: failed to create http request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "UniversalAPIPlatform/1.0 (contact@universal-api.org)")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream geocoding transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}

	body, err := provider.ReadLimitedJSON(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read body", provider.ErrMalformedResponse)
	}

	var item NominatimRawPlace
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
