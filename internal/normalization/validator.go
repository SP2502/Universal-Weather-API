package normalization

import (
	"errors"
	"fmt"
	"math"
	"time"
	_ "time/tzdata"

	locv1 "github.com/universal-api/platform/schemas/location/v1"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

// MaxObservationAge is a conservative gateway integrity ceiling, not a claim
// about a vendor's publication frequency. Source observation time is retained.
const MaxObservationAge = 2 * time.Hour

func ValidateWeather(w *weatherv1.WeatherResponse) error {
	return ValidateWeatherAt(w, time.Now())
}

func ValidateWeatherAt(w *weatherv1.WeatherResponse, now time.Time) error {
	if w == nil {
		return errors.New("weather response is nil")
	}

	if err := ValidateLocation(&w.Location); err != nil {
		return fmt.Errorf("weather location invalid: %w", err)
	}

	// Temperature bounds [-100C, 70C]
	if math.IsNaN(w.Temperature.Value) || math.IsInf(w.Temperature.Value, 0) || w.Temperature.Value < -100 || w.Temperature.Value > 70 {
		return fmt.Errorf("temperature out of plausible range [-100C, 70C]: %f", w.Temperature.Value)
	}
	if w.Temperature.Unit != "celsius" {
		return fmt.Errorf("invalid canonical temperature unit: %s", w.Temperature.Unit)
	}

	// Humidity bounds [0, 100] if present
	if w.Humidity != nil {
		if w.Humidity.Value < 0 || w.Humidity.Value > 100 {
			return fmt.Errorf("humidity out of range [0, 100]: %d", w.Humidity.Value)
		}
		if w.Humidity.Unit != "percent" {
			return fmt.Errorf("invalid canonical humidity unit: %s", w.Humidity.Unit)
		}
	}

	// Condition enum check
	switch w.Condition {
	case weatherv1.ConditionClear,
		weatherv1.ConditionPartlyCloudy,
		weatherv1.ConditionCloudy,
		weatherv1.ConditionRain,
		weatherv1.ConditionSnow,
		weatherv1.ConditionThunderstorm,
		weatherv1.ConditionFog,
		weatherv1.ConditionUnknown:
	default:
		return fmt.Errorf("invalid canonical weather condition: %s", w.Condition)
	}

	if w.SchemaVersion != "weather.v1" {
		return fmt.Errorf("unexpected weather schema version")
	}
	if w.ObservedAt.IsZero() {
		return errors.New("observed_at timestamp cannot be zero")
	}
	// This is current-weather, not the historical product. Protect the API from
	// bad epochs and future timestamps without silently replacing observations.
	if w.ObservedAt.Before(now.Add(-MaxObservationAge)) || w.ObservedAt.After(now.Add(15*time.Minute)) {
		return errors.New("observed_at timestamp outside supported current-weather bounds")
	}
	if w.SourceCondition != nil && (w.SourceCondition.Provider == "" || w.SourceCondition.Code < 0) {
		return errors.New("invalid source condition metadata")
	}

	return nil
}

func ValidateLocation(loc *locv1.Location) error {
	if loc == nil {
		return errors.New("location is nil")
	}
	if math.IsNaN(loc.Latitude) || math.IsInf(loc.Latitude, 0) || loc.Latitude < -90 || loc.Latitude > 90 {
		return fmt.Errorf("latitude out of range [-90, 90]: %f", loc.Latitude)
	}
	if math.IsNaN(loc.Longitude) || math.IsInf(loc.Longitude, 0) || loc.Longitude < -180 || loc.Longitude > 180 {
		return fmt.Errorf("longitude out of range [-180, 180]: %f", loc.Longitude)
	}
	if math.IsNaN(loc.Confidence) || math.IsInf(loc.Confidence, 0) || loc.Confidence < 0 || loc.Confidence > 1 {
		return fmt.Errorf("invalid confidence score: %v", loc.Confidence)
	}
	if err := validateTimezone(loc.Timezone); err != nil {
		return err
	}
	if loc.CountryCode != "" {
		if len(loc.CountryCode) != 2 || loc.CountryCode[0] < 'A' || loc.CountryCode[0] > 'Z' || loc.CountryCode[1] < 'A' || loc.CountryCode[1] > 'Z' {
			return errors.New("country_code must be ISO alpha-2 uppercase")
		}
	}
	return nil
}

func validateTimezone(zone string) error {
	if zone == "" {
		return nil
	}
	if zone == "Local" {
		return errors.New("timezone must identify a portable IANA zone")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return errors.New("invalid timezone identifier")
	}
	return nil
}
