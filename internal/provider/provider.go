package provider

import (
	"context"
	"encoding/json"
	"time"
)

type Provider interface {
	Manifest() Manifest
}

type RawWeatherResponse struct {
	FreshTTL    *time.Duration  `json:"-"` // nonpositive forbids storage/stale reuse
	ProviderID  string          `json:"provider_id"`
	RawPayload  interface{}     `json:"raw_payload"`
	RawJSON     json.RawMessage `json:"-"` // Exact upstream JSON, preserving missing fields before typed Go decoding.
	RetrievedAt time.Time       `json:"retrieved_at"`
}

type WeatherProvider interface {
	Provider
	FetchWeather(ctx context.Context, location string) (*RawWeatherResponse, error)
}

type ForwardGeocodeRequest struct {
	Query       string `json:"query"`
	CountryCode string `json:"country_code,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ReverseGeocodeRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type RawGeocodeResponse struct {
	ProviderID  string      `json:"provider_id"`
	Mode        string      `json:"mode"` // "forward" or "reverse"
	RawPayload  interface{} `json:"raw_payload"`
	RetrievedAt time.Time   `json:"retrieved_at"`
}

type GeocodingProvider interface {
	Provider
	ForwardGeocode(ctx context.Context, req ForwardGeocodeRequest) (*RawGeocodeResponse, error)
	ReverseGeocode(ctx context.Context, req ReverseGeocodeRequest) (*RawGeocodeResponse, error)
}

// RawForecastResponse carries model forecasts; never pass it to a
// WeatherProvider or normalize it into weather.v1 observations.
type RawForecastResponse struct {
	ProviderID  string
	RawJSON     json.RawMessage
	RetrievedAt time.Time
	// Explicit requested coordinates for upstream schemas that omit a point
	// geometry (NWS grid forecasts and Bright Sky hourly station results).
	Latitude                 *float64
	Longitude                *float64
	MaxStationDistanceMeters int
	// If present, this upstream-provided TTL is an upper bound for the
	// gateway's forecast cache. A non-positive value disables caching.
	FreshTTL *time.Duration
}
type ForecastProvider interface {
	Provider
	FetchForecast(ctx context.Context, latitude, longitude float64) (*RawForecastResponse, error)
}

// RawAirQualityResponse is modeled pollutant data. It must not be interpreted
// as measured AQI or station measurements.
type RawAirQualityResponse struct {
	ProviderID  string
	RawJSON     json.RawMessage
	RetrievedAt time.Time
}
type AirQualityProvider interface {
	Provider
	FetchAirQuality(ctx context.Context, latitude, longitude float64) (*RawAirQualityResponse, error)
}

// Phase 07D products have distinct request contracts and source provenance.
// They must never be routed as live current-weather observations.
type RawExtendedResponse struct {
	ProviderID  string
	RawJSON     json.RawMessage
	RetrievedAt time.Time
}
type HistoryProvider interface {
	Provider
	FetchHistory(ctx context.Context, latitude, longitude float64, start, end time.Time) (*RawExtendedResponse, error)
}
type AstronomyProvider interface {
	Provider
	FetchAstronomy(ctx context.Context, latitude, longitude float64, date string) (*RawExtendedResponse, error)
}
type MarineProvider interface {
	Provider
	FetchMarine(ctx context.Context, latitude, longitude float64) (*RawExtendedResponse, error)
}
type AlertsProvider interface {
	Provider
	FetchAlerts(ctx context.Context, latitude, longitude float64) (*RawExtendedResponse, error)
}
