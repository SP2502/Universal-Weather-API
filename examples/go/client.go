package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	api "github.com/universal-api/platform/internal/api"
	weather "github.com/universal-api/platform/schemas/weather/v1"
)

type WeatherResponse struct {
	Data weather.WeatherResponse `json:"data"`
	Meta api.Meta                `json:"meta"`
}
type Client struct {
	baseURL, apiKey string
	httpClient      *http.Client
}

func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, httpClient: &http.Client{Timeout: 15 * time.Second}}
}
func (c *Client) GetWeather(ctx context.Context, place string) (*WeatherResponse, error) {
	endpoint, e := url.Parse(c.baseURL + "/v1/weather")
	if e != nil {
		return nil, e
	}
	q := endpoint.Query()
	q.Set("location", place)
	endpoint.RawQuery = q.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("X-API-Key", c.apiKey)
	resp, e := c.httpClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("UWA HTTP %d", resp.StatusCode)
	}
	var output WeatherResponse
	if e := json.NewDecoder(resp.Body).Decode(&output); e != nil {
		return nil, e
	}
	if output.Data.SchemaVersion != "weather.v1" || output.Data.Temperature.Unit != "celsius" {
		return nil, errors.New("unexpected UWA canonical schema")
	}
	return &output, nil
}
func main() {
	key := os.Getenv("UWA_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "Set UWA_API_KEY")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := NewClient(key, os.Getenv("UWA_BASE_URL")).GetWeather(ctx, "London")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s: %0.1f°C (%s) from %s\n", out.Data.Location.City, out.Data.Temperature.Value, out.Data.Condition, out.Meta.Provider)
}
