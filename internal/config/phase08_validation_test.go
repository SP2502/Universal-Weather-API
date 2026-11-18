package config

import "testing"

func TestPhase08RejectsMalformedEnvironmentNumbersAndDurations(t *testing.T) {
	for _, k := range []string{"APP_PORT", "APP_MAX_HEADER_BYTES", "APP_REDIS_DB"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv(k, "not-a-number")
			if _, err := Load(); err == nil {
				t.Fatalf("%s ignored malformed number", k)
			}
		})
	}
	for _, k := range []string{"APP_READ_HEADER_TIMEOUT", "APP_READ_TIMEOUT", "APP_WRITE_TIMEOUT", "APP_IDLE_TIMEOUT", "APP_SHUTDOWN_TIMEOUT"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv(k, "not-a-duration")
			if _, err := Load(); err == nil {
				t.Fatalf("%s ignored malformed timeout", k)
			}
		})
	}
	t.Run("negative Redis DB", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("APP_REDIS_DB", "-2")
		if _, err := Load(); err == nil {
			t.Fatal("accepted negative db index")
		}
	})
}
