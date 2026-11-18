package main

import "testing"

func TestPhase07DActivationAndSharedQuotas(t *testing.T) {
	t.Setenv("APP_DEPLOYMENT_USE", "commercial")
	t.Setenv("APP_ENABLE_OPEN_METEO_HISTORY", "true")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("commercial hosted history incorrectly enabled")
	}
	t.Setenv("APP_DEPLOYMENT_USE", "noncommercial")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("hosted history requires quota")
	}
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_history":{"per_minute":200,"per_day":4000,"unit":"weighted_calls","cost_milli":10000}}`)
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENABLE_OPEN_METEO_MARINE", "true")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("shared quotas not enforced")
	}
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"open_meteo_history":{"per_minute":200,"per_day":4000,"unit":"weighted_calls","cost_milli":10000,"group":"openmeteo"},"open_meteo_marine":{"per_minute":200,"per_day":4000,"unit":"weighted_calls","cost_milli":10000,"group":"openmeteo"}}`)
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENABLE_US_NWS_ALERTS", "true")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("NWS alerts need identifying User-Agent")
	}
	t.Setenv("APP_NWS_USER_AGENT", "UWA Test (ops@example.org)")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "operator-reviewed-country-index.json")
	if err := validatePhase06Activation(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENABLE_SUNRISE_SUNSET", "perhaps")
	if err := validatePhase06Activation(); err == nil {
		t.Fatal("invalid opt-in flag accepted")
	}
}
func TestPhase07DRequiredCapabilities(t *testing.T) {
	values, err := requiredCapabilities("alerts,marine,history,astronomy")
	if err != nil || len(values) != 4 {
		t.Fatalf("expected 4 capability names: %v %v", values, err)
	}
	for _, s := range []string{"marine,marine", "history,unknown", ""} {
		_, e := requiredCapabilities(s)
		if s == "" && e != nil {
			t.Fatal("empty should preserve weather default")
		}
		if s != "" && e == nil {
			t.Fatalf("accepted invalid %q", s)
		}
	}
}
