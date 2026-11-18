package main

import (
	"reflect"
	"testing"
)

func TestRequiredCapabilitiesValidation(t *testing.T) {
	tests := []struct {
		input string
		want  []string
		bad   bool
	}{
		{"", []string{"weather"}, false},
		{"weather", []string{"weather"}, false},
		{"forecast", []string{"forecast"}, false},
		{"forecast,weather", []string{"weather", "forecast"}, false},
		{" weather , forecast ", []string{"weather", "forecast"}, false},
		{"weather,weather", nil, true},
		{"forecast,", nil, true},
		{"", []string{"weather"}, false},
		{"geocode", nil, true},
	}
	for _, tc := range tests {
		actual, err := requiredCapabilities(tc.input)
		if (err != nil) != tc.bad {
			t.Errorf("%q err=%v", tc.input, err)
			continue
		}
		if !tc.bad && !reflect.DeepEqual(actual, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.input, actual, tc.want)
		}
	}
}
func TestCheckConfigRejectsUnsupportedCapabilities(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_REQUIRED_CAPABILITIES", "weather,geocode")
	if err := run([]string{"--check-config"}); err == nil {
		t.Fatal("unsupported capability passed config check")
	}
	t.Setenv("APP_REQUIRED_CAPABILITIES", "forecast")
	if err := run([]string{"--check-config"}); err != nil {
		t.Fatal(err)
	}
}
