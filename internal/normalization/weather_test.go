package normalization

import (
	"fmt"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

func TestNormalizeOpenWeather(t *testing.T) {
	norm := NewWeatherNormalizer()

	rawJson := []byte(`{
		"coord": {"lon": -0.1278, "lat": 51.5074},
		"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
		"main": {"temp": 295.65, "humidity": 65},
		"name": "London",
		"dt": ` + fmt.Sprint(time.Now().Unix()) + `
	}`)

	raw := &provider.RawWeatherResponse{
		ProviderID:  "openweather",
		RawPayload:  rawJson,
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected normalization error: %v", err)
	}

	if canonical.SchemaVersion != "weather.v1" {
		t.Errorf("expected weather.v1, got %s", canonical.SchemaVersion)
	}
	if canonical.Temperature.Value != 22.5 {
		t.Errorf("expected 22.5 C, got %f", canonical.Temperature.Value)
	}
	if canonical.Temperature.Unit != "celsius" {
		t.Errorf("expected celsius, got %s", canonical.Temperature.Unit)
	}
	if canonical.Humidity == nil || canonical.Humidity.Value != 65 {
		t.Errorf("expected humidity 65%%, got %v", canonical.Humidity)
	}
	if canonical.Condition != weatherv1.ConditionClear {
		t.Errorf("expected condition clear, got %s", canonical.Condition)
	}
	if canonical.Location.City != "London" {
		t.Errorf("expected London, got %s", canonical.Location.City)
	}
}

func TestNormalizeWeatherAPI(t *testing.T) {
	norm := NewWeatherNormalizer()

	rawJson := []byte(`{
		"location": {"name": "Paris", "country": "France", "lat": 48.8566, "lon": 2.3522, "tz_id": "Europe/Paris"},
		"current": {
			"temp_c": 19.4,
			"humidity": 70,
			"condition": {"text": "Partly cloudy", "code": 1003},
			"last_updated_epoch": ` + fmt.Sprint(time.Now().Unix()) + `
		}
	}`)

	raw := &provider.RawWeatherResponse{
		ProviderID:  "weatherapi",
		RawPayload:  rawJson,
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected normalization error: %v", err)
	}

	if canonical.Temperature.Value != 19.4 {
		t.Errorf("expected 19.4 C, got %f", canonical.Temperature.Value)
	}
	if canonical.Condition != weatherv1.ConditionPartlyCloudy {
		t.Errorf("expected condition partly_cloudy, got %s", canonical.Condition)
	}
	if canonical.Location.Country != "France" {
		t.Errorf("expected France, got %s", canonical.Location.Country)
	}
}

func TestValidationBoundsFailure(t *testing.T) {
	norm := NewWeatherNormalizer()

	// Temperature 500K -> 226.85C (exceeds 70C limit)
	rawJson := []byte(`{
		"coord": {"lon": 0, "lat": 0},
		"weather": [{"id": 800, "main": "Clear"}],
		"main": {"temp": 500.0, "humidity": 50},
		"name": "Venus",
		"dt": ` + fmt.Sprint(time.Now().Unix()) + `
	}`)

	raw := &provider.RawWeatherResponse{
		ProviderID:  "openweather",
		RawPayload:  rawJson,
		RetrievedAt: time.Now().UTC(),
	}

	_, err := norm.Normalize(raw)
	if err == nil {
		t.Fatal("expected temperature validation failure for 226C, got nil")
	}
}
