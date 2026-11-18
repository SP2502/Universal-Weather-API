package main

import (
	"context"
	"github.com/universal-api/platform/internal/secrets"
	"io"
	"log/slog"
	"testing"
)

func TestMissingProviderSecretsNeverRegisterAsWeather(t *testing.T) {
	store := secrets.NewMemorySecretStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry, count := registerConfiguredProviders(store, logger)
	if count != 0 {
		t.Fatalf("got %d weather providers without secrets", count)
	}
	if _, err := registry.GetWeatherProviders(context.Background()); err == nil {
		t.Fatal("empty secret store should have no weather provider")
	}
	if _, ok := registry.Get("nominatim"); ok {
		t.Fatal("public Nominatim must not be registered by default")
	}
}
func TestOnlyConfiguredWeatherProviderRegistered(t *testing.T) {
	store := secrets.NewMemorySecretStore()
	_ = store.SetSecret(context.Background(), "WEATHERAPI_API_KEY", "synthetic-test-key-not-real")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry, count := registerConfiguredProviders(store, logger)
	if count != 1 {
		t.Fatalf("want 1 configured weather provider, got %d", count)
	}
	if _, ok := registry.Get("openweather"); ok {
		t.Fatal("missing openweather key unexpectedly enabled adapter")
	}
	if _, ok := registry.Get("weatherapi"); !ok {
		t.Fatal("configured weatherapi not registered")
	}
}
