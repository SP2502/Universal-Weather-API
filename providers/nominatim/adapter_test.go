package nominatim

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/provider"
)

func TestNominatimAdapter_RealHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("expected User-Agent header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/search" {
			_, _ = w.Write([]byte(`[{
				"place_id": 1001,
				"lat": "51.5074",
				"lon": "-0.1278",
				"display_name": "London, UK",
				"class": "place",
				"type": "city",
				"importance": 0.95,
				"address": {"city": "London", "country": "United Kingdom", "country_code": "gb"}
			}]`))
		} else {
			_, _ = w.Write([]byte(`{
				"place_id": 2002,
				"lat": "51.5074",
				"lon": "-0.1278",
				"display_name": "10 Downing Street, London",
				"class": "building",
				"type": "yes",
				"importance": 0.85,
				"address": {"house_number": "10", "road": "Downing Street", "city": "London", "country": "United Kingdom", "country_code": "gb"}
			}`))
		}
	}))
	defer ts.Close()

	adapter := NewAdapter(ts.URL)

	fwdResp, err := adapter.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "London"})
	if err != nil {
		t.Fatalf("unexpected forward error: %v", err)
	}
	if fwdResp.ProviderID != "nominatim" {
		t.Errorf("expected provider nominatim, got %s", fwdResp.ProviderID)
	}

	revResp, err := adapter.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 51.5074, Longitude: -0.1278})
	if err != nil {
		t.Fatalf("unexpected reverse error: %v", err)
	}
	if revResp.Mode != "reverse" {
		t.Errorf("expected mode reverse, got %s", revResp.Mode)
	}
}
