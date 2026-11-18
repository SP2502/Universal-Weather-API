package main

import "testing"

func TestPhase06ActivationValidation(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"defaults", nil, true},
		{"geoapify missing quota", map[string]string{"APP_ENABLE_GEOAPIFY": "true"}, false},
		{"geoapify valid quota", map[string]string{"APP_ENABLE_GEOAPIFY": "true", "APP_PROVIDER_QUOTAS_JSON": `{"geoapify":{"per_minute":180,"per_day":2800}}`}, true},
		{"met missing user agent", map[string]string{"APP_ENABLE_MET_NORWAY": "true"}, false},
		{"met with operator", map[string]string{"APP_ENABLE_MET_NORWAY": "true", "APP_MET_NORWAY_USER_AGENT": "UWA/1.0 https://example.org/contact"}, true},
		{"openmeteo commercial forbidden", map[string]string{"APP_ENABLE_OPEN_METEO_FORECAST": "true", "APP_DEPLOYMENT_USE": "commercial"}, false},
		{"openmeteo quota required", map[string]string{"APP_ENABLE_OPEN_METEO_FORECAST": "true", "APP_DEPLOYMENT_USE": "noncommercial"}, false},
		{"openmeteo valid", map[string]string{"APP_ENABLE_OPEN_METEO_FORECAST": "true", "APP_DEPLOYMENT_USE": "noncommercial", "APP_PROVIDER_QUOTAS_JSON": `{"open_meteo_forecast":{"per_minute":250,"per_day":4400}}`}, true},
		{"openmeteo bad boolean", map[string]string{"APP_ENABLE_OPEN_METEO_GEOCODING": "yes"}, false},
	}
	all := []string{"APP_ENABLE_GEOAPIFY", "APP_ENABLE_MET_NORWAY", "APP_ENABLE_OPEN_METEO_FORECAST", "APP_ENABLE_OPEN_METEO_GEOCODING", "APP_DEPLOYMENT_USE", "APP_MET_NORWAY_USER_AGENT", "APP_PROVIDER_QUOTAS_JSON"}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range all {
				t.Setenv(name, tt.env[name])
			}
			err := validatePhase06Activation()
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v unexpected err=%v", tt.valid, err)
			}
		})
	}
}
