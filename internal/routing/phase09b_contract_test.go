package routing

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/routing/decision"
)

// This contract test intentionally builds the actual mux and verifies routes,
// rather than checking a separately maintained expected paths-only list.
func specPaths(t *testing.T, filename string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "docs", filename))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rx := regexp.MustCompile(`^  (/[^: ]+):\s*$`)
	paths := []string{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if match := rx.FindStringSubmatch(scan.Text()); len(match) > 1 {
			paths = append(paths, match[1])
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func TestPhase09BPublishedOpenAPIMatchesRuntimeRoutes(t *testing.T) {
	reg := provider.NewRegistry()
	eng := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}
	base := RouterDependencies{
		HealthHandler:     health.NewHandler("test"),
		WeatherHandler:    capability.NewWeatherHandler(reg, eng, normalization.NewWeatherNormalizer(), nil, nil, nil),
		GeocodingHandler:  capability.NewGeocodingHandler(reg, eng, normalization.NewGeocodingNormalizer(), nil),
		ForecastHandler:   capability.NewForecastHandler(reg, eng, nil),
		AirQualityHandler: capability.NewAirQualityHandler(reg, eng, nil),
		ExtendedHandler:   capability.NewExtendedHandler(reg, eng, nil),
		AuthMiddleware:    deny,
	}
	tests := []struct {
		name, profile string
		metrics       bool
		spec          string
	}{
		{"lean_v1", "lean_v1", false, "openapi.yaml"},
		{"experimental", "full_experimental", true, "openapi.experimental.yaml"},
	}
	all := []string{"/v1/datasets", "/v1/weather", "/v1/forecast", "/v1/forecast/daily", "/v1/geocode", "/v1/astronomy", "/v1/air-quality", "/v1/history", "/v1/marine", "/v1/alerts", "/health", "/readiness", "/liveness", "/metrics"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := base
			deps.ReleaseProfile = tc.profile
			if tc.metrics {
				deps.MetricsHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			}
			actual := NewRouter(deps)
			expected := map[string]bool{}
			for _, p := range specPaths(t, tc.spec) {
				expected[p] = true
			}
			for _, p := range all {
				rr := httptest.NewRecorder()
				actual.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
				exposed := rr.Code != http.StatusNotFound
				if exposed != expected[p] {
					t.Errorf("%s: in spec=%v, mux status=%d", p, expected[p], rr.Code)
				}
				delete(expected, p)
			}
			if len(expected) != 0 {
				t.Fatalf("undocumented/unimplemented paths in spec: %v", strings.Join(mapKeys(expected), ", "))
			}
		})
	}
}

func mapKeys(m map[string]bool) []string {
	out := []string{}
	for x := range m {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
