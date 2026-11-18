package providerpolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f(x float64) *float64 { return &x }
func TestCatalogPolicyGate(t *testing.T) {
	doc := DefaultDocument()
	c, err := NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Check("openweather", Request{Product: ProductCurrentWeather, DeploymentUse: "commercial"}); !ok {
		t.Fatal("legacy current weather should remain configured")
	}
	if ok, _ := c.Check("nominatim", Request{Product: ProductForwardGeocode, DeploymentUse: "internal"}); ok {
		t.Fatal("public Nominatim must not be auto-enabled")
	}
	if ok, _ := c.Check("met_norway", Request{Product: ProductCurrentWeather, DeploymentUse: "commercial"}); ok {
		t.Fatal("unregistered research candidate must be excluded")
	}
	doc.Rules = append(doc.Rules, Rule{ProviderID: "candidate", Enabled: true, ReviewStatus: "research_only", Products: []string{ProductCurrentWeather}, DeploymentUses: []string{"any"}, Coverage: Coverage{Kind: "global"}})
	if _, err := NewCatalog(doc); err == nil {
		t.Fatal("unreviewed provider enabled")
	}
	doc.Rules[len(doc.Rules)-1].ReviewStatus = "operator_approved"
	doc.Rules[len(doc.Rules)-1].DeploymentUses = []string{"noncommercial"}
	c, err = NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Check("candidate", Request{Product: ProductCurrentWeather, DeploymentUse: "commercial"}); ok {
		t.Fatal("noncommercial candidate routed commercially")
	}
	if ok, _ := c.Check("candidate", Request{Product: ProductReverseGeocode, DeploymentUse: "noncommercial"}); ok {
		t.Fatal("unsupported operation routed")
	}
	if ok, _ := c.Check("candidate", Request{Product: ProductCurrentWeather, DeploymentUse: "noncommercial"}); !ok {
		t.Fatal("approved noncommercial deployment excluded")
	}
}
func TestCoverageModesAndUnknownLocation(t *testing.T) {
	cases := []struct {
		name     string
		c        Coverage
		country  string
		lat, lon *float64
		want     bool
	}{
		{"global unknown", Coverage{Kind: "global"}, "", nil, nil, true},
		{"country match", Coverage{Kind: "countries", Countries: []string{"US"}}, "US", nil, nil, true},
		{"country unknown", Coverage{Kind: "countries", Countries: []string{"US"}}, "", f(40), f(-75), false},
		{"bbox india", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: 20, MaxLatitude: 22, MinLongitude: 80, MaxLongitude: 85}}}, "", f(21), f(82), true},
		{"bbox outside", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: 20, MaxLatitude: 22, MinLongitude: 80, MaxLongitude: 85}}}, "", f(35), f(82), false},
		{"bbox no coordinates", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: 20, MaxLatitude: 22, MinLongitude: 80, MaxLongitude: 85}}}, "", nil, nil, false},
		{"antimeridian west", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: -10, MaxLatitude: 10, MinLongitude: 170, MaxLongitude: -170}}}, "", f(0), f(175), true},
		{"antimeridian east", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: -10, MaxLatitude: 10, MinLongitude: 170, MaxLongitude: -170}}}, "", f(0), f(-175), true},
		{"antimeridian outside", Coverage{Kind: "boxes", Boxes: []Box{{MinLatitude: -10, MaxLatitude: 10, MinLongitude: 170, MaxLongitude: -170}}}, "", f(0), f(0), false},
		{"station near", Coverage{Kind: "stations", Stations: []Station{{Latitude: 21, Longitude: 82}}, MaxDistanceKM: 30}, "", f(21.1), f(82), true},
		{"station far", Coverage{Kind: "stations", Stations: []Station{{Latitude: 21, Longitude: 82}}, MaxDistanceKM: 30}, "", f(23), f(82), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); err != nil {
				t.Fatal(err)
			}
			got := tc.c.Covers(tc.country, tc.lat, tc.lon)
			if got != tc.want {
				t.Fatalf("got %t expected %t", got, tc.want)
			}
		})
	}
}
func TestPolygonHolesAndBoundary(t *testing.T) {
	outer := []Point{{0, 0}, {0, 10}, {10, 10}, {10, 0}}
	hole := []Point{{4, 4}, {4, 6}, {6, 6}, {6, 4}}
	c := Coverage{Kind: "polygons", Polygons: []Polygon{{Outer: outer, Holes: [][]Point{hole}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		lat, lon float64
		want     bool
	}{{2, 2, true}, {5, 5, false}, {15, 5, false}, {0, 2, true}} {
		if got := c.Covers("", f(tc.lat), f(tc.lon)); got != tc.want {
			t.Fatalf("%v got %v", tc, got)
		}
	}
	bad := Coverage{Kind: "polygons", Polygons: []Polygon{{Outer: []Point{{0, 175}, {1, -175}, {2, -170}}}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("unsplit antimeridian polygon accepted")
	}
}
func TestCountryIndexUnknownAndOverlap(t *testing.T) {
	a := CountryShape{CountryCode: "AA", Coverage: Coverage{Kind: "polygons", Polygons: []Polygon{{Outer: []Point{{0, 0}, {0, 10}, {10, 10}, {10, 0}}}}}}
	b := CountryShape{CountryCode: "BB", Coverage: Coverage{Kind: "polygons", Polygons: []Polygon{{Outer: []Point{{5, 5}, {5, 15}, {15, 15}, {15, 5}}}}}}
	idx := CountryIndex{Version: 1, Countries: []CountryShape{a, b}}
	if err := idx.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := idx.Lookup(2, 2); got != "AA" {
		t.Fatal(got)
	}
	if got := idx.Lookup(7, 7); got != "" {
		t.Fatalf("overlap must be unknown, got %q", got)
	}
	if got := idx.Lookup(-20, 20); got != "" {
		t.Fatalf("ocean/unknown must be empty, got %q", got)
	}
}
func TestCatalogJSONStrict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	bytes, err := json.Marshal(DefaultDocument())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(bytes, []byte(` {"extra":true}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("trailing document accepted")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(bytes), `"enabled"`, `"unknown"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("unknown policy field accepted")
	}
}
