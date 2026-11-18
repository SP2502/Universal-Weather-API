package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

func TestFixtures_OpenWeather_Contract(t *testing.T) {
	norm := normalization.NewWeatherNormalizer()

	// 1. Success Fixture
	succBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "weather", "openweather_success.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	raw := &provider.RawWeatherResponse{
		ProviderID:  "openweather",
		RawPayload:  currentFixture(t, succBytes),
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("expected clean normalization, got: %v", err)
	}

	if canonical.SchemaVersion != "weather.v1" {
		t.Errorf("expected weather.v1, got %s", canonical.SchemaVersion)
	}
	if canonical.Temperature.Value != 22.5 {
		t.Errorf("expected 22.5 C, got %f", canonical.Temperature.Value)
	}
	if canonical.Condition != weatherv1.ConditionClear {
		t.Errorf("expected clear, got %s", canonical.Condition)
	}

	// 2. Missing field fixture (Null humidity allowed)
	missBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "weather", "openweather_missing_field.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	raw.RawPayload = currentFixture(t, missBytes)
	canonicalMiss, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("expected missing humidity to normalize cleanly, got: %v", err)
	}
	if canonicalMiss.Humidity != nil {
		t.Errorf("expected nil humidity, got %v", canonicalMiss.Humidity)
	}

	// 3. Malformed fixture
	malBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "weather", "openweather_malformed.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	raw.RawPayload = malBytes
	_, err = norm.Normalize(raw)
	if err == nil {
		t.Fatal("expected malformed fixture to error, got nil")
	}
}

func TestFixtures_WeatherAPI_Contract(t *testing.T) {
	norm := normalization.NewWeatherNormalizer()

	// 1. Success Fixture
	succBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "weather", "weatherapi_success.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	raw := &provider.RawWeatherResponse{
		ProviderID:  "weatherapi",
		RawPayload:  currentFixture(t, succBytes),
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("expected clean normalization, got: %v", err)
	}

	if canonical.Location.City != "Tokyo" {
		t.Errorf("expected Tokyo, got %s", canonical.Location.City)
	}
	if canonical.Temperature.Value != 26.8 {
		t.Errorf("expected 26.8, got %f", canonical.Temperature.Value)
	}
	if canonical.Condition != weatherv1.ConditionPartlyCloudy {
		t.Errorf("expected partly_cloudy, got %s", canonical.Condition)
	}

	// 2. Edge Case Fixture (Subzero temp & Snow)
	edgeBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "weather", "weatherapi_edge_case.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	raw.RawPayload = currentFixture(t, edgeBytes)
	canonicalEdge, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("expected subzero weather to normalize cleanly, got: %v", err)
	}
	if canonicalEdge.Temperature.Value != -12.4 {
		t.Errorf("expected -12.4 C, got %f", canonicalEdge.Temperature.Value)
	}
	if canonicalEdge.Condition != weatherv1.ConditionSnow {
		t.Errorf("expected snow, got %s", canonicalEdge.Condition)
	}
}

// Success fixture timestamps are made current at load; archived raw samples
// remain unchanged. Dedicated regressions separately require old data to fail.
func currentFixture(t *testing.T, b []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["dt"]; ok {
		doc["dt"] = time.Now().Unix()
	}
	if current, ok := doc["current"].(map[string]any); ok {
		current["last_updated_epoch"] = time.Now().Unix()
	}
	result, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
