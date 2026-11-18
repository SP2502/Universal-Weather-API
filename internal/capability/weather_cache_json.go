package capability

import (
	"encoding/json"
	"errors"
	"time"
)

// Preserve required-field presence when decoding persisted weather. Validating
// Go zero values alone would fabricate 0°C or (0,0) from a truncated cache item.
func (item *CachedWeatherPayload) UnmarshalJSON(data []byte) error {
	var presence struct {
		Data *struct {
			SchemaVersion *string `json:"schema_version"`
			Location      *struct {
				Latitude  *float64 `json:"latitude"`
				Longitude *float64 `json:"longitude"`
			} `json:"location"`
			Temperature *struct {
				Value *float64 `json:"value"`
				Unit  *string  `json:"unit"`
			} `json:"temperature"`
			Humidity *struct {
				Value *int    `json:"value"`
				Unit  *string `json:"unit"`
			} `json:"humidity"`
			Condition       *string    `json:"condition"`
			ObservedAt      *time.Time `json:"observed_at"`
			SourceCondition *struct {
				Provider *string `json:"provider"`
				Code     *int    `json:"code"`
			} `json:"source_condition"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	p := presence.Data
	if p == nil || p.SchemaVersion == nil || p.Location == nil || p.Location.Latitude == nil || p.Location.Longitude == nil || p.Temperature == nil || p.Temperature.Value == nil || p.Temperature.Unit == nil || p.Condition == nil || p.ObservedAt == nil {
		return errors.New("cached weather required fields missing")
	}
	if p.Humidity != nil && (p.Humidity.Value == nil || p.Humidity.Unit == nil) {
		return errors.New("cached humidity required fields missing")
	}
	if p.SourceCondition != nil && (p.SourceCondition.Provider == nil || p.SourceCondition.Code == nil) {
		return errors.New("cached source condition required fields missing")
	}
	type payload CachedWeatherPayload
	var decoded payload
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*item = CachedWeatherPayload(decoded)
	return nil
}
