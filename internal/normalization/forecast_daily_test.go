package normalization

import (
	forecast "github.com/universal-api/platform/schemas/forecast/v1"
	location "github.com/universal-api/platform/schemas/location/v1"
	weather "github.com/universal-api/platform/schemas/weather/v1"
	"testing"
	"time"
)

func TestPhase07C_DailyFromSamples(t *testing.T) {
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	f := &forecast.ForecastResponse{SchemaVersion: "forecast.v1", DataKind: "model_forecast", Location: location.Location{Latitude: 21.25, Longitude: 81.63}, Points: []forecast.Point{
		{ValidAt: day.Add(2 * time.Hour), Temperature: weather.Temperature{Value: 11, Unit: "celsius"}, Condition: weather.ConditionClear},
		{ValidAt: day.Add(5 * time.Hour), Temperature: weather.Temperature{Value: 17, Unit: "celsius"}, Condition: weather.ConditionClear},
		{ValidAt: day.Add(26 * time.Hour), Temperature: weather.Temperature{Value: 9, Unit: "celsius"}, Condition: weather.ConditionClear},
	}}
	d, err := AggregateForecastDays(f, 2)
	if err != nil {
		t.Fatal(err)
	}
	if d.SchemaVersion != "forecast.daily.v1" || len(d.Days) != 2 || d.Days[0].MinimumSampledCelsius != 11 || d.Days[0].MaximumSampledCelsius != 17 || d.Days[0].SampleCount != 2 || d.Timezone != "UTC" {
		t.Fatalf("wrong summary: %+v", d)
	}
	if _, err := AggregateForecastDays(f, 8); err == nil {
		t.Fatal("accepted 8 days")
	}
}
