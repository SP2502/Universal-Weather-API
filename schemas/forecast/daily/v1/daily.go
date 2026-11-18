// Package v1 defines daily UTC summaries derived from sampled forecast points.
// These values are NOT vendor-supplied daily extrema or observations.
package v1

import (
	loc "github.com/universal-api/platform/schemas/location/v1"
	"time"
)

type Day struct {
	DateUTC               string    `json:"date_utc"`
	MinimumSampledCelsius float64   `json:"minimum_sampled_celsius"`
	MaximumSampledCelsius float64   `json:"maximum_sampled_celsius"`
	SampleCount           int       `json:"sample_count"`
	FirstValidAt          time.Time `json:"first_valid_at"`
	LastValidAt           time.Time `json:"last_valid_at"`
}

type Response struct {
	SchemaVersion    string       `json:"schema_version"`
	DataKind         string       `json:"data_kind"`
	AggregationBasis string       `json:"aggregation_basis"`
	Timezone         string       `json:"timezone"`
	Location         loc.Location `json:"location"`
	IssuedAt         *time.Time   `json:"issued_at,omitempty"`
	Days             []Day        `json:"days"`
}
