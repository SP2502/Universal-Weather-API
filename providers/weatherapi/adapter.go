package weatherapi

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

type WeatherAPIRawPayload struct {
	Location struct {
		Name    string  `json:"name"`
		Country string  `json:"country"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
		TzID    string  `json:"tz_id"`
	} `json:"location"`
	Current struct {
		TempC     float64 `json:"temp_c"`
		TempF     float64 `json:"temp_f"`
		Humidity  *int    `json:"humidity"`
		Condition struct {
			Text string `json:"text"`
			Code int    `json:"code"`
		} `json:"condition"`
		LastUpdatedEpoch int64 `json:"last_updated_epoch"`
	} `json:"current"`
}

type WeatherAPIAdapter struct {
	manifest   provider.Manifest
	httpClient *http.Client
	apiKey     string
}

func NewAdapter(apiKey string, baseURL string) *WeatherAPIAdapter {
	isTestURL := client.IsLoopbackTestEndpoint(baseURL)
	if baseURL == "" {
		baseURL = "https://api.weatherapi.com/v1"
	}

	var httpClient *http.Client
	if isTestURL {
		httpClient = client.NewTestHTTPClient(5 * time.Second)
	} else {
		httpClient = client.NewHTTPClient(5 * time.Second)
	}

	return &WeatherAPIAdapter{
		manifest: provider.Manifest{
			ID:               "weatherapi",
			Name:             "WeatherAPI.com",
			Version:          "v1",
			Capabilities:     []string{"weather"},
			BaseURL:          baseURL,
			Timeout:          5 * time.Second,
			FallbackPriority: 2,
			Attribution: provider.Attribution{
				Required: true,
				Text:     "Powered by WeatherAPI.com",
				URL:      "https://www.weatherapi.com",
			},
			Status: provider.StatusActive,
		},
		httpClient: httpClient,
		apiKey:     apiKey,
	}
}

func (a *WeatherAPIAdapter) WithHTTPClient(c *http.Client) *WeatherAPIAdapter {
	a.httpClient = c
	return a
}

func (a *WeatherAPIAdapter) Manifest() provider.Manifest {
	return a.manifest
}

func (a *WeatherAPIAdapter) FetchWeather(ctx context.Context, location string) (*provider.RawWeatherResponse, error) {
	if location == "" {
		return nil, errors.New("weatherapi: location required")
	}

	endpoint, err := url.Parse(a.manifest.BaseURL + "/current.json")
	if err != nil {
		return nil, fmt.Errorf("weatherapi: invalid base url: %w", err)
	}

	q := endpoint.Query()
	q.Set("q", location)
	if a.apiKey != "" {
		q.Set("key", a.apiKey)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("weatherapi: failed to create http request: %w", err)
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

		var payload WeatherAPIRawPayload
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

	case http.StatusBadRequest:
		body, readErr := provider.ReadLimitedJSON(resp.Body)
		if readErr != nil {
			return nil, provider.ErrMalformedResponse
		}
		var doc struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &doc) != nil {
			return nil, provider.ErrMalformedResponse
		}
		switch doc.Error.Code {
		case 1003, 1005, 1006:
			return nil, provider.ErrProviderNotFound
		default:
			return nil, provider.ErrProviderUnavailable
		}
	case http.StatusForbidden:
		body, readErr := provider.ReadLimitedJSON(resp.Body)
		if readErr == nil {
			var doc struct {
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &doc) == nil && doc.Error.Code == 2007 {
				return nil, provider.ErrRateLimited
			}
		}
		return nil, provider.ErrProviderDisabled
	case http.StatusUnauthorized:
		return nil, provider.ErrProviderDisabled
	case http.StatusNotFound:
		return nil, provider.ErrProviderNotFound
	case http.StatusTooManyRequests:
		return nil, provider.ErrRateLimited
	default:
		return nil, fmt.Errorf("%w: status code %d", provider.ErrProviderUnavailable, resp.StatusCode)
	}
}
