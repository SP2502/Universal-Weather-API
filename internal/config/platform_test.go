package config

import "testing"

func TestListenAddressAndHardening(t *testing.T) {
	t.Setenv("APP_BIND_HOST", "::1")
	t.Setenv("APP_PORT", "8801")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ListenAddress(); got != "[::1]:8801" {
		t.Fatalf("got %s", got)
	}
	t.Setenv("APP_MAX_HEADER_BYTES", "10")
	if _, err := Load(); err == nil {
		t.Fatal("small header cap should fail")
	}
}
func TestDefaultPrivateBind(t *testing.T) {
	t.Setenv("APP_BIND_HOST", "127.0.0.1")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BindHost != "127.0.0.1" {
		t.Fatal(cfg.BindHost)
	}
}
