package normalization

import (
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	loc "github.com/universal-api/platform/schemas/location/v1"
	w "github.com/universal-api/platform/schemas/weather/v1"
	"testing"
	"time"
)

func TestCurrentObservationAgeBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		at    time.Time
		valid bool
	}{{"zero", time.Time{}, false}, {"boundary", now.Add(-MaxObservationAge), true}, {"over-boundary", now.Add(-MaxObservationAge - time.Nanosecond), false}, {"zero-value-weather", now, true}, {"future-boundary", now.Add(15 * time.Minute), true}, {"future-over", now.Add(15*time.Minute + time.Nanosecond), false}} {
		t.Run(tc.name, func(t *testing.T) {
			v := &w.WeatherResponse{SchemaVersion: "weather.v1", Location: loc.Location{}, Temperature: w.Temperature{Value: 0, Unit: "celsius"}, Condition: w.ConditionClear, ObservedAt: tc.at}
			err := ValidateWeatherAt(v, now)
			if (err == nil) != tc.valid {
				t.Fatalf("at=%s err=%v", tc.at, err)
			}
		})
	}
}
func TestPortableTimezones(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		valid bool
	}{{"", true}, {"UTC", true}, {"Asia/Kolkata", true}, {"Europe/London", true}, {"Invalid/Timezone", false}, {"Local", false}, {"../../etc/passwd", false}} {
		err := ValidateLocation(&loc.Location{Timezone: tc.zone})
		if (err == nil) != tc.valid {
			t.Errorf("timezone%q err=%v", tc.zone, err)
		}
	}
}
func TestPointToleranceDatelineAndPoles(t *testing.T) {
	for _, tc := range []struct {
		lat, lon, alat, alon float64
		valid                bool
	}{{0, 179.999, 0, -179.999, true}, {90, 0, 90, 179, true}, {21.2, 81.6, 21.21, 81.61, true}, {21.2, 81.6, 51.5, -.12, false}} {
		err := ValidateRequestedLocation("met_norway", &tc.lat, &tc.lon, &loc.Location{Latitude: tc.alat, Longitude: tc.alon})
		if (err == nil) != tc.valid {
			t.Fatalf("%+v err=%v", tc, err)
		}
	}
}
func TestMETSleetPreservesSourceWithoutFalseSnow(t *testing.T) {
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, code := range []string{"sleet", "sleetshowers_day", "lightsleet_night"} {
		b := fmt.Sprintf(`{"geometry":{"coordinates":[81.6,21.2]},"properties":{"timeseries":[{"time":%q,"data":{"instant":{"details":{"air_temperature":0}},"next_1_hours":{"summary":{"symbol_code":%q}}}}]}}`, at, code)
		v, e := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "met_norway", RawJSON: json.RawMessage(b)})
		if e != nil || v.Points[0].Condition != w.ConditionUnknown || v.Points[0].SourceCondition != code {
			t.Fatalf("%s result=%+v err=%v", code, v, e)
		}
	}
}
