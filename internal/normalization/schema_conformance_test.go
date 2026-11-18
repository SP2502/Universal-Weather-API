package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

func TestSchemaCatalogAllIntegratedAdapters(t *testing.T) {
	catalog := ProviderSchemaCatalog()
	if len(catalog) != 15 {
		t.Fatalf("expected 15 known integrations, got %d", len(catalog))
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, c := range catalog {
		if seen[c.ProviderID] || c.ProviderID == "" || c.Product == "" || c.SchemaVersion == "" || !strings.HasPrefix(c.ReferenceURL, "https://") {
			t.Fatalf("invalid/duplicate adapter contract %+v", c)
		}
		seen[c.ProviderID] = true
		counts[c.Product]++
	}
	for product, want := range map[string]int{"weather": 2, "forecast": 4, "geocoding": 4, "air_quality": 1, "history": 1, "astronomy": 1, "marine": 1, "alerts": 1} {
		if counts[product] != want {
			t.Fatalf("%s count=%d want %d", product, counts[product], want)
		}
	}
	// Return-by-copy: no caller can change the shared static inventory.
	catalog[0].ProviderID = "changed"
	if ProviderSchemaCatalog()[0].ProviderID == "changed" {
		t.Fatal("shared catalog modified by caller")
	}
}

func TestOpenMeteoForecastRejectsIncompatibleUnitsAndOffset(t *testing.T) {
	next := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Hour).Unix()
	base := func(units string, offset int) []byte {
		return []byte(fmt.Sprintf(`{"latitude":21.25,"longitude":81.63,"utc_offset_seconds":%d,"hourly_units":%s,"hourly":{"time":[%d],"temperature_2m":[25],"relative_humidity_2m":[0],"weather_code":[0]}}`, offset, units, next))
	}
	for _, tc := range []struct {
		name string
		data []byte
		bad  bool
	}{
		{"real UTC/Celsius", base(`{"temperature_2m":"°C","relative_humidity_2m":"%","weather_code":"wmo code"}`, 0), false},
		{"fahrenheit drift", base(`{"temperature_2m":"°F","relative_humidity_2m":"%","weather_code":"wmo code"}`, 0), true},
		{"humidity fraction drift", base(`{"temperature_2m":"°C","relative_humidity_2m":"fraction"}`, 0), true},
		{"unknown code unit", base(`{"temperature_2m":"°C","weather_code":"unknown"}`, 0), true},
		{"non UTC offset", base(`{"temperature_2m":"°C"}`, 3600), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewForecastNormalizer().Normalize(&provider.RawForecastResponse{ProviderID: "open_meteo_forecast", RawJSON: tc.data})
			if tc.bad && !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("expected malformed response, got r=%+v err=%v", r, err)
			}
			if !tc.bad && (err != nil || r.Points[0].Humidity == nil || r.Points[0].Humidity.Value != 0 || r.Points[0].Temperature.Value != 25) {
				t.Fatalf("valid upstream zeros must survive: r=%+v err=%v", r, err)
			}
		})
	}
}

func TestWeatherAPIConditionMappingFromPublishedCodeTable(t *testing.T) {
	cases := []struct {
		code int
		want weatherv1.Condition
	}{
		{1000, weatherv1.ConditionClear}, {1003, weatherv1.ConditionPartlyCloudy}, {1009, weatherv1.ConditionCloudy},
		{1030, weatherv1.ConditionFog}, {1135, weatherv1.ConditionFog},
		{1063, weatherv1.ConditionRain}, {1150, weatherv1.ConditionRain}, {1198, weatherv1.ConditionRain},
		{1201, weatherv1.ConditionRain}, {1246, weatherv1.ConditionRain},
		{1066, weatherv1.ConditionSnow}, {1117, weatherv1.ConditionSnow}, {1258, weatherv1.ConditionSnow},
		{1273, weatherv1.ConditionThunderstorm}, {1282, weatherv1.ConditionThunderstorm},
		{1069, weatherv1.ConditionUnknown}, {1204, weatherv1.ConditionUnknown}, {1264, weatherv1.ConditionUnknown},
		{9999, weatherv1.ConditionUnknown},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			got := mapWeatherAPICondition(tc.code, "Sunny and clear sky; do not infer from text")
			if got != tc.want {
				t.Fatalf("code %d got %q want %q", tc.code, got, tc.want)
			}
		})
	}
	// Upstream code and human-readable text remain visible even when no
	// canonical category faithfully describes a mixed sleet observation.
	at := time.Now().UTC().Add(-time.Minute).Unix()
	body := []byte(fmt.Sprintf(`{"location":{"lat":21.25,"lon":81.63},"current":{"temp_c":0,"last_updated_epoch":%d,"condition":{"code":1204,"text":"Light sleet"}}}`, at))
	w, err := NewWeatherNormalizer().Normalize(&provider.RawWeatherResponse{ProviderID: "weatherapi", RawJSON: body})
	if err != nil || w.Condition != weatherv1.ConditionUnknown || w.SourceCondition == nil || w.SourceCondition.Code != 1204 {
		t.Fatalf("source condition must remain traceable: w=%+v err=%v", w, err)
	}
}

func TestGeocodingImportanceIsNotConfidence(t *testing.T) {
	b := []byte(`[{"lat":"0","lon":"0","display_name":"Gulf of Guinea","importance":0.99}]`)
	res, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: "nominatim", Mode: "forward", RawPayload: b})
	if err != nil || len(res.Results) != 1 || res.Results[0].Confidence != 0 {
		t.Fatalf("search ranking cannot imply location accuracy: res=%+v err=%v", res, err)
	}
	out, err := json.Marshal(res.Results[0])
	if err != nil || strings.Contains(string(out), `"confidence"`) {
		t.Fatalf("unknown confidence must be absent: %s err=%v", out, err)
	}
}

func TestBrightSkyConditionNormalizationIsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct {
		code string
		want weatherv1.Condition
	}{
		{"PARTLY-CLOUDY-DAY", weatherv1.ConditionPartlyCloudy},
		{"PartlyCloudy-Night", weatherv1.ConditionPartlyCloudy},
		{"Thunderstorms", weatherv1.ConditionThunderstorm},
		{"Cloudy", weatherv1.ConditionCloudy},
	} {
		if got := skyCondition(tc.code); got != tc.want {
			t.Errorf("%q maps to %q rather than %q", tc.code, got, tc.want)
		}
	}
}
