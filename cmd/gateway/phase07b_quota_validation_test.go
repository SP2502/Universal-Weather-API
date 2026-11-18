package main

import "testing"

func TestPhase07B_SharedHostedOpenMeteoBudgetRequired(t *testing.T) {
	t.Setenv("APP_ENABLE_OPEN_METEO_FORECAST", "true")
	t.Setenv("APP_ENABLE_OPEN_METEO_GEOCODING", "true")
	t.Setenv("APP_DEPLOYMENT_USE", "noncommercial")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_forecast":{"per_minute":200,"per_day":4400},"open_meteo_geocoding":{"per_minute":200,"per_day":4400}}`)
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("shared allowance must be explicit")
	}
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_forecast":{"group":"open_meteo","per_minute":200,"per_day":4400},"open_meteo_geocoding":{"group":"open_meteo","per_minute":200,"per_day":4400}}`)
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
}
