package openweather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/http/client"
	"github.com/universal-api/platform/internal/provider"
)

type OpenWeatherRawPayload struct {
	Coord struct {
		Lon float64 `json:"lon"`
		Lat float64 `json:"lat"`
	} `json:"coord"`
	Weather []struct {
		ID          int    `json:"id"`
		Main        string `json:"main"`
		Description string `json:"description"`
	} `json:"weather"`
	Main struct {
		Temp     float64 `json:"temp"` // in Kelvin
		Humidity *int    `json:"humidity"`
	} `json:"main"`
	Name string `json:"name"`
	Dt   int64  `json:"dt"`
}

type OpenWeatherAdapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
	apiKey     string
}

func NewAdapter(apiKey string, baseURL string) *OpenWeatherAdapter {
	isTestURL := client.IsLoopbackTestEndpoint(baseURL)
	if baseURL == "" {
		baseURL = "https://api.openweathermap.org/data/2.5"
	}

	var httpClient *http.Client
	if isTestURL {
		httpClient = client.NewTestHTTPClient(5 * time.Second)
	} else {
		httpClient = client.NewHTTPClient(5 * time.Second)
	}

	return &OpenWeatherAdapter{
		manifest: provider.Manifest{
			ID:               "openweather",
			Name:             "OpenWeatherMap API",
			Version:          "2.5",
			Capabilities:     []string{"weather"},
			BaseURL:          baseURL,
			Timeout:          5 * time.Second,
			FallbackPriority: 1,
			Attribution: provider.Attribution{
				Required: true,
				Text:     "Weather data provided by OpenWeatherMap",
				URL:      "https://openweathermap.org",
			},
			Status: provider.StatusActive,
		},
		httpClient: httpClient,
		apiKey:     apiKey,
	}
}

func (a *OpenWeatherAdapter) WithHTTPClient(c *http.Client) *OpenWeatherAdapter {
	a.httpClient = c
	return a
}

func (a *OpenWeatherAdapter) Manifest() provider.Manifest {
	return a.manifest
}

func (a *OpenWeatherAdapter) FetchWeather(ctx context.Context, location string) (*provider.RawWeatherResponse, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil, errors.New("openweather: location required")
	}

	endpoint, err := url.Parse(a.manifest.BaseURL + "/weather")
	if err != nil {
		return nil, fmt.Errorf("openweather: invalid base url: %w", err)
	}

	q := endpoint.Query()

	// Check if location is coordinates (e.g. "51.5074,-0.1278")
	if strings.Contains(location, ",") {
		parts := strings.Split(location, ",")
		if len(parts) == 2 {
			latStr := strings.TrimSpace(parts[0])
			lonStr := strings.TrimSpace(parts[1])
			if lat, errLat := strconv.ParseFloat(latStr, 64); errLat == nil {
				if lon, errLon := strconv.ParseFloat(lonStr, 64); errLon == nil {
					q.Set("lat", fmt.Sprintf("%.6f", lat))
					q.Set("lon", fmt.Sprintf("%.6f", lon))
				} else {
					q.Set("q", location)
				}
			} else {
				q.Set("q", location)
			}
		} else {
			q.Set("q", location)
		}
	} else {
		q.Set("q", location)
	}

	if a.apiKey != "" {
		q.Set("appid", a.apiKey)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("openweather: failed to create http request: %w", err)
	}

	req.Header.Set("User-Agent", "UniversalAPIPlatform/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: upstream transport failed", provider.ErrProviderUnavailable)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := provider.ReadLimitedJSON(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read response body", provider.ErrMalformedResponse)
		}

		var payload OpenWeatherRawPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
		}

		return &provider.RawWeatherResponse{
			FreshTTL:    provider.ForecastFreshTTL(resp.Header, time.Now()),
			ProviderID:  a.manifest.ID,
			RawPayload:  payload,
			RawJSON:     json.RawMessage(body),
			RetrievedAt: time.Now().UTC(),
		}, nil

	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, provider.ErrProviderDisabled
	case http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
}
