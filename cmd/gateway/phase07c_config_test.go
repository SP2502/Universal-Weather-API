package main

import "testing"

func TestPhase07C_GeocodeCacheAllowlist(t *testing.T) {
	for _, s := range []string{"nominatim", "unknown", "geoapify,geoapify", "geoapify,", "x y"} {
		if _, err := parseGeocodeCacheAllowlist(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	out, err := parseGeocodeCacheAllowlist("geoapify,locationiq")
	if err != nil || len(out) != 2 {
		t.Fatalf("valid allowlist rejected: %v %v", out, err)
	}
}
func TestPhase07C_AirQualityActivation(t *testing.T) {
	t.Setenv("APP_ENABLE_OPEN_METEO_AIR_QUALITY", "true")
	t.Setenv("APP_DEPLOYMENT_USE", "commercial")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("commercial hosted free activation accepted")
	}
	t.Setenv("APP_DEPLOYMENT_USE", "noncommercial")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_air_quality":{"per_minute":200,"per_day":4000}}`)
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENABLE_OPEN_METEO_FORECAST", "true")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_air_quality":{"per_minute":200,"per_day":4000},"open_meteo_forecast":{"per_minute":200,"per_day":4000}}`)
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("shared hosted quota group not required")
	}
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_air_quality":{"per_minute":200,"per_day":4000,"group":"openmeteo"},"open_meteo_forecast":{"per_minute":200,"per_day":4000,"group":"openmeteo"}}`)
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
}
