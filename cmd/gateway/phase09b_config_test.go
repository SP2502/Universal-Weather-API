package main

import (
	"strings"
	"testing"
)

func phase09BProductionConfig(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_DB_DRIVER", "postgres")
	t.Setenv("APP_DATABASE_URL", "postgres://synthetic.invalid/uap")
	t.Setenv("APP_CACHE_DRIVER", "redis")
	t.Setenv("APP_SECRET_DRIVER", "env")
	t.Setenv("APP_RELEASE_PROFILE", "lean_v1")
	t.Setenv("APP_REQUIRED_CAPABILITIES", "weather")
	t.Setenv("APP_METRICS_MODE", "off")
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "")
	t.Setenv("APP_ENABLE_OPEN_METEO_FORECAST", "false")
	t.Setenv("APP_ENABLE_MET_NORWAY", "false")
	t.Setenv("APP_ENABLE_GEOAPIFY", "false")
	t.Setenv("APP_ENABLE_US_NWS", "false")
	t.Setenv("APP_ENABLE_BRIGHT_SKY", "false")
	t.Setenv("APP_ENABLE_SUNRISE_SUNSET", "false")
	t.Setenv("APP_WEATHER_PLACE_RESOLVER", "disabled")
	t.Setenv("APP_DEPLOYMENT_USE", "internal")
	t.Setenv("OPENWEATHER_API_KEY", "")
	t.Setenv("WEATHERAPI_API_KEY", "")
	t.Setenv("LOCATIONIQ_API_KEY", "")
}

func TestPhase09BConfigRejectsUnrelatedQuotaWithoutProviders(t *testing.T) {
	phase09BProductionConfig(t)
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"unrelated_provider":{"per_day":5}}`)
	err := run([]string{"--check-config"})
	if err == nil {
		t.Fatal("production preflight accepted an unrelated budget and no usable weather provider")
	}
	if !strings.Contains(err.Error(), "no configured weather provider") {
		t.Fatalf("wrong failure reason: %v", err)
	}
}

func TestPhase09BConfigRejectsMissingBudgetForRegisteredProvider(t *testing.T) {
	phase09BProductionConfig(t)
	t.Setenv("OPENWEATHER_API_KEY", "synthetic-never-contact-live")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"unrelated_provider":{"per_day":5}}`)
	err := run([]string{"--check-config"})
	if err == nil || !strings.Contains(err.Error(), `"openweather"`) {
		t.Fatalf("missing source-matched budget must fail: %v", err)
	}
}

func TestPhase09BConfigAcceptsSafeDeclaredProviderWithoutLiveConnections(t *testing.T) {
	phase09BProductionConfig(t)
	t.Setenv("OPENWEATHER_API_KEY", "synthetic-never-contact-live")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"openweather":{"per_minute":10,"per_day":80}}`)
	if err := run([]string{"--check-config"}); err != nil {
		t.Fatalf("declared provider and quota should pass offline preflight, got: %v", err)
	}
	t.Setenv("LOCATIONIQ_API_KEY", "another-synthetic-key")
	if err := run([]string{"--check-config"}); err == nil || !strings.Contains(err.Error(), `"locationiq"`) {
		t.Fatalf("config must account for geocoder registration too: %v", err)
	}
}

func TestPhase09BConfigRejectsRedisMissingOrBudgetsEmpty(t *testing.T) {
	phase09BProductionConfig(t)
	t.Setenv("OPENWEATHER_API_KEY", "synthetic-never-contact-live")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"openweather":{"per_day":80}}`)
	t.Setenv("APP_CACHE_DRIVER", "memory")
	if err := run([]string{"--check-config"}); err == nil {
		t.Fatal("accepted production without shared Redis")
	}
	t.Setenv("APP_CACHE_DRIVER", "redis")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", "")
	if err := run([]string{"--check-config"}); err == nil {
		t.Fatal("accepted production without budgets")
	}
}
