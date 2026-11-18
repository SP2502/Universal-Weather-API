package contract

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

func TestPhase02AdapterToNormalizerRetainsMissingWeatherFields(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"OW missing temp", `{"coord":{"lat":0,"lon":0},"main":{},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`, false},
		{"OW genuine zero Celsius", `{"coord":{"lat":0,"lon":0},"main":{"temp":273.15},"dt":` + fmt.Sprint(time.Now().Unix()) + `}`, true},
		{"WeatherAPI missing temp", `{"location":{"lat":0,"lon":0},"current":{"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`, false},
		{"WeatherAPI true zero", `{"location":{"lat":0,"lon":0},"current":{"temp_c":0,"last_updated_epoch":` + fmt.Sprint(time.Now().Unix()) + `}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			var p provider.WeatherProvider
			if strings.HasPrefix(tc.name, "OW") {
				p = openweather.NewAdapter("test-key", server.URL).WithHTTPClient(server.Client())
			} else {
				p = weatherapi.NewAdapter("test-key", server.URL).WithHTTPClient(server.Client())
			}
			raw, err := p.FetchWeather(context.Background(), "0,0")
			if err != nil {
				t.Fatalf("fetch failed: %v", err)
			}
			if len(raw.RawJSON) == 0 {
				t.Fatal("original JSON required to distinguish absent zero values")
			}
			normalized, err := normalization.NewWeatherNormalizer().Normalize(raw)
			if tc.valid {
				if err != nil || normalized.Temperature.Value != 0 || normalized.Location.Latitude != 0 {
					t.Fatalf("valid zero weather incorrectly rejected: %v %+v", err, normalized)
				}
			} else if !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("missing data must be malformed: %v", err)
			}
		})
	}
}

func TestPhase02GeocodeAdaptersRejectFabricatedZero(t *testing.T) {
	for _, name := range []string{"nominatim", "locationiq"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"display_name":"Somewhere","lat":"not numeric","lon":"0"}`))
			}))
			defer server.Close()
			var p provider.GeocodingProvider
			if name == "nominatim" {
				p = nominatim.NewAdapter(server.URL).WithHTTPClient(server.Client())
			} else {
				p = locationiq.NewAdapter("key", server.URL).WithHTTPClient(server.Client())
			}
			raw, err := p.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 1, Longitude: 2})
			if err != nil {
				t.Fatal(err)
			}
			_, err = normalization.NewGeocodingNormalizer().Normalize(raw)
			if !errors.Is(err, provider.ErrMalformedResponse) {
				t.Fatalf("bad coordinates must not decode as (0,0): %v", err)
			}
		})
	}
}
