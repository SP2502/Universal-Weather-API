package capability

import (
	"net/url"
	"strings"
	"testing"
)

func TestPhase02WeatherRequestValidation(t *testing.T) {
	tests := []struct {
		name, query string
		valid       bool
	}{
		{"location", "location=Raipur%2C+Chhattisgarh%2C+India", true},
		{"coordinates", "lat=0&lon=0", true},
		{"latitude alias", "latitude=51.5074&longitude=-0.1278", true},
		{"place with street number", "location=London%2C+5th+Avenue", true},
		{"invalid pair", "lat=91&lon=180", false},
		{"nan", "lat=NaN&lon=0", false},
		{"infinite", "lat=Inf&lon=0", false},
		{"numeric string invalid", "location=91%2C181", false},
		{"partial", "lat=15", false},
		{"mixed", "location=London&lat=20&lon=10", false},
		{"aliases", "location=London&q=Paris", false},
		{"duplicate", "location=London&location=Paris", false},
		{"empty", "location=", false},
		{"missing", "", false},
		{"unknown key", "location=Delhi&units=imperial", false},
		{"unknown policy", "location=London&policy=magic", false},
		{"invalid provider", "location=London&provider=abc%2Fdef", false},
		{"malformed escape", "location=London%ZZ", false},
		{"control character", "location=London%00", false},
		{"invalid UTF8", "location=%FF", false},
		{"too long", "location=" + strings.Repeat("a", 510), false},
		{"too big raw", "location=" + strings.Repeat("a", 4100), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := &url.URL{RawQuery: tc.query}
			out, err := parseWeatherRequest(u)
			if (err == nil) != tc.valid {
				t.Fatalf("got valid=%v; wanted=%v; output=%+v; err=%v", err == nil, tc.valid, out, err)
			}
		})
	}
}

func TestPhase02GeocodeRequestValidation(t *testing.T) {
	tests := []struct {
		name, query string
		valid       bool
	}{
		{"forward", "q=Raipur&limit=10&country=in", true},
		{"reverse", "lat=0&lon=0", true},
		{"limit min", "q=London&limit=1", true},
		{"limit max", "q=London&limit=50", true},
		{"bad limit", "q=London&limit=0", false},
		{"nonnumeric limit", "q=London&limit=abc", false},
		{"limit reverse", "lat=10&lon=20&limit=5", false},
		{"country reverse", "lat=10&lon=20&country=GB", false},
		{"bad country", "q=London&country=GBR", false},
		{"mixed", "q=London&lat=1&lon=2", false},
		{"partial", "lat=1", false},
		{"duplicate", "q=London&q=Paris", false},
		{"invalid encoding", "q=abc%XX", false},
		{"control", "q=abc%0Adef", false},
		{"NaN", "lat=NaN&lon=0", false},
		{"out of range", "lat=10&lon=181", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseGeocodeRequest(&url.URL{RawQuery: tc.query})
			if (err == nil) != tc.valid {
				t.Fatalf("got valid=%v want=%v, output=%+v err=%v", err == nil, tc.valid, out, err)
			}
		})
	}
}
