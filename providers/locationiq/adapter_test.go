package locationiq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/provider"
)

func TestLocationIQAdapter_RealHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/search.php" {
			_, _ = w.Write([]byte(`[{
				"place_id": "liq-991",
				"lat": "48.8566",
				"lon": "2.3522",
				"display_name": "Paris, Île-de-France, France",
				"class": "place",
				"type": "city",
				"importance": 0.98,
				"address": {"city": "Paris", "state": "Île-de-France", "country": "France", "country_code": "fr"}
			}]`))
		} else {
			_, _ = w.Write([]byte(`{
				"place_id": "liq-992",
				"lat": "48.8584",
				"lon": "2.2945",
				"display_name": "Eiffel Tower, Paris, France",
				"class": "tourism",
				"type": "attraction",
				"importance": 0.99,
				"address": {"name": "Eiffel Tower", "city": "Paris", "country": "France", "country_code": "fr"}
			}`))
		}
	}))
	defer ts.Close()

	adapter := NewAdapter("test-key", ts.URL)

	fwdResp, err := adapter.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Paris"})
	if err != nil {
		t.Fatalf("unexpected forward error: %v", err)
	}
	if fwdResp.ProviderID != "locationiq" {
		t.Errorf("expected provider locationiq, got %s", fwdResp.ProviderID)
	}

	revResp, err := adapter.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 48.8584, Longitude: 2.2945})
	if err != nil {
		t.Fatalf("unexpected reverse error: %v", err)
	}
	if revResp.Mode != "reverse" {
		t.Errorf("expected mode reverse, got %s", revResp.Mode)
	}
}
