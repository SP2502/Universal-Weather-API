package normalization

import (
	"errors"
	"fmt"
	daily "github.com/universal-api/platform/schemas/forecast/daily/v1"
	forecast "github.com/universal-api/platform/schemas/forecast/v1"
	"time"
)

// AggregateForecastDays returns min/max of the actual, available forecast
// samples per UTC date. It never labels them as complete calendar-day extrema.
func AggregateForecastDays(src *forecast.ForecastResponse, days int) (*daily.Response, error) {
	if err := ValidateForecast(src); err != nil {
		return nil, fmt.Errorf("invalid source forecast: %w", err)
	}
	if days < 1 || days > 7 {
		return nil, errors.New("days must be 1 through 7")
	}
	out := &daily.Response{SchemaVersion: "forecast.daily.v1", DataKind: "derived_model_forecast", AggregationBasis: "available_sampled_points_only", Timezone: "UTC", Location: src.Location, IssuedAt: src.IssuedAt, Days: make([]daily.Day, 0, days)}
	today := time.Now().UTC()
	for _, pt := range src.Points {
		if pt.ValidAt.Before(today) {
			continue
		}
		key := pt.ValidAt.UTC().Format("2006-01-02")
		n := len(out.Days)
		if n == 0 || out.Days[n-1].DateUTC != key {
			if n >= days {
				break
			}
			out.Days = append(out.Days, daily.Day{DateUTC: key, MinimumSampledCelsius: pt.Temperature.Value, MaximumSampledCelsius: pt.Temperature.Value, SampleCount: 1, FirstValidAt: pt.ValidAt, LastValidAt: pt.ValidAt})
		} else {
			row := &out.Days[n-1]
			row.SampleCount++
			if pt.Temperature.Value < row.MinimumSampledCelsius {
				row.MinimumSampledCelsius = pt.Temperature.Value
			}
			if pt.Temperature.Value > row.MaximumSampledCelsius {
				row.MaximumSampledCelsius = pt.Temperature.Value
			}
			row.LastValidAt = pt.ValidAt
		}
	}
	if len(out.Days) == 0 {
		return nil, errors.New("no future forecast samples available")
	}
	return out, nil
}
