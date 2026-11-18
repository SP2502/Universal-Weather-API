package integration

import (
	"testing"

	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/brightsky"
	"github.com/universal-api/platform/providers/geoapify"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/metnorway"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/nwsalerts"
	"github.com/universal-api/platform/providers/openmeteo"
	"github.com/universal-api/platform/providers/openmeteoairquality"
	"github.com/universal-api/platform/providers/openmeteogeocode"
	"github.com/universal-api/platform/providers/openmeteohistory"
	"github.com/universal-api/platform/providers/openmeteomarine"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/sunrisesunset"
	"github.com/universal-api/platform/providers/usnws"
	"github.com/universal-api/platform/providers/weatherapi"
)

// A regression check against the actual adapter manifests, not a second
// hardcoded expected provider-ID list. No network is called by constructors.
func TestNormalizationCatalogMatchesAllAdapterManifests(t *testing.T) {
	adapters := []provider.Provider{
		openweather.NewAdapter("test", ""), weatherapi.NewAdapter("test", ""),
		metnorway.NewAdapter("UWA Test contact@example.test", ""), openmeteo.NewAdapter(""),
		usnws.NewAdapter("UWA Test contact@example.test", ""), brightsky.NewAdapter("", 25000),
		nominatim.NewAdapter(""), locationiq.NewAdapter("test", ""),
		geoapify.NewAdapter("test", ""), openmeteogeocode.NewAdapter(""),
		openmeteoairquality.NewAdapter(""), openmeteohistory.NewAdapter(""),
		sunrisesunset.NewAdapter(""), openmeteomarine.NewAdapter(""),
		nwsalerts.NewAdapter("UWA Test contact@example.test", ""),
	}
	catalog := map[string]normalization.ProviderSchemaContract{}
	for _, c := range normalization.ProviderSchemaCatalog() {
		catalog[c.ProviderID] = c
	}
	if len(catalog) != len(adapters) {
		t.Fatalf("schema registry has %d entries, adapter list has %d", len(catalog), len(adapters))
	}
	seen := map[string]bool{}
	for _, adapter := range adapters {
		manifest := adapter.Manifest()
		if seen[manifest.ID] {
			t.Errorf("duplicate provider ID %q", manifest.ID)
		}
		seen[manifest.ID] = true
		contract, exists := catalog[manifest.ID]
		if !exists {
			t.Errorf("missing schema normalization entry for %q", manifest.ID)
			continue
		}
		capability := contract.Product
		if capability == "geocoding" {
			capability = "geocode"
		}
		found := false
		for _, c := range manifest.Capabilities {
			if c == capability {
				found = true
			}
		}
		if !found {
			t.Errorf("%q capability %q missing from %v", manifest.ID, capability, manifest.Capabilities)
		}
	}
}
