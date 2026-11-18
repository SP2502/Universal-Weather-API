// Package v1 defines source-attributed pollutant concentration forecasts.
// It is not a station measurement or an AQI category/index.
package v1

import (
	location "github.com/universal-api/platform/schemas/location/v1"
	"time"
)

type Concentration struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}
type Point struct {
	ValidAt         time.Time      `json:"valid_at"`
	PM25            *Concentration `json:"pm2_5,omitempty"`
	PM10            *Concentration `json:"pm10,omitempty"`
	Ozone           *Concentration `json:"ozone,omitempty"`
	NitrogenDioxide *Concentration `json:"nitrogen_dioxide,omitempty"`
	SulphurDioxide  *Concentration `json:"sulphur_dioxide,omitempty"`
	CarbonMonoxide  *Concentration `json:"carbon_monoxide,omitempty"`
}
type Response struct {
	SchemaVersion string            `json:"schema_version"`
	DataKind      string            `json:"data_kind"`
	ModelSource   string            `json:"model_source"`
	Location      location.Location `json:"location"`
	Points        []Point           `json:"points"`
}
