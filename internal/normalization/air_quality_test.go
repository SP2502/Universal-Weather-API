package normalization

import (
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	"strings"
	"testing"
	"time"
)

func TestPhase07C_AirQualityPresenceAndRange(t *testing.T) {
	timestamp := time.Now().UTC().Add(time.Hour).Unix()
	template := `{"latitude":21.25,"longitude":81.63,"hourly":{"time":[%d],"pm2_5":[%s],"pm10":[20]}}`
	for _, tc := range []struct {
		value string
		bad   bool
	}{{"0", false}, {"3.4", false}, {"null", true}, {"-1", true}, {"9999999", true}} {
		raw := &provider.RawAirQualityResponse{ProviderID: "open_meteo_air_quality", RawJSON: []byte(fmt.Sprintf(template, timestamp, tc.value))}
		got, err := NormalizeAirQuality(raw)
		if (err != nil) != tc.bad {
			t.Fatalf("value=%s err=%v", tc.value, err)
		}
		if !tc.bad && (got.Points[0].PM25 == nil || got.Points[0].PM25.Unit != "µg/m³" || got.Points[0].Ozone != nil) {
			t.Fatalf("missing fields fabricated: %+v", got)
		}
	}
	bad := &provider.RawAirQualityResponse{ProviderID: "open_meteo_air_quality", RawJSON: []byte(fmt.Sprintf(template, timestamp, `2,3`))}
	if _, err := NormalizeAirQuality(bad); err == nil {
		t.Fatal("array mismatch accepted")
	}
	if _, err := NormalizeAirQuality(&provider.RawAirQualityResponse{ProviderID: "wrong", RawJSON: []byte(strings.ReplaceAll(fmt.Sprintf(template, timestamp, "2"), "pm10", "pm10"))}); err == nil {
		t.Fatal("identity mismatch accepted")
	}
}
