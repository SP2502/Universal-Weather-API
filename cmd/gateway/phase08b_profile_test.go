package main

import (
	"strings"
	"testing"
)

func TestPhase08BLeanProfileRejectsDeferredFeatures(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_RELEASE_PROFILE", "")
	profile, err := releaseProfile()
	if err != nil || profile != leanV1 {
		t.Fatalf("want lean default: %v %v", profile, err)
	}
	for _, name := range []string{"APP_ENABLE_OPEN_METEO_HISTORY", "APP_ENABLE_US_NWS_ALERTS", "APP_ENABLE_PUBLIC_NOMINATIM"} {
		t.Setenv(name, "true")
		if err := validateLeanOptions(profile); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s not rejected: %v", name, err)
		}
		t.Setenv(name, "")
	}
	t.Setenv("APP_RELEASE_PROFILE", "full_experimental")
	if prof, err := releaseProfile(); err != nil || prof != experimental {
		t.Fatalf("dev experimental rejected: %s %v", prof, err)
	}
}
func TestPhase08BProductionCannotRunExperimental(t *testing.T) {
	t.Setenv("APP_RELEASE_PROFILE", "full_experimental")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_DB_DRIVER", "postgres")
	t.Setenv("APP_DATABASE_URL", "postgres://test.example/uwa")
	if _, err := releaseProfile(); err == nil {
		t.Fatal("experimental production profile allowed")
	}
}
func TestPhase08BMetricsLoopbackAndDisabledByDefault(t *testing.T) {
	for _, tc := range []struct {
		mode, host   string
		enabled, bad bool
	}{
		{"", "0.0.0.0", false, false}, {"off", "0.0.0.0", false, false},
		{"loopback", "127.0.0.1", true, false},
		{"loopback", "::1", true, false},
		{"loopback", "0.0.0.0", false, true},
		{"public", "127.0.0.1", false, true},
	} {
		enabled, err := metricsModeAllowed(tc.mode, tc.host)
		if enabled != tc.enabled || (err != nil) != tc.bad {
			t.Errorf("%+v -> enabled %v err %v", tc, enabled, err)
		}
	}
}
