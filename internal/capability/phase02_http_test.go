package capability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Validation must precede registry selection and any upstream network work.
func TestPhase02MalformedRequestsReturn400BeforeProviderSelection(t *testing.T) {
	weather := NewWeatherHandler(nil, nil, nil, nil, nil, nil)
	geocode := NewGeocodingHandler(nil, nil, nil, nil)
	for _, tc := range []struct {
		name, path string
		handle     http.HandlerFunc
	}{
		{"weather NaN", "/v1/weather?lat=NaN&lon=5", weather.GetWeather},
		{"weather duplicate", "/v1/weather?location=London&location=Paris", weather.GetWeather},
		{"weather invalid percent", "/v1/weather?location=Bad%XX", weather.GetWeather},
		{"geocode NaN", "/v1/geocode?lat=NaN&lon=5", geocode.Geocode},
		{"geocode conflicting", "/v1/geocode?q=London&lat=51&lon=0", geocode.Geocode},
		{"geocode invalid limit", "/v1/geocode?q=London&limit=999", geocode.Geocode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tc.handle(rr, httptest.NewRequest("GET", tc.path, nil))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("got status %d body %s", rr.Code, rr.Body.String())
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil || envelope.Error.Code == "" {
				t.Fatalf("expected JSON error envelope: %v, %s", err, rr.Body.String())
			}
		})
	}
}
