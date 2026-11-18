package mock

import (
	"context"
	"errors"
	"time"
)

type WeatherData struct {
	Location    string    `json:"location"`
	Temperature float64   `json:"temp_c"`
	Humidity    int       `json:"humidity_pct"`
	Condition   string    `json:"condition_text"`
	ReportedAt  time.Time `json:"reported_at"`
}

type MockWeatherProvider struct {
	ProviderName string
}

func NewMockWeatherProvider(name string) *MockWeatherProvider {
	if name == "" {
		name = "mock-weather-provider"
	}
	return &MockWeatherProvider{ProviderName: name}
}

func (p *MockWeatherProvider) Name() string {
	return p.ProviderName
}

func (p *MockWeatherProvider) FetchCurrentWeather(ctx context.Context, location string) (*WeatherData, error) {
	if location == "" {
		return nil, errors.New("location query is required")
	}

	return &WeatherData{
		Location:    location,
		Temperature: 22.5,
		Humidity:    55,
		Condition:   "Sunny",
		ReportedAt:  time.Now().UTC(),
	}, nil
}
