package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.Environment != "development" {
		t.Errorf("expected development env, got %s", cfg.Environment)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("expected INFO log level, got %s", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected 10s shutdown timeout, got %v", cfg.ShutdownTimeout)
	}
	if cfg.DBDriver != "memory" {
		t.Errorf("expected memory db driver, got %s", cfg.DBDriver)
	}
	if cfg.CacheDriver != "memory" {
		t.Errorf("expected memory cache driver, got %s", cfg.CacheDriver)
	}
	if cfg.SecretDriver != "chained" {
		t.Errorf("expected chained secret driver, got %s", cfg.SecretDriver)
	}
}

func TestValidationErrors(t *testing.T) {
	cfg := &Config{
		Port:            -1,
		ReadTimeout:     time.Second,
		WriteTimeout:    time.Second,
		ShutdownTimeout: time.Second,
		LogLevel:        "INFO",
		DBDriver:        "memory",
		CacheDriver:     "memory",
		SecretDriver:    "chained",
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative port")
	}

	cfg.Port = 8080
	cfg.LogLevel = "INVALID"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid log level")
	}

	cfg.LogLevel = "INFO"
	cfg.DBDriver = "invalid_db"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid db driver")
	}

	cfg.DBDriver = "memory"
	cfg.CacheDriver = "invalid_cache"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid cache driver")
	}

	cfg.CacheDriver = "memory"
	cfg.SecretDriver = "invalid_secret"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid secret driver")
	}
}
