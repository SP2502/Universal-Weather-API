package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
)

var whitespaceRegex = regexp.MustCompile(`\s+`)

// NormalizeQueryString standardizes text queries by trimming, lowercasing, and collapsing whitespace.
func NormalizeQueryString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	lowered := strings.ToLower(trimmed)
	collapsed := whitespaceRegex.ReplaceAllString(lowered, " ")
	return collapsed
}

// NormalizeCoordinate rounds a floating-point coordinate to a specific number of decimal places.
// Weather observations benefit from 2 decimals (~1.1 km), whereas street geocoding requires 4 decimals (~11.1 m).
func NormalizeCoordinate(coord float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(coord*pow) / pow
}

// WeatherKeyParams represents canonical inputs for generating a weather cache key.
type WeatherKeyParams struct {
	Location          string
	Latitude          *float64
	Longitude         *float64
	SchemaVersion     string
	Unit              string
	PreferredProvider string
	PolicyMode        string
}

// BuildWeatherCacheKey generates a deterministic, collision-resistant canonical cache key for weather.
func BuildWeatherCacheKey(params WeatherKeyParams) string {
	schemaVer := params.SchemaVersion
	if schemaVer == "" {
		schemaVer = "weather.v1"
	}
	unit := params.Unit
	if unit == "" {
		unit = "celsius"
	}

	var locationComponent string
	if params.Latitude != nil && params.Longitude != nil {
		// 2 decimal places (~1.1 km precision) for weather observation grids
		normLat := NormalizeCoordinate(*params.Latitude, 2)
		normLon := NormalizeCoordinate(*params.Longitude, 2)
		locationComponent = fmt.Sprintf("geo:%.2f,%.2f", normLat, normLon)
	} else {
		locationComponent = "loc:" + NormalizeQueryString(params.Location)
	}

	rawDescriptor := fmt.Sprintf("cap=weather|ver=%s|unit=%s|%s|provider=%s|policy=%s", schemaVer, unit, locationComponent, strings.ToLower(strings.TrimSpace(params.PreferredProvider)), strings.ToUpper(strings.TrimSpace(params.PolicyMode)))
	sum := sha256.Sum256([]byte(rawDescriptor))
	return fmt.Sprintf("uap:cache:v1:weather:%s", hex.EncodeToString(sum[:16]))
}

// GeocodeKeyParams represents canonical inputs for generating a geocoding cache key.
type GeocodeKeyParams struct {
	Query         string
	CountryCode   string
	Latitude      *float64
	Longitude     *float64
	SchemaVersion string
}

// BuildGeocodeCacheKey generates a deterministic canonical cache key for forward or reverse geocoding.
func BuildGeocodeCacheKey(params GeocodeKeyParams) string {
	schemaVer := params.SchemaVersion
	if schemaVer == "" {
		schemaVer = "location.v1"
	}

	var queryComponent string
	if params.Latitude != nil && params.Longitude != nil {
		// 4 decimal places (~11.1 meters precision) for high-accuracy reverse geocoding
		normLat := NormalizeCoordinate(*params.Latitude, 4)
		normLon := NormalizeCoordinate(*params.Longitude, 4)
		queryComponent = fmt.Sprintf("mode=reverse|geo:%.4f,%.4f", normLat, normLon)
	} else {
		normQuery := NormalizeQueryString(params.Query)
		normCountry := strings.ToLower(strings.TrimSpace(params.CountryCode))
		queryComponent = fmt.Sprintf("mode=forward|q=%s|country=%s", normQuery, normCountry)
	}

	rawDescriptor := fmt.Sprintf("cap=geocode|ver=%s|%s", schemaVer, queryComponent)
	sum := sha256.Sum256([]byte(rawDescriptor))
	return fmt.Sprintf("uap:cache:v1:geocode:%s", hex.EncodeToString(sum[:16]))
}

// ForecastKeyParams includes all request constraints that can change provider
// selection. PointLimit is deliberately NOT part of the identity: cached
// forecast data is stored untruncated and sliced per response.
type ForecastKeyParams struct {
	Latitude          float64
	Longitude         float64
	SchemaVersion     string
	Product           string
	PreferredProvider string
	PolicyMode        string
}

func BuildForecastCacheKey(p ForecastKeyParams) string {
	version := p.SchemaVersion
	if version == "" {
		version = "forecast.v1"
	}
	product := p.Product
	if product == "" {
		product = "hourly_forecast"
	}
	// Forecast adapters dispatch to four decimals: don't conflate more distant
	// coordinates (as the observation-weather cache intentionally can).
	descriptor := fmt.Sprintf("forecast|%s|%s|%.4f,%.4f|provider=%s|policy=%s", version, product,
		NormalizeCoordinate(p.Latitude, 4), NormalizeCoordinate(p.Longitude, 4),
		strings.ToLower(strings.TrimSpace(p.PreferredProvider)), strings.ToUpper(strings.TrimSpace(p.PolicyMode)))
	digest := sha256.Sum256([]byte(descriptor))
	return "uap:cache:v1:forecast:" + hex.EncodeToString(digest[:16])
}

// GeocodeV2KeyParams includes response-affecting constraints. Unlike the
// legacy generic geocode key it separates limit, preferred provider and policy.
type GeocodeV2KeyParams struct {
	Query, Country, PreferredProvider, Policy, Mode string
	Latitude, Longitude                             float64
	Limit                                           int
}

func BuildGeocodeV2Key(p GeocodeV2KeyParams) string {
	location := "q:" + NormalizeQueryString(p.Query)
	if p.Mode == "reverse" {
		location = fmt.Sprintf("geo:%.6f,%.6f", NormalizeCoordinate(p.Latitude, 6), NormalizeCoordinate(p.Longitude, 6))
	}
	descriptor := fmt.Sprintf("geo.v2|mode=%s|%s|country=%s|limit=%d|provider=%s|policy=%s", p.Mode, location, strings.ToUpper(p.Country), p.Limit, strings.ToLower(p.PreferredProvider), strings.ToUpper(p.Policy))
	sum := sha256.Sum256([]byte(descriptor))
	return fmt.Sprintf("uap:cache:v2:geocode:%s", hex.EncodeToString(sum[:16]))
}
