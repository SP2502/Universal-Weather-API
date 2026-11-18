package main

import "testing"

func TestPhase08NoDefaultDeveloperCredentialAndSafeOptIn(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_DB_DRIVER", "memory")
	t.Setenv("APP_DEV_CLIENT_KEY", "")
	t.Setenv("APP_BIND_HOST", "0.0.0.0")
	if err := validateDevClientKeyConfiguration(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_DEV_CLIENT_KEY", "long-operator-supplied-test-key-123456789")
	if err := validateDevClientKeyConfiguration(); err == nil {
		t.Fatal("exposed listener accepted developer key")
	}
	t.Setenv("APP_BIND_HOST", "127.0.0.1")
	if err := validateDevClientKeyConfiguration(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "production")
	if err := validateDevClientKeyConfiguration(); err == nil {
		t.Fatal("production accepted dev key")
	}
}
