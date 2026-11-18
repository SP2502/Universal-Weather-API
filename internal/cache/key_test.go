package cache

import (
	"testing"
)

func TestBuildWeatherCacheKey_Determinism(t *testing.T) {
	k1 := BuildWeatherCacheKey(WeatherKeyParams{
		Location:      "London",
		SchemaVersion: "weather.v1",
		Unit:          "celsius",
	})

	k2 := BuildWeatherCacheKey(WeatherKeyParams{
		Location:      "  london  ",
		SchemaVersion: "weather.v1",
		Unit:          "celsius",
	})

	k3 := BuildWeatherCacheKey(WeatherKeyParams{
		Location:      "Paris",
		SchemaVersion: "weather.v1",
		Unit:          "celsius",
	})

	if k1 != k2 {
		t.Errorf("expected k1 == k2 despite whitespace/case differences, got k1=%s, k2=%s", k1, k2)
	}

	if k1 == k3 {
		t.Errorf("expected different keys for London and Paris")
	}
}

func TestBuildWeatherCacheKey_CoordinateRounding(t *testing.T) {
	lat1 := 51.507412
	lon1 := -0.127815

	// Slightly jittered coordinates within ~10 meters
	lat2 := 51.507488
	lon2 := -0.127845

	k1 := BuildWeatherCacheKey(WeatherKeyParams{Latitude: &lat1, Longitude: &lon1})
	k2 := BuildWeatherCacheKey(WeatherKeyParams{Latitude: &lat2, Longitude: &lon2})

	if k1 != k2 {
		t.Errorf("expected 2-decimal coordinate rounding to match nearby coordinates for weather: k1=%s, k2=%s", k1, k2)
	}
}

func TestBuildGeocodeCacheKey_Determinism(t *testing.T) {
	k1 := BuildGeocodeCacheKey(GeocodeKeyParams{Query: "10 Downing Street", CountryCode: "GB"})
	k2 := BuildGeocodeCacheKey(GeocodeKeyParams{Query: "  10  downing  street  ", CountryCode: "gb"})

	if k1 != k2 {
		t.Errorf("expected equivalent forward geocoding query to match: k1=%s, k2=%s", k1, k2)
	}

	lat1 := 48.856612
	lon1 := 2.352215
	lat2 := 48.856699 // Different at 4th decimal place -> different address
	lon2 := 2.352299

	rk1 := BuildGeocodeCacheKey(GeocodeKeyParams{Latitude: &lat1, Longitude: &lon1})
	rk2 := BuildGeocodeCacheKey(GeocodeKeyParams{Latitude: &lat2, Longitude: &lon2})

	if rk1 == rk2 {
		t.Errorf("expected high-precision reverse geocode to produce distinct keys: rk1=%s, rk2=%s", rk1, rk2)
	}
}

func TestWeatherCacheIdentityChangesWithRoutingConstraints(t *testing.T) {
	base := WeatherKeyParams{Location: "London", SchemaVersion: "weather.v1", Unit: "celsius"}
	want := BuildWeatherCacheKey(base)
	for _, tc := range []struct {
		name   string
		change func(*WeatherKeyParams)
	}{
		{"provider", func(p *WeatherKeyParams) { p.PreferredProvider = "openweather" }},
		{"policy", func(p *WeatherKeyParams) { p.PolicyMode = "COST_FIRST" }},
		{"schema", func(p *WeatherKeyParams) { p.SchemaVersion = "weather.v2" }},
		{"unit", func(p *WeatherKeyParams) { p.Unit = "fahrenheit" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := base
			tc.change(&changed)
			if got := BuildWeatherCacheKey(changed); got == want {
				t.Fatalf("routing or response constraint %s reused base key", tc.name)
			}
		})
	}
}
