package normalization

import (
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/schemas/weather/v1"
	"strings"
	"testing"
	"time"
)

func TestNWSNormalizesOnlyFutureAndExplicitUnits(t *testing.T) {
	at := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	b := []byte(fmt.Sprintf(`{"properties":{"periods":[{"startTime":%q,"temperature":68,"temperatureUnit":"F","relativeHumidity":{"value":50},"shortForecast":"Sunny"}]}}`, at))
	lat, lon := 39.7, -97.0
	p, err := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "us_nws", RawJSON: b, Latitude: &lat, Longitude: &lon})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Points) != 1 || p.Points[0].Temperature.Value != 20 || p.Points[0].Condition != v1.ConditionClear {
		t.Fatalf("incorrect NWS output: %+v", p.Points)
	}
}
func TestNWSRejectsAbsentTemperatureAndUnknownUnits(t *testing.T) {
	at := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	lat, lon := 39.7, -97.0
	for _, input := range []string{`{"temperatureUnit":"F"}`, `{"temperature":50,"temperatureUnit":"K"}`, `{"temperature":null,"temperatureUnit":"F"}`} {
		b := []byte(fmt.Sprintf(`{"properties":{"periods":[{"startTime":%q,%s}]}}`, at, strings.TrimPrefix(strings.TrimSuffix(input, "}"), "{")))
		if _, err := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "us_nws", RawJSON: b, Latitude: &lat, Longitude: &lon}); err == nil {
			t.Errorf("accepted invalid NWS payload %s", input)
		}
	}
}
func TestBrightSkyFiltersCurrentAndDistantForecastSources(t *testing.T) {
	at := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	lat, lon := 52.5, 13.4
	b := []byte(fmt.Sprintf(`{"sources":[{"id":1,"observation_type":"current","distance":1000},{"id":2,"observation_type":"forecast","distance":1000},{"id":3,"observation_type":"forecast","distance":26000}],"weather":[{"source_id":1,"timestamp":%q,"temperature":50},{"source_id":2,"timestamp":%q,"temperature":0,"icon":"clear-day"},{"source_id":3,"timestamp":%q,"temperature":25}]}`, at, at, at))
	resp, err := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "bright_sky", RawJSON: b, Latitude: &lat, Longitude: &lon})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Points) != 1 || resp.Points[0].Temperature.Value != 0 || resp.Points[0].SourceCondition != "clear-day" {
		t.Fatalf("incorrect Bright Sky forecast: %+v", resp.Points)
	}
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
}
func TestBrightSkyRejectsOnlyCurrentSources(t *testing.T) {
	at := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	lat, lon := 52.5, 13.4
	b := []byte(fmt.Sprintf(`{"sources":[{"id":1,"observation_type":"current","distance":1000}],"weather":[{"source_id":1,"timestamp":%q,"temperature":25}]}`, at))
	if _, err := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "bright_sky", RawJSON: b, Latitude: &lat, Longitude: &lon}); err == nil {
		t.Fatal("current observation accepted as forecast")
	}
}
