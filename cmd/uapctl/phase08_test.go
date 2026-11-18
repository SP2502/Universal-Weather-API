package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPhase08AllScopesProvisionable(t *testing.T) {
	for _, scope := range []string{"weather:read", "geocode:read", "forecast:read", "air-quality:read", "history:read", "astronomy:read", "marine:read", "alerts:read"} {
		t.Run(scope, func(t *testing.T) {
			_, scopes, _, err := parseKeyArgs([]string{"create-key", "--name", "operator", "--scopes", scope})
			if err != nil || len(scopes) != 1 || scopes[0] != scope {
				t.Fatalf("scope %s rejected: %v", scope, err)
			}
		})
	}
	if _, _, _, err := parseKeyArgs([]string{"create-key", "--name", "operator", "--scopes", ""}); err == nil {
		t.Fatal("accepted empty scopes")
	}
	if _, _, _, err := parseKeyArgs([]string{"create-key", "--name", "operator", "--scopes", "weather:read,admin:*"}); err == nil {
		t.Fatal("accepted privilege escalation")
	}
}

func TestPhase08OfflineDoctorDoesNotPrintSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_DB_DRIVER", "memory")
	t.Setenv("APP_CACHE_DRIVER", "memory")
	t.Setenv("APP_PORT", "18501")
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_DATABASE_URL", "postgres://private:supersecret@localhost/local")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "")
	t.Setenv("APP_COUNTRY_POLYGONS_FILE", "")
	t.Setenv("APP_PROVIDER_QUOTAS_JSON", "")
	var b bytes.Buffer
	if err := run([]string{"doctor", "--json"}, &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "supersecret") {
		t.Fatal("secret exposed in diagnostic output")
	}
	var report doctorReport
	if err := json.Unmarshal(b.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.LiveIntegrationVerified || report.Mode != "offline" {
		t.Fatalf("unexpected acceptance claim: %+v", report)
	}
	if len(report.Checks) < 4 {
		t.Fatal("missing checks")
	}
}

func TestPhase08DoctorFailsOnBrokenPolicyWithoutSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_DB_DRIVER", "memory")
	t.Setenv("APP_PORT", "18501")
	t.Setenv("APP_PROVIDER_POLICY_FILE", "/no/such/policy/file.json")
	var b bytes.Buffer
	if err := run([]string{"doctor", "--json"}, &b); err == nil {
		t.Fatal("broken policy accepted")
	}
	if !strings.Contains(b.String(), `"result": "fail"`) {
		t.Fatalf("missing failed check: %s", b.String())
	}
}
