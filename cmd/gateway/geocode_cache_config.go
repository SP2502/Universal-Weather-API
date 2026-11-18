package main

import (
	"errors"
	"fmt"
	"strings"
)

// parseGeocodeCacheAllowlist never silently activates caching for a newly
// discovered provider. Operators must review upstream licence and TTL terms.
func parseGeocodeCacheAllowlist(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 256 {
		return nil, errors.New("geocode cache allowlist too long")
	}
	known := map[string]bool{"geoapify": true, "locationiq": true, "open_meteo_geocoding": true}
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		id := strings.TrimSpace(item)
		if !known[id] || seen[id] {
			return nil, fmt.Errorf("unsupported or duplicate geocoding cache provider %q", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}
