package v1

import (
	"time"

	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

type Condition string

const (
	ConditionClear        Condition = "clear"
	ConditionPartlyCloudy Condition = "partly_cloudy"
	ConditionCloudy       Condition = "cloudy"
	ConditionRain         Condition = "rain"
	ConditionSnow         Condition = "snow"
	ConditionThunderstorm Condition = "thunderstorm"
	ConditionFog          Condition = "fog"
	ConditionUnknown      Condition = "unknown"
)

type Temperature struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // Always "celsius" in canonical v1
}

// SourceCondition preserves upstream terminology without guessing that an
// unrepresentable phenomenon belongs to an unrelated canonical category.
type SourceCondition struct {
	Provider    string `json:"provider"`
	Code        int    `json:"code"`
	Text        string `json:"text,omitempty"`
	Description string `json:"description,omitempty"`
}

type Humidity struct {
	Value int    `json:"value"`
	Unit  string `json:"unit"` // Always "percent" in canonical v1
}

type WeatherResponse struct {
	SchemaVersion   string           `json:"schema_version"` // "weather.v1"
	Location        locv1.Location   `json:"location"`
	Temperature     Temperature      `json:"temperature"`
	Humidity        *Humidity        `json:"humidity,omitempty"`
	Condition       Condition        `json:"condition"`
	SourceCondition *SourceCondition `json:"source_condition,omitempty"`
	ObservedAt      time.Time        `json:"observed_at"`
}
