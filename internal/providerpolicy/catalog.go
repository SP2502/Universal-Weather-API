// Package providerpolicy owns conservative, operator-reviewed eligibility rules.
// Research candidates are not runtime providers until explicitly approved.
package providerpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

const (
	ProductCurrentWeather     = "current_weather"
	ProductHourlyForecast     = "hourly_forecast"
	ProductAirQualityForecast = "air_quality_forecast"
	ProductForwardGeocode     = "forward_geocoding"
	ProductReverseGeocode     = "reverse_geocoding"
	ProductHistory            = "historical_reanalysis"
	ProductAstronomy          = "astronomy"
	ProductMarine             = "marine_forecast"
	ProductAlerts             = "active_alerts"
)

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
type Box struct {
	MinLatitude  float64 `json:"min_latitude"`
	MaxLatitude  float64 `json:"max_latitude"`
	MinLongitude float64 `json:"min_longitude"`
	MaxLongitude float64 `json:"max_longitude"`
}
type Polygon struct {
	Outer []Point   `json:"outer"`
	Holes [][]Point `json:"holes,omitempty"`
}
type Station struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
type Coverage struct {
	Kind          string    `json:"kind"` // global, countries, boxes, polygons, stations
	Countries     []string  `json:"countries,omitempty"`
	Boxes         []Box     `json:"boxes,omitempty"`
	Polygons      []Polygon `json:"polygons,omitempty"`
	Stations      []Station `json:"stations,omitempty"`
	MaxDistanceKM float64   `json:"max_distance_km,omitempty"`
}
type Rule struct {
	ProviderID     string   `json:"provider_id"`
	Enabled        bool     `json:"enabled"`
	ReviewStatus   string   `json:"review_status"` // legacy, operator_approved, research_only
	Products       []string `json:"products"`
	DeploymentUses []string `json:"deployment_uses"` // any, internal, noncommercial, commercial
	Coverage       Coverage `json:"coverage"`
	SourceURL      string   `json:"source_url,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}
type Document struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}
type Catalog struct{ rules map[string]Rule }

type Request struct {
	Product, DeploymentUse, CountryCode string
	Latitude, Longitude                 *float64
}

func DefaultDocument() Document {
	// Legacy does NOT mean that licensing has been independently certified.
	// These records preserve existing configured adapters' functionality while
	// new integrations must be explicitly reviewed and enabled.
	return Document{Version: 1, Rules: []Rule{
		{ProviderID: "openweather", Enabled: true, ReviewStatus: "legacy", Products: []string{ProductCurrentWeather}, DeploymentUses: []string{"any"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://openweathermap.org/price", Notes: "Existing BYOK adapter; review operator terms and caching permissions."},
		{ProviderID: "weatherapi", Enabled: true, ReviewStatus: "legacy", Products: []string{ProductCurrentWeather}, DeploymentUses: []string{"any"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://www.weatherapi.com/pricing.aspx", Notes: "Existing BYOK adapter; review operator terms and caching permissions."},
		{ProviderID: "locationiq", Enabled: true, ReviewStatus: "legacy", Products: []string{ProductForwardGeocode, ProductReverseGeocode}, DeploymentUses: []string{"any"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://locationiq.com/pricing", Notes: "Existing BYOK adapter; operator must obey actual plan terms."},
		{ProviderID: "geoapify", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductForwardGeocode, ProductReverseGeocode}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://apidocs.geoapify.com/docs/geocoding/forward-geocoding/", Notes: "Disabled until operator enables and verifies BYOK entitlement and attribution."},
		{ProviderID: "met_norway", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductHourlyForecast}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://api.met.no/weatherapi/locationforecast/2.0/documentation", Notes: "Model forecasts only. Requires identification, source attribution and HTTP cache revalidation."},
		{ProviderID: "open_meteo_forecast", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductHourlyForecast}, DeploymentUses: []string{"noncommercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://open-meteo.com/en/pricing", Notes: "Hosted free service is non-commercial only; requires explicit opt-in."},
		{ProviderID: "open_meteo_air_quality", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductAirQualityForecast}, DeploymentUses: []string{"noncommercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://open-meteo.com/en/docs/air-quality-api", Notes: "Modeled CAMS concentrations, not station observations; hosted free tier non-commercial only, quota shared with other Open-Meteo endpoints."},
		{ProviderID: "open_meteo_geocoding", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductForwardGeocode}, DeploymentUses: []string{"noncommercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://open-meteo.com/en/pricing", Notes: "Hosted free service is non-commercial only; no reverse geocoding."},
		{ProviderID: "us_nws", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductHourlyForecast}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "countries", Countries: []string{"US"}}, SourceURL: "https://www.weather.gov/documentation/services-web-api", Notes: "NWS US regional forecast only. Requires verified country polygon index and identifying user agent."},
		{ProviderID: "bright_sky", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductHourlyForecast}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "countries", Countries: []string{"DE"}}, SourceURL: "https://brightsky.dev/docs/", Notes: "German DWD forecast stations; strict 25km nearest-source limit, operator-reviewed geographic boundaries and attribution."},
		{ProviderID: "open_meteo_history", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductHistory}, DeploymentUses: []string{"noncommercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://open-meteo.com/en/docs/historical-weather-api", Notes: "ERA5 reanalysis; hosted free non-commercial only. Not station observations."},
		{ProviderID: "open_meteo_marine", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductMarine}, DeploymentUses: []string{"noncommercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://open-meteo.com/en/docs/marine-weather-api", Notes: "Marine model grid only; reject distant grid cells. Do not use inland as an observation."},
		{ProviderID: "sunrise_sunset", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductAstronomy}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://sunrise-sunset.org/api", Notes: "Calculated astronomical events; attribution required. Events may be null on polar days."},
		{ProviderID: "us_nws_alerts", Enabled: false, ReviewStatus: "operator_approved", Products: []string{ProductAlerts}, DeploymentUses: []string{"internal", "noncommercial", "commercial"}, Coverage: Coverage{Kind: "countries", Countries: []string{"US"}}, SourceURL: "https://www.weather.gov/documentation/services-web-api", Notes: "Official active alerts within verified US polygons only; a missing alert response is not a no-alert guarantee."},
		{ProviderID: "nominatim", Enabled: false, ReviewStatus: "research_only", Products: []string{ProductForwardGeocode, ProductReverseGeocode}, DeploymentUses: []string{"internal"}, Coverage: Coverage{Kind: "global"}, SourceURL: "https://operations.osmfoundation.org/policies/nominatim/", Notes: "Public service is not a general-purpose gateway backend; use explicitly approved self-hosted or permitted endpoint."},
	}}
}

func NewCatalog(doc Document) (*Catalog, error) {
	if doc.Version != 1 {
		return nil, fmt.Errorf("policy catalog: unsupported version %d", doc.Version)
	}
	if len(doc.Rules) == 0 {
		return nil, errors.New("policy catalog: at least one rule required")
	}
	c := &Catalog{rules: make(map[string]Rule, len(doc.Rules))}
	for _, r := range doc.Rules {
		if !validID(r.ProviderID) {
			return nil, fmt.Errorf("policy catalog: invalid provider ID %q", r.ProviderID)
		}
		if _, exists := c.rules[r.ProviderID]; exists {
			return nil, fmt.Errorf("policy catalog: duplicate provider %q", r.ProviderID)
		}
		if r.ReviewStatus != "legacy" && r.ReviewStatus != "operator_approved" && r.ReviewStatus != "research_only" {
			return nil, fmt.Errorf("policy catalog: %s invalid review status", r.ProviderID)
		}
		if r.Enabled && r.ReviewStatus == "research_only" {
			return nil, fmt.Errorf("policy catalog: %s cannot enable research-only rule", r.ProviderID)
		}
		if len(r.Products) == 0 || len(r.DeploymentUses) == 0 {
			return nil, fmt.Errorf("policy catalog: %s requires products and deployment uses", r.ProviderID)
		}
		seen := map[string]bool{}
		for _, p := range r.Products {
			if p != ProductCurrentWeather && p != ProductHourlyForecast && p != ProductAirQualityForecast && p != ProductForwardGeocode && p != ProductReverseGeocode && p != ProductHistory && p != ProductAstronomy && p != ProductMarine && p != ProductAlerts || seen[p] {
				return nil, fmt.Errorf("policy catalog: %s invalid/duplicate product %q", r.ProviderID, p)
			}
			seen[p] = true
		}
		seen = map[string]bool{}
		for _, mode := range r.DeploymentUses {
			if mode != "any" && mode != "internal" && mode != "noncommercial" && mode != "commercial" || seen[mode] {
				return nil, fmt.Errorf("policy catalog: %s invalid/duplicate deployment use %q", r.ProviderID, mode)
			}
			seen[mode] = true
		}
		if err := r.Coverage.Validate(); err != nil {
			return nil, fmt.Errorf("policy catalog: %s: %w", r.ProviderID, err)
		}
		c.rules[r.ProviderID] = r
	}
	return c, nil
}

func LoadFile(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Document
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("policy catalog JSON: %w", err)
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("policy catalog: invalid trailing JSON")
	}
	return NewCatalog(doc)
}

// PotentiallyEligible checks product/deployment approval without a location.
// It is only for startup/readiness, never for request admission.
func (c *Catalog) PotentiallyEligible(id, product, deploymentUse string) bool {
	if c == nil {
		return false
	}
	r, ok := c.rules[id]
	return ok && r.Enabled && (r.ReviewStatus == "legacy" || r.ReviewStatus == "operator_approved") && has(r.Products, product) && (has(r.DeploymentUses, "any") || has(r.DeploymentUses, deploymentUse))
}

func (c *Catalog) Check(providerID string, req Request) (bool, string) {
	if c == nil {
		return false, "provider policy catalog not configured"
	}
	r, ok := c.rules[providerID]
	if !ok {
		return false, "provider has no reviewed policy"
	}
	if !r.Enabled {
		return false, "provider policy disabled"
	}
	if r.ReviewStatus != "legacy" && r.ReviewStatus != "operator_approved" {
		return false, "provider policy not approved"
	}
	if req.Product == "" {
		return false, "product missing from request"
	}
	if !has(r.Products, req.Product) {
		return false, "product not supported by provider policy"
	}
	if !has(r.DeploymentUses, "any") && !has(r.DeploymentUses, req.DeploymentUse) {
		return false, "deployment use not permitted by provider policy"
	}
	if !r.Coverage.Covers(req.CountryCode, req.Latitude, req.Longitude) {
		return false, "location outside verified coverage or location unknown"
	}
	return true, ""
}
func (c *Catalog) Rule(id string) (Rule, bool) {
	if c == nil {
		return Rule{}, false
	}
	r, ok := c.rules[id]
	return r, ok
}
func has(items []string, x string) bool {
	for _, v := range items {
		if v == x {
			return true
		}
	}
	return false
}
func validID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '_' && r != '-' {
					return false
				}
			}
		}
	}
	return true
}
func validPoint(p Point) bool {
	return !math.IsNaN(p.Latitude) && !math.IsNaN(p.Longitude) && !math.IsInf(p.Latitude, 0) && !math.IsInf(p.Longitude, 0) && p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180
}
func validCountry(s string) bool {
	return len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z'
}

func (c Coverage) Validate() error {
	switch c.Kind {
	case "global":
		return nil
	case "countries":
		if len(c.Countries) == 0 {
			return errors.New("country coverage requires country codes")
		}
		for _, s := range c.Countries {
			if !validCountry(s) {
				return fmt.Errorf("invalid ISO country code %q", s)
			}
		}
	case "boxes":
		if len(c.Boxes) == 0 {
			return errors.New("box coverage requires boxes")
		}
		for _, b := range c.Boxes {
			if !validPoint(Point{b.MinLatitude, b.MinLongitude}) || !validPoint(Point{b.MaxLatitude, b.MaxLongitude}) || b.MinLatitude > b.MaxLatitude {
				return errors.New("invalid coverage box")
			}
		}
	case "polygons":
		if len(c.Polygons) == 0 {
			return errors.New("polygon coverage requires polygons")
		}
		for _, poly := range c.Polygons {
			if err := validRing(poly.Outer); err != nil {
				return err
			}
			for _, hole := range poly.Holes {
				if err := validRing(hole); err != nil {
					return err
				}
			}
		}
	case "stations":
		if len(c.Stations) == 0 || c.MaxDistanceKM <= 0 || c.MaxDistanceKM > 2000 || math.IsNaN(c.MaxDistanceKM) || math.IsInf(c.MaxDistanceKM, 0) {
			return errors.New("stations require coordinates and finite max distance (0,2000] km")
		}
		for _, s := range c.Stations {
			if !validPoint(Point(s)) {
				return errors.New("invalid station coordinate")
			}
		}
	default:
		return fmt.Errorf("unsupported coverage kind %q", c.Kind)
	}
	return nil
}
func validRing(r []Point) error {
	if len(r) < 3 {
		return errors.New("polygon needs at least three vertices")
	}
	for _, p := range r {
		if !validPoint(p) {
			return errors.New("invalid polygon coordinate")
		}
	}
	// Polygon ring with an antimeridian-crossing edge needs an explicit
	// geographic splitting step. Reject instead of misclassifying vast areas.
	for i, p := range r {
		q := r[(i+1)%len(r)]
		if math.Abs(p.Longitude-q.Longitude) > 180 {
			return errors.New("split polygons crossing the antimeridian")
		}
	}
	return nil
}

func (c Coverage) Covers(country string, lat, lon *float64) bool {
	switch c.Kind {
	case "global":
		return true
	case "countries":
		return has(c.Countries, strings.ToUpper(country))
	}
	if lat == nil || lon == nil || !validPoint(Point{*lat, *lon}) {
		return false
	}
	p := Point{*lat, *lon}
	switch c.Kind {
	case "boxes":
		for _, b := range c.Boxes {
			// A box with west>east explicitly crosses the antimeridian.
			insideLon := p.Longitude >= b.MinLongitude && p.Longitude <= b.MaxLongitude
			if b.MinLongitude > b.MaxLongitude {
				insideLon = p.Longitude >= b.MinLongitude || p.Longitude <= b.MaxLongitude
			}
			if p.Latitude >= b.MinLatitude && p.Latitude <= b.MaxLatitude && insideLon {
				return true
			}
		}
	case "polygons":
		for _, poly := range c.Polygons {
			if insideRing(p, poly.Outer) {
				insideHole := false
				for _, hole := range poly.Holes {
					if insideRing(p, hole) {
						insideHole = true
						break
					}
				}
				if !insideHole {
					return true
				}
			}
		}
	case "stations":
		for _, s := range c.Stations {
			if haversineKM(p, Point(s)) <= c.MaxDistanceKM {
				return true
			}
		}
	}
	return false
}
func insideRing(p Point, ring []Point) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[j], ring[i]
		cross := (p.Longitude-a.Longitude)*(b.Latitude-a.Latitude) - (p.Latitude-a.Latitude)*(b.Longitude-a.Longitude)
		if math.Abs(cross) < 1e-10 && p.Longitude >= math.Min(a.Longitude, b.Longitude) && p.Longitude <= math.Max(a.Longitude, b.Longitude) && p.Latitude >= math.Min(a.Latitude, b.Latitude) && p.Latitude <= math.Max(a.Latitude, b.Latitude) {
			return true
		}
		if (a.Latitude > p.Latitude) != (b.Latitude > p.Latitude) {
			x := (b.Longitude-a.Longitude)*(p.Latitude-a.Latitude)/(b.Latitude-a.Latitude) + a.Longitude
			if p.Longitude < x {
				in = !in
			}
		}
	}
	return in
}
func haversineKM(a, b Point) float64 {
	const radius = 6371.0088
	toRad := math.Pi / 180
	lat1, lat2 := a.Latitude*toRad, b.Latitude*toRad
	dLat := (b.Latitude - a.Latitude) * toRad
	dLon := (b.Longitude - a.Longitude) * toRad
	x := math.Pow(math.Sin(dLat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dLon/2), 2)
	return 2 * radius * math.Atan2(math.Sqrt(x), math.Sqrt(math.Max(0, 1-x)))
}

// CountryIndex contains operator-sourced verified country polygons; no invented
// boundaries are bundled with the gateway. Overlap and no match return unknown.
type CountryIndex struct {
	Version   int            `json:"version"`
	Countries []CountryShape `json:"countries"`
}
type CountryShape struct {
	CountryCode string   `json:"country_code"`
	Coverage    Coverage `json:"coverage"`
}

func LoadCountryIndex(path string) (*CountryIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx CountryIndex
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return nil, err
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("country index: invalid trailing JSON")
	}
	if err := idx.Validate(); err != nil {
		return nil, err
	}
	return &idx, nil
}
func (idx *CountryIndex) Validate() error {
	if idx == nil || idx.Version != 1 || len(idx.Countries) == 0 {
		return errors.New("country index version 1 with country shapes required")
	}
	seen := map[string]bool{}
	for _, c := range idx.Countries {
		if !validCountry(c.CountryCode) || seen[c.CountryCode] {
			return fmt.Errorf("country index: invalid/duplicate country %q", c.CountryCode)
		}
		seen[c.CountryCode] = true
		if c.Coverage.Kind != "polygons" {
			return errors.New("country index requires actual polygon geometries (not bounding-box inference)")
		}
		if err := c.Coverage.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (idx *CountryIndex) Lookup(lat, lon float64) string {
	if idx == nil || !validPoint(Point{lat, lon}) {
		return ""
	}
	var match string
	for _, c := range idx.Countries {
		if c.Coverage.Covers("", &lat, &lon) {
			if match != "" {
				return ""
			}
			match = c.CountryCode
		}
	}
	return match
}
