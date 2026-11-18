package capability

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/universal-api/platform/internal/routing/decision"
)

// These limits apply to the decoded query, before any provider selection or network I/O.
const maxQueryBytes = 4096
const maxPlaceChars = 500

type weatherRequest struct {
	location            string
	latitude, longitude *float64
	provider            string
	policy              decision.RoutingPolicyMode
}

type geocodeRequest struct {
	query               string
	latitude, longitude float64
	forward             bool
	country, provider   string
	limit               int
	policy              decision.RoutingPolicyMode
}

func validateQueryShape(raw string, vals url.Values, allowed ...string) error {
	if len(raw) > maxQueryBytes {
		return fmt.Errorf("query string exceeds %d bytes", maxQueryBytes)
	}
	for k, v := range vals {
		permitted := false
		for _, name := range allowed {
			if name == k {
				permitted = true
				break
			}
		}
		if !permitted {
			return fmt.Errorf("unsupported query parameter %q", k)
		}
		if len(v) != 1 {
			return fmt.Errorf("query parameter %q must appear exactly once", k)
		}
		if len(v[0]) > maxQueryBytes {
			return fmt.Errorf("query parameter %q is too long", k)
		}
	}
	return nil
}

// oneOf returns an error if aliases are used together instead of silently selecting one.
func oneOf(vals url.Values, aliases ...string) (string, bool, error) {
	var selected string
	var present bool
	for _, key := range aliases {
		if parts, found := vals[key]; found {
			if present {
				return "", false, fmt.Errorf("conflicting aliases for %q", aliases[0])
			}
			present = true
			selected = strings.TrimSpace(parts[0])
			if selected == "" {
				return "", false, fmt.Errorf("query parameter %q cannot be empty", key)
			}
		}
	}
	return selected, present, nil
}

func parsePolicy(vals url.Values) (decision.RoutingPolicyMode, error) {
	raw, ok, err := oneOf(vals, "policy")
	if err != nil {
		return "", err
	}
	if !ok {
		return decision.PolicyAvailabilityFirst, nil
	}
	mode := decision.RoutingPolicyMode(raw)
	switch mode {
	case decision.PolicyAvailabilityFirst, decision.PolicyLatencyFirst, decision.PolicyCostFirst, decision.PolicyPriorityFirst:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported routing policy")
	}
}

func parseProvider(vals url.Values) (string, error) {
	raw, _, err := oneOf(vals, "provider")
	if err != nil {
		return "", err
	}
	if len(raw) > 64 {
		return "", fmt.Errorf("provider identifier is too long")
	}
	for _, r := range raw {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return "", fmt.Errorf("provider identifier contains unsupported characters")
		}
	}
	return raw, nil
}

func parseCoordinates(latText, lonText string) (float64, float64, error) {
	lat, e1 := strconv.ParseFloat(strings.TrimSpace(latText), 64)
	lon, e2 := strconv.ParseFloat(strings.TrimSpace(lonText), 64)
	if e1 != nil || e2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("latitude must be finite within [-90,90] and longitude finite within [-180,180]")
	}
	return lat, lon, nil
}

func parseWeatherRequest(u *url.URL) (weatherRequest, error) {
	vals, queryErr := url.ParseQuery(u.RawQuery)
	var out weatherRequest
	if queryErr != nil {
		return out, fmt.Errorf("malformed query encoding")
	}
	if err := validateQueryShape(u.RawQuery, vals, "location", "city", "q", "lat", "latitude", "lon", "longitude", "provider", "policy"); err != nil {
		return out, err
	}
	location, hasLocation, err := oneOf(vals, "location", "city", "q")
	if err != nil {
		return out, err
	}
	latText, hasLat, err := oneOf(vals, "lat", "latitude")
	if err != nil {
		return out, err
	}
	lonText, hasLon, err := oneOf(vals, "lon", "longitude")
	if err != nil {
		return out, err
	}
	if hasLat != hasLon {
		return out, fmt.Errorf("latitude and longitude must be provided together")
	}
	if hasLocation && hasLat {
		return out, fmt.Errorf("use either place text or coordinates, not both")
	}
	if !hasLocation && !hasLat {
		return out, fmt.Errorf("location or latitude and longitude are required")
	}
	if hasLocation {
		if !utf8.ValidString(location) {
			return out, fmt.Errorf("location must be UTF-8")
		}
		if len([]rune(location)) > maxPlaceChars {
			return out, fmt.Errorf("location exceeds %d characters", maxPlaceChars)
		}
		for _, c := range location {
			if unicode.IsControl(c) {
				return out, fmt.Errorf("location contains control characters")
			}
		}
		out.location = location
		// Treat two numeric-looking components as coordinates; never let malformed numeric
		// coordinate text bypass validation by being passed through as a place string.
		parts := strings.Split(location, ",")
		if len(parts) == 2 && looksNumeric(parts[0]) && looksNumeric(parts[1]) {
			lat, lon, err := parseCoordinates(parts[0], parts[1])
			if err != nil {
				return out, err
			}
			out.latitude, out.longitude = &lat, &lon
			out.location = fmt.Sprintf("%.6f,%.6f", lat, lon)
		}
	} else {
		lat, lon, err := parseCoordinates(latText, lonText)
		if err != nil {
			return out, err
		}
		out.latitude, out.longitude = &lat, &lon
		out.location = fmt.Sprintf("%.6f,%.6f", lat, lon)
	}
	out.provider, err = parseProvider(vals)
	if err != nil {
		return out, err
	}
	out.policy, err = parsePolicy(vals)
	return out, err
}

func looksNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	// Catch malformed coordinate-like tokens such as 12abc, +91foo and -200x.
	return (s[0] >= '0' && s[0] <= '9') || s[0] == '+' || s[0] == '-'
}

func parseGeocodeRequest(u *url.URL) (geocodeRequest, error) {
	vals, queryErr := url.ParseQuery(u.RawQuery)
	out := geocodeRequest{limit: 10}
	if queryErr != nil {
		return out, fmt.Errorf("malformed query encoding")
	}
	if err := validateQueryShape(u.RawQuery, vals, "q", "address", "lat", "latitude", "lon", "longitude", "provider", "policy", "country", "limit"); err != nil {
		return out, err
	}
	query, hasQuery, err := oneOf(vals, "q", "address")
	if err != nil {
		return out, err
	}
	latText, hasLat, err := oneOf(vals, "lat", "latitude")
	if err != nil {
		return out, err
	}
	lonText, hasLon, err := oneOf(vals, "lon", "longitude")
	if err != nil {
		return out, err
	}
	if hasLat != hasLon {
		return out, fmt.Errorf("latitude and longitude must be provided together")
	}
	if hasQuery && hasLat {
		return out, fmt.Errorf("use either query text or coordinates")
	}
	if !hasQuery && !hasLat {
		return out, fmt.Errorf("query text or coordinate pair required")
	}
	if hasQuery {
		if !utf8.ValidString(query) {
			return out, fmt.Errorf("query must be UTF-8")
		}
		if len([]rune(query)) > maxPlaceChars {
			return out, fmt.Errorf("query exceeds %d characters", maxPlaceChars)
		}
		for _, c := range query {
			if unicode.IsControl(c) {
				return out, fmt.Errorf("query contains control characters")
			}
		}
		out.query, out.forward = query, true
	} else {
		out.latitude, out.longitude, err = parseCoordinates(latText, lonText)
		if err != nil {
			return out, err
		}
	}
	country, hasCountry, err := oneOf(vals, "country")
	if err != nil {
		return out, err
	}
	if hasCountry {
		country = strings.ToUpper(country)
		if !out.forward {
			return out, fmt.Errorf("country is only valid for forward geocoding")
		}
		if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
			return out, fmt.Errorf("country must be an ISO 3166-1 alpha-2 code")
		}
	}
	out.country = country
	limitText, hasLimit, err := oneOf(vals, "limit")
	if err != nil {
		return out, err
	}
	if hasLimit {
		if !out.forward {
			return out, fmt.Errorf("limit is only valid for forward geocoding")
		}
		out.limit, err = strconv.Atoi(limitText)
		if err != nil || out.limit < 1 || out.limit > 50 {
			return out, fmt.Errorf("limit must be an integer between 1 and 50")
		}
	}
	out.provider, err = parseProvider(vals)
	if err != nil {
		return out, err
	}
	out.policy, err = parsePolicy(vals)
	return out, err
}
