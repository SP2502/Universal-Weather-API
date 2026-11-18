package normalization

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/universal-api/platform/internal/provider"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

type GeocodingNormalizer struct{}

func NewGeocodingNormalizer() *GeocodingNormalizer { return &GeocodingNormalizer{} }

// The upstream JSON is decoded once for both supported OSM-like response formats.
// Required coordinates remain strings until validated, so absent/null/invalid
// values cannot silently be converted into the real location (0,0).
type geocodeUpstreamItem struct {
	Lat         string   `json:"lat"`
	Lon         string   `json:"lon"`
	DisplayName string   `json:"display_name"`
	Class       string   `json:"class"`
	Type        string   `json:"type"`
	Importance  *float64 `json:"importance"`
	Address     struct {
		Name        string `json:"name,omitempty"`
		HouseNumber string `json:"house_number,omitempty"`
		Road        string `json:"road,omitempty"`
		Suburb      string `json:"suburb,omitempty"`
		City        string `json:"city,omitempty"`
		County      string `json:"county,omitempty"`
		State       string `json:"state,omitempty"`
		Postcode    string `json:"postcode,omitempty"`
		Country     string `json:"country,omitempty"`
		CountryCode string `json:"country_code,omitempty"`
	} `json:"address"`
}

func upstreamGeocodeCoordinates(latText, lonText string) (float64, float64, error) {
	if strings.TrimSpace(latText) == "" || strings.TrimSpace(lonText) == "" {
		return 0, 0, fmt.Errorf("%w: missing geocoding coordinates", provider.ErrMalformedResponse)
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(latText), 64)
	lon, lonErr := strconv.ParseFloat(strings.TrimSpace(lonText), 64)
	if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("%w: invalid geocoding coordinates", provider.ErrMalformedResponse)
	}
	return lat, lon, nil
}

func (n *GeocodingNormalizer) Normalize(raw *provider.RawGeocodeResponse) (*locv1.GeocodeResponse, error) {
	if raw == nil {
		return nil, fmt.Errorf("%w: missing geocoding response", provider.ErrMalformedResponse)
	}
	if raw.ProviderID != "nominatim" && raw.ProviderID != "locationiq" && raw.ProviderID != "geoapify" && raw.ProviderID != "open_meteo_geocoding" {
		return nil, fmt.Errorf("unsupported geocoding provider: %s", raw.ProviderID)
	}
	if raw.Mode != "forward" && raw.Mode != "reverse" {
		return nil, fmt.Errorf("%w: unsupported geocoding mode", provider.ErrMalformedResponse)
	}
	var payload []byte
	var err error
	switch v := raw.RawPayload.(type) {
	case []byte:
		payload = v
	case json.RawMessage:
		payload = v
	default:
		payload, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid geocoding payload", provider.ErrMalformedResponse)
		}
	}
	if len(payload) == 0 || string(payload) == "null" {
		return nil, fmt.Errorf("%w: empty geocoding payload", provider.ErrMalformedResponse)
	}
	if raw.ProviderID == "geoapify" {
		return normalizeGeoapify(payload, raw.Mode)
	}
	if raw.ProviderID == "open_meteo_geocoding" {
		if raw.Mode != "forward" {
			return nil, provider.ErrProviderNotFound
		}
		return normalizeOpenMeteoGeocode(payload)
	}
	var items []geocodeUpstreamItem
	if raw.Mode == "forward" {
		if err = json.Unmarshal(payload, &items); err != nil || items == nil {
			return nil, fmt.Errorf("%w: invalid forward geocoding payload", provider.ErrMalformedResponse)
		}
	} else {
		var single geocodeUpstreamItem
		if err = json.Unmarshal(payload, &single); err != nil || string(payload) == "[]" {
			return nil, fmt.Errorf("%w: invalid reverse geocoding payload", provider.ErrMalformedResponse)
		}
		items = []geocodeUpstreamItem{single}
	}
	// Explicit empty forward result is legitimate; JSON must be [] rather than null.
	results := make([]locv1.Location, 0, len(items))
	for i, it := range items {
		lat, lon, err := upstreamGeocodeCoordinates(it.Lat, it.Lon)
		if err != nil {
			return nil, fmt.Errorf("geocoding result %d: %w", i, err)
		}
		if strings.TrimSpace(it.DisplayName) == "" {
			return nil, fmt.Errorf("%w: result %d missing display_name", provider.ErrMalformedResponse, i)
		}
		loc := locv1.Location{
			Latitude: lat, Longitude: lon, FormattedAddress: it.DisplayName,
			HouseNumber: it.Address.HouseNumber, Street: it.Address.Road,
			Neighborhood: it.Address.Suburb, City: it.Address.City,
			District: it.Address.County, State: it.Address.State,
			Country: it.Address.Country, CountryCode: strings.ToUpper(it.Address.CountryCode),
			PostalCode: it.Address.Postcode, Precision: mapPrecision(it.Class, it.Type),
			ResultType: it.Type,
		}
		// OSM `importance` measures search prominence/ranking, not the
		// probability that geocoding coordinates are correct. Do not put it
		// into the public `confidence` field.
		if err := ValidateLocation(&loc); err != nil {
			return nil, fmt.Errorf("%w: result %d invalid: %v", provider.ErrMalformedResponse, i, err)
		}
		results = append(results, loc)
	}
	return &locv1.GeocodeResponse{SchemaVersion: "location.v1", Results: results}, nil
}

func mapPrecision(classStr, typeStr string) string {
	switch classStr {
	case "building":
		return "building"
	case "highway":
		return "street"
	case "place":
		switch typeStr {
		case "house", "building":
			return "rooftop"
		case "neighbourhood", "suburb":
			return "neighborhood"
		case "city", "town", "village":
			return "locality"
		case "state", "county":
			return "administrative_area"
		case "country":
			return "country"
		default:
			return "locality"
		}
	default:
		return "unknown"
	}
}
