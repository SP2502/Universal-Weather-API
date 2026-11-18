package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckConfigCLI(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_DB_DRIVER", "memory")
	t.Setenv("APP_PORT", "18411")
	if err := run([]string{"--check-config"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--check-config", "extra"}); err == nil {
		t.Fatal("unexpected arg accepted")
	}
	p := filepath.Join(t.TempDir(), "gateway.env")
	if err := os.WriteFile(p, []byte("APP_TEST_GATEWAY_OPTION=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--env-file", p, "--check-config"}); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("APP_TEST_GATEWAY_OPTION")
}
