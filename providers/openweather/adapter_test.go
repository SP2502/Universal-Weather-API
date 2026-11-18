package openweather

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenWeatherAdapter_RealHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "London" {
			t.Errorf("expected q=London, got %s", r.URL.Query().Get("q"))
		}
		if r.Header.Get("User-Agent") != "UniversalAPIPlatform/1.0" {
			t.Errorf("expected User-Agent header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"coord": {"lon": -0.1278, "lat": 51.5074},
			"weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
			"main": {"temp": 295.65, "humidity": 60},
			"name": "London",
			"dt": ` + fmt.Sprint(time.Now().Unix()) + `
		}`))
	}))
	defer ts.Close()

	adapter := NewAdapter("test-api-key", ts.URL)
	resp, err := adapter.FetchWeather(context.Background(), "London")
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}

	if resp.ProviderID != "openweather" {
		t.Errorf("expected openweather, got %s", resp.ProviderID)
	}

	payload, ok := resp.RawPayload.(OpenWeatherRawPayload)
	if !ok || payload.Name != "London" || payload.Main.Temp != 295.65 {
		t.Errorf("unexpected raw payload: %+v", resp.RawPayload)
	}
}
