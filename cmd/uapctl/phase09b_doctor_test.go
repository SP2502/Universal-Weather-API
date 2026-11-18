package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func doctorPhase09B(t *testing.T) doctorReport {
	t.Helper()
	var b bytes.Buffer
	if err := run([]string{"doctor", "--json"}, &b); err == nil {
		t.Fatal("expected failed offline doctor exit when production quota protection is incomplete")
	}
	var report doctorReport
	if err := json.Unmarshal(b.Bytes(), &report); err != nil {
		t.Fatalf("doctor JSON: %v, raw: %s", err, b.String())
	}
	if report.LiveIntegrationVerified {
		t.Fatal("doctor must never claim live verification")
	}
	return report
}

func checkResult(t *testing.T, report doctorReport, name, want string) {
	t.Helper()
	for _, c := range report.Checks {
		if c.Name == name {
			if c.Result != want {
				t.Errorf("%s: got %q, expected %q", name, c.Result, want)
			}
			return
		}
	}
	t.Errorf("missing doctor check %q", name)
}

func TestPhase09BDoctorProductionWithoutQuotasOrRedisFails(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_DB_DRIVER", "postgres")
	t.Setenv("APP_DATABASE_URL", "postgres://invalid.local/uwa_synthetic")
	t.Setenv("APP_CACHE_DRIVER", "memory")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", "")
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "")
	report := doctorPhase09B(t)
	checkResult(t, report, "provider_quotas", "fail")
	checkResult(t, report, "shared_quotas", "fail")
}

func TestPhase09BDoctorProductionRedisWithoutBudgetsFails(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_DB_DRIVER", "postgres")
	t.Setenv("APP_DATABASE_URL", "postgres://invalid.local/uwa_synthetic")
	t.Setenv("APP_CACHE_DRIVER", "redis")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", "")
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "")
	report := doctorPhase09B(t)
	checkResult(t, report, "provider_quotas", "fail")
	checkResult(t, report, "shared_quotas", "fail")
}

func TestPhase09BDoctorProductionMemoryWithBudgetsFails(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_DB_DRIVER", "postgres")
	t.Setenv("APP_DATABASE_URL", "postgres://invalid.local/uwa_synthetic")
	t.Setenv("APP_CACHE_DRIVER", "memory")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", `{"openweather":{"per_day":50}}`)
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "")
	report := doctorPhase09B(t)
	checkResult(t, report, "shared_quotas", "fail")
}
