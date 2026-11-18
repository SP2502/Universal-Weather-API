package main

import (
	"errors"
	"os"
	"strings"

	"github.com/universal-api/platform/internal/config"
)

const leanV1 = "lean_v1"
const experimental = "full_experimental"

// releaseProfile defaults to a narrow, stable surface in staging/production.
// A deliberate experimental profile is supported only in dev/test.
func releaseProfile() (string, error) {
	v := strings.TrimSpace(os.Getenv("APP_RELEASE_PROFILE"))
	if v == "" {
		return leanV1, nil
	}
	if v != leanV1 && v != experimental {
		return "", errors.New("APP_RELEASE_PROFILE must be lean_v1 or full_experimental")
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if v == experimental && (cfg.Environment == "production" || cfg.Environment == "staging") {
		return "", errors.New("full_experimental is not allowed in production/staging")
	}
	return v, nil
}

func validateLeanOptions(profile string) error {
	if profile != leanV1 {
		return nil
	}
	for _, name := range []string{"APP_ENABLE_PUBLIC_NOMINATIM", "APP_ENABLE_OPEN_METEO_AIR_QUALITY", "APP_ENABLE_OPEN_METEO_HISTORY", "APP_ENABLE_OPEN_METEO_MARINE", "APP_ENABLE_US_NWS_ALERTS", "APP_ENABLE_US_NWS", "APP_ENABLE_BRIGHT_SKY"} {
		if os.Getenv(name) == "true" {
			return errors.New(name + " is deferred in lean_v1; use full_experimental only in dev/test")
		}
	}
	return nil
}

func metricsModeAllowed(mode, bindHost string) (bool, error) {
	if mode == "" || mode == "off" {
		return false, nil
	}
	if mode != "loopback" {
		return false, errors.New("APP_METRICS_MODE must be off or loopback")
	}
	if !isLoopbackBind(bindHost) {
		return false, errors.New("metrics on the gateway listener requires a loopback bind")
	}
	return true, nil
}
