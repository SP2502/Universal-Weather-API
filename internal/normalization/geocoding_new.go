package normalization

import (
	"encoding/json"
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	loc "github.com/universal-api/platform/schemas/location/v1"
	"strings"
)

func normalizeGeoapify(payload []byte, mode string) (*loc.GeocodeResponse, error) {
	var raw struct {
		Results []struct {
			Latitude    *float64 `json:"lat"`
			Longitude   *float64 `json:"lon"`
			Formatted   string   `json:"formatted"`
			Name        string   `json:"name"`
			Housenumber string   `json:"housenumber"`
			Street      string   `json:"street"`
			Suburb      string   `json:"suburb"`
			City        string   `json:"city"`
			County      string   `json:"county"`
			State       string   `json:"state"`
			Country     string   `json:"country"`
			CountryCode string   `json:"country_code"`
			Postcode    string   `json:"postcode"`
			ResultType  string   `json:"result_type"`
			Rank        *struct {
				Confidence *float64 `json:"confidence"`
			} `json:"rank"`
		} `json:"results"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("%w: invalid Geoapify JSON", provider.ErrMalformedResponse)
	}
	if raw.Results == nil {
		return nil, fmt.Errorf("%w: missing results array", provider.ErrMalformedResponse)
	}
	out := &loc.GeocodeResponse{SchemaVersion: "location.v1", Results: make([]loc.Location, 0, len(raw.Results))}
	for _, item := range raw.Results {
		if item.Latitude == nil || item.Longitude == nil || strings.TrimSpace(item.Formatted) == "" {
			return nil, fmt.Errorf("%w: missing location fields", provider.ErrMalformedResponse)
		}
		l := loc.Location{Latitude: *item.Latitude, Longitude: *item.Longitude, FormattedAddress: item.Formatted, HouseNumber: item.Housenumber, Street: item.Street, Neighborhood: item.Suburb, City: item.City, District: item.County, State: item.State, Country: item.Country, CountryCode: strings.ToUpper(item.CountryCode), PostalCode: item.Postcode, ResultType: item.ResultType, Precision: geoapifyPrecision(item.ResultType)}
		if item.Rank != nil && item.Rank.Confidence != nil {
			l.Confidence = *item.Rank.Confidence
		}
		if err := ValidateLocation(&l); err != nil {
			return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
		}
		out.Results = append(out.Results, l)
	}
	if mode == "reverse" && len(out.Results) == 0 {
		return nil, provider.ErrProviderNotFound
	}
	return out, nil
}
func geoapifyPrecision(t string) string {
	switch t {
	case "building":
		return "building"
	case "street":
		return "street"
	case "suburb", "district":
		return "neighborhood"
	case "city":
		return "locality"
	case "country":
		return "country"
	case "county", "state":
		return "administrative_area"
	default:
		return "unknown"
	}
}
func normalizeOpenMeteoGeocode(payload []byte) (*loc.GeocodeResponse, error) {
	var raw struct {
		Results []struct {
			Latitude    *float64 `json:"latitude"`
			Longitude   *float64 `json:"longitude"`
			Name        string   `json:"name"`
			Country     string   `json:"country"`
			CountryCode string   `json:"country_code"`
			Admin1      string   `json:"admin1"`
			Admin2      string   `json:"admin2"`
			Timezone    string   `json:"timezone"`
		} `json:"results"`
		Error          bool     `json:"error"`
		GenerationTime *float64 `json:"generationtime_ms"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil || raw.Error {
		return nil, fmt.Errorf("%w: invalid geocoding JSON", provider.ErrMalformedResponse)
	}
	if raw.Results == nil && raw.GenerationTime == nil {
		return nil, provider.ErrMalformedResponse
	}
	out := &loc.GeocodeResponse{SchemaVersion: "location.v1", Results: make([]loc.Location, 0, len(raw.Results))}
	for _, item := range raw.Results {
		if item.Latitude == nil || item.Longitude == nil || strings.TrimSpace(item.Name) == "" {
			return nil, provider.ErrMalformedResponse
		}
		address := item.Name
		if item.Admin1 != "" {
			address += ", " + item.Admin1
		}
		if item.Country != "" {
			address += ", " + item.Country
		}
		l := loc.Location{Latitude: *item.Latitude, Longitude: *item.Longitude, FormattedAddress: address, City: item.Name, State: item.Admin1, District: item.Admin2, Country: item.Country, CountryCode: strings.ToUpper(item.CountryCode), Timezone: item.Timezone, Precision: "locality", ResultType: "place"}
		if err := ValidateLocation(&l); err != nil {
			return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
		}
		out.Results = append(out.Results, l)
	}
	return out, nil
}
