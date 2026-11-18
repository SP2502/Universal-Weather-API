package normalization

import (
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

func TestGeocodingNormalizer_Forward(t *testing.T) {
	norm := NewGeocodingNormalizer()

	rawJSON := []byte(`[{
		"lat": "51.5074",
		"lon": "-0.1278",
		"display_name": "London, UK",
		"class": "place",
		"type": "city",
		"importance": 0.9,
		"address": {"city": "London", "country": "United Kingdom", "country_code": "gb"}
	}]`)

	raw := &provider.RawGeocodeResponse{
		ProviderID:  "nominatim",
		Mode:        "forward",
		RawPayload:  rawJSON,
		RetrievedAt: time.Now().UTC(),
	}

	resp, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	res := resp.Results[0]
	if res.City != "London" || res.CountryCode != "GB" {
		t.Errorf("unexpected location: %+v", res)
	}
	if res.Latitude != 51.5074 || res.Longitude != -0.1278 {
		t.Errorf("unexpected coords: %f, %f", res.Latitude, res.Longitude)
	}
}

func TestGeocodingNormalizer_Reverse(t *testing.T) {
	norm := NewGeocodingNormalizer()

	rawJSON := []byte(`{
		"lat": "48.8584",
		"lon": "2.2945",
		"display_name": "Eiffel Tower, Paris",
		"class": "tourism",
		"type": "attraction",
		"importance": 0.95,
		"address": {"city": "Paris", "country": "France", "country_code": "fr"}
	}`)

	raw := &provider.RawGeocodeResponse{
		ProviderID:  "locationiq",
		Mode:        "reverse",
		RawPayload:  rawJSON,
		RetrievedAt: time.Now().UTC(),
	}

	resp, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].City != "Paris" || resp.Results[0].CountryCode != "FR" {
		t.Errorf("unexpected result: %+v", resp.Results[0])
	}
}
