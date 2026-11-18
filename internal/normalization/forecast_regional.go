package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	forecast "github.com/universal-api/platform/schemas/forecast/v1"
	location "github.com/universal-api/platform/schemas/location/v1"
	weather "github.com/universal-api/platform/schemas/weather/v1"
	"math"
	"strings"
	"time"
)

// NWS temperatures are sometimes represented as primitive numbers and may
// switch to a QuantitativeValue object under an endpoint feature flag.
func nwsTemperature(raw json.RawMessage) (float64, error) {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}
	var q struct {
		Value    *float64 `json:"value"`
		UnitCode string   `json:"unitCode"`
	}
	if json.Unmarshal(raw, &q) != nil || q.Value == nil {
		return 0, errors.New("missing NWS temperature")
	}
	switch q.UnitCode {
	case "wmoUnit:degC":
		return *q.Value, nil
	case "wmoUnit:degF":
		return (*q.Value - 32) * 5 / 9, nil
	default:
		return 0, errors.New("unrecognized NWS temperature unit")
	}
}
func (n *ForecastNormalizer) usNWS(raw *provider.RawForecastResponse) (*forecast.ForecastResponse, error) {
	if raw.Latitude == nil || raw.Longitude == nil {
		return nil, errors.New("nWS requested coordinates missing")
	}
	var doc struct {
		Properties *struct {
			Updated string `json:"updated"`
			Periods []struct {
				StartTime        string          `json:"startTime"`
				Temperature      json.RawMessage `json:"temperature"`
				TemperatureUnit  string          `json:"temperatureUnit"`
				RelativeHumidity *struct {
					Value *float64 `json:"value"`
				} `json:"relativeHumidity"`
				ShortForecast string `json:"shortForecast"`
			} `json:"periods"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw.RawJSON, &doc); err != nil || doc.Properties == nil || len(doc.Properties.Periods) == 0 {
		return nil, errors.New("missing NWS forecast periods")
	}
	out := &forecast.ForecastResponse{SchemaVersion: "forecast.v1", DataKind: "model_forecast", Location: location.Location{Latitude: *raw.Latitude, Longitude: *raw.Longitude}, Points: make([]forecast.Point, 0, MaxForecastPoints)}
	if doc.Properties.Updated != "" {
		t, e := time.Parse(time.RFC3339, doc.Properties.Updated)
		if e != nil {
			return nil, errors.New("invalid NWS issue time")
		}
		out.IssuedAt = &t
	}
	for _, p := range doc.Properties.Periods {
		t, e := time.Parse(time.RFC3339, p.StartTime)
		if e != nil {
			return nil, errors.New("invalid NWS forecast time")
		}
		if t.Before(time.Now().Add(-time.Hour)) {
			continue
		}
		if len(p.Temperature) == 0 || string(p.Temperature) == "null" {
			return nil, errors.New("missing NWS temperature")
		}
		value, e := nwsTemperature(p.Temperature)
		if e != nil {
			return nil, e
		}
		// Primitive numeric temperatureUnit is Fahrenheit or Celsius, not implicit.
		if p.Temperature[0] != '{' {
			switch p.TemperatureUnit {
			case "F":
				value = (value - 32) * 5 / 9
			case "C":
			default:
				return nil, errors.New("unknown NWS temperature unit")
			}
		}
		point := forecast.Point{ValidAt: t.UTC(), Temperature: weather.Temperature{Value: math.Round(value*100) / 100, Unit: "celsius"}, SourceCondition: p.ShortForecast, Condition: nwsCondition(p.ShortForecast)}
		if p.RelativeHumidity != nil && p.RelativeHumidity.Value != nil {
			v := *p.RelativeHumidity.Value
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
				return nil, errors.New("invalid NWS humidity")
			}
			point.Humidity = &weather.Humidity{Value: int(math.Round(v)), Unit: "percent"}
		}
		out.Points = append(out.Points, point)
		if len(out.Points) >= MaxForecastPoints {
			break
		}
	}
	return out, nil
}
func nwsCondition(value string) weather.Condition {
	v := strings.ToLower(value)
	switch {
	case strings.Contains(v, "thunder"):
		return weather.ConditionThunderstorm
	case strings.Contains(v, "snow"), strings.Contains(v, "sleet"):
		return weather.ConditionSnow
	case strings.Contains(v, "rain"), strings.Contains(v, "shower"), strings.Contains(v, "drizzle"):
		return weather.ConditionRain
	case strings.Contains(v, "fog"), strings.Contains(v, "mist"):
		return weather.ConditionFog
	case strings.Contains(v, "partly"), strings.Contains(v, "mostly sunny"), strings.Contains(v, "mostly clear"):
		return weather.ConditionPartlyCloudy
	case strings.Contains(v, "cloudy"), strings.Contains(v, "overcast"):
		return weather.ConditionCloudy
	case strings.Contains(v, "sunny"), strings.Contains(v, "clear"):
		return weather.ConditionClear
	default:
		return weather.ConditionUnknown
	}
}
func (n *ForecastNormalizer) brightSky(raw *provider.RawForecastResponse) (*forecast.ForecastResponse, error) {
	if raw.Latitude == nil || raw.Longitude == nil {
		return nil, errors.New("bright Sky requested coordinates missing")
	}
	var doc struct {
		Sources []struct {
			ID              int      `json:"id"`
			ObservationType string   `json:"observation_type"`
			Distance        *float64 `json:"distance"`
		} `json:"sources"`
		Weather []struct {
			Timestamp        string   `json:"timestamp"`
			SourceID         int      `json:"source_id"`
			Temperature      *float64 `json:"temperature"`
			RelativeHumidity *float64 `json:"relative_humidity"`
			Condition        *string  `json:"condition"`
			Icon             *string  `json:"icon"`
		} `json:"weather"`
	}
	if err := json.Unmarshal(raw.RawJSON, &doc); err != nil || len(doc.Sources) == 0 {
		return nil, errors.New("invalid Bright Sky response")
	}
	// Never promote historical/current observation records to forecasts.
	maxDistance := raw.MaxStationDistanceMeters
	if maxDistance <= 0 || maxDistance > 50000 {
		maxDistance = 25000
	}
	candidates := map[int]bool{}
	for _, s := range doc.Sources {
		if s.ObservationType == "forecast" && s.Distance != nil && *s.Distance >= 0 && *s.Distance <= float64(maxDistance) {
			candidates[s.ID] = true
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("no eligible Bright Sky forecast source within 25 km")
	}
	out := &forecast.ForecastResponse{SchemaVersion: "forecast.v1", DataKind: "model_forecast", Location: location.Location{Latitude: *raw.Latitude, Longitude: *raw.Longitude}, Points: make([]forecast.Point, 0, MaxForecastPoints)}
	for _, v := range doc.Weather {
		if !candidates[v.SourceID] {
			continue
		}
		at, e := time.Parse(time.RFC3339, v.Timestamp)
		if e != nil {
			return nil, errors.New("invalid Bright Sky forecast time")
		}
		if at.Before(time.Now().Add(-time.Hour)) {
			continue
		}
		if v.Temperature == nil {
			return nil, errors.New("missing Bright Sky forecast temperature")
		}
		pt := forecast.Point{ValidAt: at.UTC(), Temperature: weather.Temperature{Value: *v.Temperature, Unit: "celsius"}, Condition: weather.ConditionUnknown}
		if v.RelativeHumidity != nil {
			v := *v.RelativeHumidity
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
				return nil, errors.New("invalid Bright Sky humidity")
			}
			pt.Humidity = &weather.Humidity{Value: int(math.Round(v)), Unit: "percent"}
		}
		if v.Icon != nil {
			pt.SourceCondition = *v.Icon
			pt.Condition = skyCondition(*v.Icon)
		} else if v.Condition != nil {
			pt.SourceCondition = *v.Condition
			pt.Condition = skyCondition(*v.Condition)
		}
		out.Points = append(out.Points, pt)
		if len(out.Points) >= MaxForecastPoints {
			break
		}
	}
	if len(out.Points) == 0 {
		return nil, fmt.Errorf("no usable Bright Sky forecast points")
	}
	return out, nil
}
func skyCondition(c string) weather.Condition {
	c = strings.ToLower(strings.TrimSpace(c))
	switch {
	case strings.Contains(c, "thunder"):
		return weather.ConditionThunderstorm
	case strings.Contains(c, "snow"), strings.Contains(c, "sleet"):
		return weather.ConditionSnow
	case strings.Contains(c, "rain"), strings.Contains(c, "showers"):
		return weather.ConditionRain
	case strings.Contains(c, "fog"), strings.Contains(c, "mist"):
		return weather.ConditionFog
	case strings.Contains(c, "partly-cloudy"), strings.Contains(c, "partlycloudy"):
		return weather.ConditionPartlyCloudy
	case strings.Contains(c, "cloudy"):
		return weather.ConditionCloudy
	case strings.Contains(c, "clear"):
		return weather.ConditionClear
	default:
		return weather.ConditionUnknown
	}
}
