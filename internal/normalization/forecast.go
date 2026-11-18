package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/provider"
	forecast "github.com/universal-api/platform/schemas/forecast/v1"
	loc "github.com/universal-api/platform/schemas/location/v1"
	weather "github.com/universal-api/platform/schemas/weather/v1"
)

// MaxForecastPoints bounds memory and response size across providers.
const MaxForecastPoints = 168

type ForecastNormalizer struct{}

func NewForecastNormalizer() *ForecastNormalizer { return &ForecastNormalizer{} }

// A forecast value's valid_at is NOT an observed_at. All points are checked
// and missing/NaN temperatures cause provider fallback, never fabricated zero.
func (n *ForecastNormalizer) Normalize(raw *provider.RawForecastResponse) (*forecast.ForecastResponse, error) {
	if raw == nil || !json.Valid(raw.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var out *forecast.ForecastResponse
	var err error
	switch raw.ProviderID {
	case "met_norway":
		out, err = n.met(raw.RawJSON)
	case "open_meteo_forecast":
		out, err = n.openMeteo(raw.RawJSON)
	case "us_nws":
		out, err = n.usNWS(raw)
	case "bright_sky":
		out, err = n.brightSky(raw)
	default:
		return nil, fmt.Errorf("unsupported forecast provider %q", raw.ProviderID)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}
	if err := ValidateForecast(out); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}
	return out, nil
}
func (n *ForecastNormalizer) met(payload []byte) (*forecast.ForecastResponse, error) {
	var doc struct {
		Geometry *struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties *struct {
			Meta struct {
				UpdatedAt string            `json:"updated_at"`
				Units     map[string]string `json:"units"`
			} `json:"meta"`
			Timeseries []struct {
				Time string `json:"time"`
				Data struct {
					Instant *struct {
						Details *struct {
							AirTemperature   *float64 `json:"air_temperature"`
							RelativeHumidity *float64 `json:"relative_humidity"`
						} `json:"details"`
					} `json:"instant"`
					Next1Hours *struct {
						Summary *struct {
							SymbolCode string `json:"symbol_code"`
						} `json:"summary"`
					} `json:"next_1_hours"`
				} `json:"data"`
			} `json:"timeseries"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	if doc.Geometry == nil || len(doc.Geometry.Coordinates) < 2 || doc.Properties == nil || len(doc.Properties.Timeseries) == 0 {
		return nil, errors.New("missing forecast coordinates or timeseries")
	}
	for field, expected := range map[string]string{"air_temperature": "celsius", "relative_humidity": "%"} {
		if unit, present := doc.Properties.Meta.Units[field]; present && unit != expected {
			return nil, errors.New("contradictory MET forecast units")
		}
	}
	out := &forecast.ForecastResponse{SchemaVersion: "forecast.v1", DataKind: "model_forecast", Location: loc.Location{Latitude: doc.Geometry.Coordinates[1], Longitude: doc.Geometry.Coordinates[0]}, Points: make([]forecast.Point, 0, MaxForecastPoints)}
	if doc.Properties.Meta.UpdatedAt != "" {
		issued, err := time.Parse(time.RFC3339, doc.Properties.Meta.UpdatedAt)
		if err != nil {
			return nil, errors.New("invalid issued_at")
		}
		out.IssuedAt = &issued
	}
	for _, item := range doc.Properties.Timeseries {
		t, err := time.Parse(time.RFC3339, item.Time)
		if err != nil {
			return nil, errors.New("invalid forecast timestamp")
		}
		if t.Before(time.Now().Add(-time.Hour)) {
			continue
		}
		if item.Data.Instant == nil || item.Data.Instant.Details == nil || item.Data.Instant.Details.AirTemperature == nil {
			return nil, errors.New("required forecast temperature missing")
		}
		temp := *item.Data.Instant.Details.AirTemperature
		pt := forecast.Point{ValidAt: t.UTC(), Temperature: weather.Temperature{Value: temp, Unit: "celsius"}, Condition: weather.ConditionUnknown}
		if x := item.Data.Instant.Details.RelativeHumidity; x != nil {
			if math.IsNaN(*x) || math.IsInf(*x, 0) || *x < 0 || *x > 100 {
				return nil, errors.New("invalid humidity")
			}
			pt.Humidity = &weather.Humidity{Value: int(math.Round(*x)), Unit: "percent"}
		}
		if item.Data.Next1Hours != nil && item.Data.Next1Hours.Summary != nil {
			pt.SourceCondition = item.Data.Next1Hours.Summary.SymbolCode
			pt.Condition = metSymbol(pt.SourceCondition)
		}
		out.Points = append(out.Points, pt)
		if len(out.Points) >= MaxForecastPoints {
			break
		}
	}
	return out, nil
}
func metSymbol(code string) weather.Condition {
	code = strings.ToLower(code)
	switch {
	case strings.HasPrefix(code, "clearsky"):
		return weather.ConditionClear
	case strings.HasPrefix(code, "fair"), strings.HasPrefix(code, "partlycloudy"):
		return weather.ConditionPartlyCloudy
	case strings.HasPrefix(code, "cloudy"):
		return weather.ConditionCloudy
	case strings.Contains(code, "thunder"):
		return weather.ConditionThunderstorm
	case strings.Contains(code, "sleet"):
		return weather.ConditionUnknown
	case strings.Contains(code, "snow"):
		return weather.ConditionSnow
	case strings.Contains(code, "rain"), strings.Contains(code, "showers"):
		return weather.ConditionRain
	case strings.Contains(code, "fog"):
		return weather.ConditionFog
	default:
		return weather.ConditionUnknown
	}
}
func (n *ForecastNormalizer) openMeteo(payload []byte) (*forecast.ForecastResponse, error) {
	var doc struct {
		Latitude         *float64          `json:"latitude"`
		Longitude        *float64          `json:"longitude"`
		UTCOffsetSeconds *int              `json:"utc_offset_seconds"`
		HourlyUnits      map[string]string `json:"hourly_units"`
		Hourly           *struct {
			Time        []int64    `json:"time"`
			Temperature []*float64 `json:"temperature_2m"`
			Humidity    []*float64 `json:"relative_humidity_2m"`
			Code        []*int     `json:"weather_code"`
		} `json:"hourly"`
		Error bool `json:"error"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	if doc.Error || doc.Latitude == nil || doc.Longitude == nil || doc.Hourly == nil {
		return nil, errors.New("missing forecast data")
	}
	// This adapter requests UTC Unix time and Celsius; a provider-side unit
	// or time-base change must not silently corrupt our canonical output.
	if doc.UTCOffsetSeconds != nil && *doc.UTCOffsetSeconds != 0 {
		return nil, errors.New("non-UTC forecast offset")
	}
	for key, expected := range map[string]string{
		"temperature_2m": "°C", "relative_humidity_2m": "%", "weather_code": "wmo code",
	} {
		if unit, declared := doc.HourlyUnits[key]; declared &&
			!validOpenMeteoForecastUnit(key, unit, expected) {
			return nil, fmt.Errorf("unexpected Open-Meteo unit for %s", key)
		}
	}
	h := doc.Hourly
	if len(h.Time) == 0 || len(h.Time) != len(h.Temperature) || len(h.Time) != len(h.Humidity) || len(h.Time) != len(h.Code) {
		return nil, errors.New("hourly arrays absent or mismatched")
	}
	out := &forecast.ForecastResponse{SchemaVersion: "forecast.v1", DataKind: "model_forecast", Location: loc.Location{Latitude: *doc.Latitude, Longitude: *doc.Longitude}, Points: make([]forecast.Point, 0, MaxForecastPoints)}
	for i, tstamp := range h.Time {
		timestamp := time.Unix(tstamp, 0).UTC()
		if timestamp.Before(time.Now().Add(-time.Hour)) {
			continue
		}
		if h.Temperature[i] == nil {
			return nil, errors.New("missing hourly temperature")
		}
		pt := forecast.Point{ValidAt: timestamp, Temperature: weather.Temperature{Value: *h.Temperature[i], Unit: "celsius"}, Condition: weather.ConditionUnknown}
		if h.Humidity[i] != nil {
			v := *h.Humidity[i]
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
				return nil, errors.New("invalid humidity")
			}
			pt.Humidity = &weather.Humidity{Value: int(math.Round(v)), Unit: "percent"}
		}
		if h.Code[i] != nil {
			pt.SourceCondition = fmt.Sprintf("WMO:%d", *h.Code[i])
			pt.Condition = wmoCondition(*h.Code[i])
		}
		out.Points = append(out.Points, pt)
		if len(out.Points) >= MaxForecastPoints {
			break
		}
	}
	return out, nil
}

// Open-Meteo's weather code unit label may be rendered as "wmo code" or
// "WMO code". This is an enum, not a numerical unit conversion.
func validOpenMeteoForecastUnit(key, unit, expected string) bool {
	if key == "weather_code" {
		return strings.EqualFold(strings.TrimSpace(unit), expected)
	}
	return unit == expected
}

func wmoCondition(code int) weather.Condition {
	switch {
	case code == 0:
		return weather.ConditionClear
	case code == 1 || code == 2:
		return weather.ConditionPartlyCloudy
	case code == 3:
		return weather.ConditionCloudy
	case code == 45 || code == 48:
		return weather.ConditionFog
	case code >= 51 && code <= 67 || code >= 80 && code <= 82:
		return weather.ConditionRain
	case code >= 71 && code <= 77 || code >= 85 && code <= 86:
		return weather.ConditionSnow
	case code == 95 || code == 96 || code == 99:
		return weather.ConditionThunderstorm
	default:
		return weather.ConditionUnknown
	}
}
func ValidateForecast(out *forecast.ForecastResponse) error {
	if out == nil || out.SchemaVersion != "forecast.v1" || out.DataKind != "model_forecast" {
		return errors.New("invalid forecast contract")
	}
	if err := ValidateLocation(&out.Location); err != nil {
		return err
	}
	if len(out.Points) == 0 || len(out.Points) > MaxForecastPoints {
		return errors.New("forecast point count invalid")
	}
	if out.IssuedAt != nil && (out.IssuedAt.IsZero() || out.IssuedAt.After(time.Now().Add(24*time.Hour))) {
		return errors.New("invalid forecast issue time")
	}
	for i, p := range out.Points {
		if p.ValidAt.IsZero() || p.ValidAt.Before(time.Now().Add(-2*time.Hour)) || p.ValidAt.After(time.Now().Add(12*24*time.Hour)) {
			return fmt.Errorf("point %d valid_at out of bounds", i)
		}
		if i > 0 && !p.ValidAt.After(out.Points[i-1].ValidAt) {
			return errors.New("forecast timestamps not increasing")
		}
		if math.IsNaN(p.Temperature.Value) || math.IsInf(p.Temperature.Value, 0) || p.Temperature.Value < -100 || p.Temperature.Value > 70 || p.Temperature.Unit != "celsius" {
			return errors.New("invalid temperature")
		}
		if p.Humidity != nil && (p.Humidity.Unit != "percent" || p.Humidity.Value < 0 || p.Humidity.Value > 100) {
			return errors.New("invalid humidity")
		}
		switch p.Condition {
		case weather.ConditionClear, weather.ConditionPartlyCloudy, weather.ConditionCloudy, weather.ConditionRain, weather.ConditionSnow, weather.ConditionThunderstorm, weather.ConditionFog, weather.ConditionUnknown:
		default:
			return errors.New("invalid condition")
		}
	}
	return nil
}
