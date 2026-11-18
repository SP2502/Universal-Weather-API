package main

import (
	"github.com/universal-api/platform/internal/config"
	"testing"
)

func TestStorageBootstrapFailsClosed(t *testing.T) {
	if _, err := initializeStore(&config.Config{Environment: "production", DBDriver: "memory"}); err == nil {
		t.Fatal("production memory must fail")
	}
	if _, err := initializeStore(&config.Config{Environment: "production", DBDriver: "postgres"}); err == nil {
		t.Fatal("missing DSN must fail")
	}
	store, err := initializeStore(&config.Config{Environment: "development", DBDriver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
}
