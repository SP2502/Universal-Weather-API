// Package v1 contains separate, provenance-explicit contracts for Phase 07D.
package v1

import "time"

type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type HistoricalPoint struct {
	ObservedAt      time.Time `json:"valid_at"` // Reanalysis valid-time, not station observation time.
	TemperatureC    float64   `json:"temperature_c"`
	HumidityPercent *float64  `json:"humidity_percent,omitempty"`
	PrecipitationMM *float64  `json:"precipitation_mm,omitempty"`
}
type HistoryResponse struct {
	SchemaVersion string            `json:"schema_version"`
	DataKind      string            `json:"data_kind"` // historical_reanalysis
	Model         string            `json:"model"`
	Location      GeoPoint          `json:"location"`
	Points        []HistoricalPoint `json:"points"`
}

type MarinePoint struct {
	ValidAt              time.Time `json:"valid_at"`
	WaveHeightM          float64   `json:"wave_height_m"`
	WavePeriodS          *float64  `json:"wave_period_s,omitempty"`
	WaveDirectionDegrees *float64  `json:"wave_direction_degrees,omitempty"`
}
type MarineResponse struct {
	SchemaVersion string        `json:"schema_version"`
	DataKind      string        `json:"data_kind"` // marine_model_forecast
	ModelGrid     GeoPoint      `json:"model_grid"`
	Points        []MarinePoint `json:"points"`
}

type AstronomyResponse struct {
	SchemaVersion string     `json:"schema_version"`
	DataKind      string     `json:"data_kind"` // astronomical_calculation
	Date          string     `json:"date"`
	Timezone      string     `json:"timezone"`
	Location      GeoPoint   `json:"location"`
	SunStatus     string     `json:"sun_status"`
	Sunrise       *time.Time `json:"sunrise"`
	Sunset        *time.Time `json:"sunset"`
	SolarNoon     *time.Time `json:"solar_noon"`
	Moonrise      *time.Time `json:"moonrise"`
	Moonset       *time.Time `json:"moonset"`
	MoonPhase     string     `json:"moon_phase,omitempty"`
}

type Alert struct {
	ID              string     `json:"id"`
	Event           string     `json:"event"`
	Headline        string     `json:"headline,omitempty"`
	Severity        string     `json:"severity,omitempty"`
	Status          string     `json:"status,omitempty"`
	Urgency         string     `json:"urgency,omitempty"`
	Certainty       string     `json:"certainty,omitempty"`
	AreaDescription string     `json:"area_description,omitempty"`
	Effective       *time.Time `json:"effective,omitempty"`
	Expires         time.Time  `json:"expires"`
	Description     string     `json:"description,omitempty"`
	Instruction     string     `json:"instruction,omitempty"`
}
type AlertsResponse struct {
	SchemaVersion string   `json:"schema_version"`
	DataKind      string   `json:"data_kind"` // official_active_alerts
	Location      GeoPoint `json:"location"`
	Alerts        []Alert  `json:"alerts"`
}
