package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

func TestPhase02GeocodingRejectsInvalidCoordinates(t *testing.T) {
	for _, providerID := range []string{"nominatim", "locationiq"} {
		for _, test := range []struct{ name, body string }{
			{"absent", `{}`},
			{"null", `{"lat":null,"lon":null}`},
			{"non numeric", `{"lat":"London","lon":"0","display_name":"Somewhere"}`},
			{"nan", `{"lat":"NaN","lon":"0","display_name":"Somewhere"}`},
			{"infinite", `{"lat":"Inf","lon":"0","display_name":"Somewhere"}`},
			{"out of bounds", `{"lat":"90.1","lon":"0","display_name":"Somewhere"}`},
			{"missing address", `{"lat":"12","lon":"21"}`},
		} {
			t.Run(providerID+"/"+test.name, func(t *testing.T) {
				_, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: providerID, Mode: "reverse", RawPayload: []byte(test.body)})
				if !errors.Is(err, provider.ErrMalformedResponse) {
					t.Fatalf("expected malformed response, got %v", err)
				}
			})
		}
		t.Run(providerID+"/mixed forward", func(t *testing.T) {
			body := `[{"lat":"1","lon":"2","display_name":"Good"},{"lat":"bad","lon":"3","display_name":"Bad"}]`
			_, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: providerID, Mode: "forward", RawPayload: []byte(body)})
			if !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("must not silently discard bad item: %v", err)
			}
		})
		t.Run(providerID+"/empty forward", func(t *testing.T) {
			res, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: providerID, Mode: "forward", RawPayload: []byte(`[]`)})
			if err != nil {
				t.Fatal(err)
			}
			bytes, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(bytes), `"results":[]`) {
				t.Fatalf("expected [] not null, got %s", bytes)
			}
		})
		t.Run(providerID+"/null forward", func(t *testing.T) {
			_, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: providerID, Mode: "forward", RawPayload: []byte(`null`)})
			if !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("expected malformed response, got %v", err)
			}
		})
		t.Run(providerID+"/zero coordinates valid", func(t *testing.T) {
			res, err := NewGeocodingNormalizer().Normalize(&provider.RawGeocodeResponse{ProviderID: providerID, Mode: "reverse", RawPayload: []byte(`{"lat":"0","lon":"0","display_name":"Gulf of Guinea"}`)})
			if err != nil || len(res.Results) != 1 || res.Results[0].Latitude != 0 {
				t.Fatalf("real zero must remain valid; res=%v err=%v", res, err)
			}
		})
	}
}

func TestPhase02WeatherMissingFieldsAreNotInvented(t *testing.T) {
	tests := []struct{ name, providerID, payload string }{
		{"OW no temperature", "openweather", `{"coord":{"lat":1,"lon":2},"main":{},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`},
		{"OW no coordinates", "openweather", `{"main":{"temp":293.1},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`},
		{"OW no time", "openweather", `{"coord":{"lat":1,"lon":2},"main":{"temp":293.1}}`},
		{"OW null temp", "openweather", `{"coord":{"lat":1,"lon":2},"main":{"temp":null},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`},
		{"WeatherAPI empty", "weatherapi", `{}`},
		{"WeatherAPI null temp", "weatherapi", `{"location":{"lat":1,"lon":2},"current":{"temp_c":null,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`},
		{"WeatherAPI no coords", "weatherapi", `{"location":{},"current":{"temp_c":0,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`},
		{"WeatherAPI no timestamp", "weatherapi", `{"location":{"lat":1,"lon":2},"current":{"temp_c":0}}`},
		{"WeatherAPI future timestamp", "weatherapi", `{"location":{"lat":1,"lon":2},"current":{"temp_c":0,"last_updated_epoch":9999999999}}`},
		{"WeatherAPI wrong humidity", "weatherapi", `{"location":{"lat":1,"lon":2},"current":{"temp_c":0,"humidity":101,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := &provider.RawWeatherResponse{ProviderID: tc.providerID, RawJSON: []byte(tc.payload), RetrievedAt: time.Now().UTC()}
			_, err := NewWeatherNormalizer().Normalize(raw)
			if !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("expected malformed upstream: %v", err)
			}
		})
	}
}

func TestPhase02HazardousConditionsPreserveSource(t *testing.T) {
	for _, code := range []int{711, 731, 762, 771, 781} {
		t.Run("openweather/"+strconv.Itoa(code), func(t *testing.T) {
			body := []byte(`{"coord":{"lat":1,"lon":2},"main":{"temp":300},"dt":` + fmt.Sprint(time.Now().Unix()) + `,"weather":[{"id":` + strconv.Itoa(code) + `,"main":"Atmosphere","description":"hazardous"}]}`)
			res, err := NewWeatherNormalizer().Normalize(&provider.RawWeatherResponse{ProviderID: "openweather", RawJSON: body})
			if err != nil {
				t.Fatal(err)
			}
			if res.Condition != "unknown" || res.SourceCondition == nil || res.SourceCondition.Code != code || res.SourceCondition.Description != "hazardous" {
				t.Fatalf("bad condition: %+v", res)
			}
		})
	}
}
