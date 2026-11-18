package config

import (
	"testing"
	"time"
)

func TestProductionRequiresDurableStore(t *testing.T) {
	cfg := &Config{Environment: "production", Port: 8080, ReadTimeout: time.Second, WriteTimeout: time.Second, ShutdownTimeout: time.Second, LogLevel: "INFO", DBDriver: "memory", CacheDriver: "memory", SecretDriver: "env"}
	if cfg.Validate() == nil {
		t.Fatal("production memory storage must be rejected")
	}
	cfg.DBDriver = "postgres"
	if cfg.Validate() == nil {
		t.Fatal("production without DSN must be rejected")
	}
	cfg.DatabaseURL = "postgres://example.invalid/uap"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid storage config rejected: %v", err)
	}
}
