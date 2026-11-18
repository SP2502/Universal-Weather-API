// Package v1 defines a forecast-only canonical contract. This is NOT the
// measured-current-weather weather.v1 contract; valid_at is a prediction time.
package v1

import (
	loc "github.com/universal-api/platform/schemas/location/v1"
	weather "github.com/universal-api/platform/schemas/weather/v1"
	"time"
)

type Point struct {
	ValidAt         time.Time           `json:"valid_at"`
	Temperature     weather.Temperature `json:"temperature"`
	Humidity        *weather.Humidity   `json:"humidity,omitempty"`
	Condition       weather.Condition   `json:"condition"`
	SourceCondition string              `json:"source_condition,omitempty"`
}
type ForecastResponse struct {
	SchemaVersion string       `json:"schema_version"`
	DataKind      string       `json:"data_kind"`
	Location      loc.Location `json:"location"`
	IssuedAt      *time.Time   `json:"issued_at,omitempty"`
	Points        []Point      `json:"points"`
}
