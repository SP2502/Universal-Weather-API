package weatherapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWeatherAPIAdapter_RealHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Paris" {
			t.Errorf("expected q=Paris, got %s", r.URL.Query().Get("q"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"location": {"name": "Paris", "country": "France", "lat": 48.8566, "lon": 2.3522, "tz_id": "Europe/Paris"},
			"current": {
				"temp_c": 19.4,
				"humidity": 70,
				"condition": {"text": "Partly cloudy", "code": 1003},
				"last_updated_epoch": ` + fmt.Sprint(time.Now().Unix()) + `
			}
		}`))
	}))
	defer ts.Close()

	adapter := NewAdapter("test-key", ts.URL)
	resp, err := adapter.FetchWeather(context.Background(), "Paris")
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}

	payload, ok := resp.RawPayload.(WeatherAPIRawPayload)
	if !ok || payload.Location.Name != "Paris" || payload.Current.TempC != 19.4 {
		t.Errorf("unexpected raw payload: %+v", resp.RawPayload)
	}
}
