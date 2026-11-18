package contract

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
)

func TestFixtures_Nominatim_Contract(t *testing.T) {
	norm := normalization.NewGeocodingNormalizer()

	// Forward
	fwdBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "geocoding", "nominatim_forward_success.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	raw := &provider.RawGeocodeResponse{
		ProviderID:  "nominatim",
		Mode:        "forward",
		RawPayload:  fwdBytes,
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(canonical.Results))
	}
	if canonical.Results[0].City != "London" || canonical.Results[0].CountryCode != "GB" {
		t.Errorf("unexpected location: %+v", canonical.Results[0])
	}

	// Reverse
	revBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "geocoding", "nominatim_reverse_success.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	raw.Mode = "reverse"
	raw.RawPayload = revBytes
	canonicalRev, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected reverse error: %v", err)
	}
	if len(canonicalRev.Results) != 1 {
		t.Fatalf("expected 1 reverse result, got %d", len(canonicalRev.Results))
	}
	if canonicalRev.Results[0].HouseNumber != "10" || canonicalRev.Results[0].Street != "Downing Street" {
		t.Errorf("unexpected reverse result: %+v", canonicalRev.Results[0])
	}
}

func TestFixtures_LocationIQ_Contract(t *testing.T) {
	norm := normalization.NewGeocodingNormalizer()

	fwdBytes, err := os.ReadFile(filepath.Join("..", "fixtures", "geocoding", "locationiq_forward_success.json"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	raw := &provider.RawGeocodeResponse{
		ProviderID:  "locationiq",
		Mode:        "forward",
		RawPayload:  fwdBytes,
		RetrievedAt: time.Now().UTC(),
	}

	canonical, err := norm.Normalize(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Results) != 1 || canonical.Results[0].City != "Paris" {
		t.Errorf("unexpected location result: %+v", canonical.Results)
	}
}
