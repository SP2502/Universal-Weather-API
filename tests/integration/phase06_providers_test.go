package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/capability"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/providerpolicy"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/providers/geoapify"
	"github.com/universal-api/platform/providers/metnorway"
	"github.com/universal-api/platform/providers/openmeteo"
	"github.com/universal-api/platform/providers/openmeteogeocode"
)

func TestGeoapifyForwardReverseAndFallbackValidation(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.URL.Query().Get("apiKey"); got != "fake-geo-key" {
			t.Errorf("missing Geoapify key")
		}
		if r.URL.Path == "/search" {
			if r.URL.Query().Get("text") != "Raipur" || r.URL.Query().Get("filter") != "countrycode:in" {
				t.Errorf("forward query incorrect: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"results":[{"lat":21.25,"lon":81.63,"formatted":"Raipur, India","city":"Raipur","country":"India","country_code":"in","result_type":"city","rank":{"confidence":0.95}}]}`)
		} else if r.URL.Path == "/reverse" {
			fmt.Fprint(w, `{"results":[{"lat":21.25,"lon":81.63,"formatted":"Raipur, India","city":"Raipur","country_code":"in"}]}`)
		} else {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	a := geoapify.NewAdapter("fake-geo-key", upstream.URL).WithHTTPClient(upstream.Client())
	norm := normalization.NewGeocodingNormalizer()
	forward, err := a.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Raipur", CountryCode: "IN", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	converted, err := norm.Normalize(forward)
	if err != nil {
		t.Fatal(err)
	}
	if len(converted.Results) != 1 || converted.Results[0].CountryCode != "IN" || converted.Results[0].Latitude != 21.25 {
		t.Fatalf("unexpected canonical result %+v", converted)
	}
	reverse, err := a.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 21.25, Longitude: 81.63})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := norm.Normalize(reverse); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}
func TestGeoapifyInvalidCoordinateAndNoResult(t *testing.T) {
	n := normalization.NewGeocodingNormalizer()
	for _, data := range []string{`{"results":[{"lon":81.6,"formatted":"missing latitude"}]}`, `{"results":[{"lat":999,"lon":80,"formatted":"invalid latitude"}]}`, `{"results":[{"lat":null,"lon":80,"formatted":"null latitude"}]}`, `{"results":null}`} {
		_, err := n.Normalize(&provider.RawGeocodeResponse{ProviderID: "geoapify", Mode: "forward", RawPayload: []byte(data)})
		if err == nil {
			t.Fatalf("expected error for %s", data)
		}
	}
	got, err := n.Normalize(&provider.RawGeocodeResponse{ProviderID: "geoapify", Mode: "forward", RawPayload: []byte(`{"results":[]}`)})
	if err != nil || got.Results == nil || len(got.Results) != 0 {
		t.Fatalf("expected []: %+v, %v", got, err)
	}
}
func TestOpenMeteoGeocodingNoReverse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("countryCode") != "IN" || r.URL.Query().Get("name") != "Raipur" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"results":[{"name":"Raipur","latitude":21.25,"longitude":81.63,"country_code":"IN","country":"India","admin1":"Chhattisgarh"}]}`)
	}))
	defer up.Close()
	a := openmeteogeocode.NewAdapter(up.URL).WithHTTPClient(up.Client())
	raw, err := a.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Raipur", CountryCode: "IN", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	canon, err := normalization.NewGeocodingNormalizer().Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(canon.Results) != 1 || canon.Results[0].State != "Chhattisgarh" {
		t.Fatal(canon)
	}
	if _, err := a.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 0, Longitude: 0}); err == nil {
		t.Fatal("reverse must not be advertised as available")
	}
}
func metJSON(temperature string) string {
	updated := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	valid := time.Now().UTC().Add(70 * time.Minute).Truncate(time.Hour).Format(time.RFC3339)
	return fmt.Sprintf(`{"geometry":{"coordinates":[81.63,21.25,300]},"properties":{"meta":{"updated_at":%q},"timeseries":[{"time":%q,"data":{"instant":{"details":{"air_temperature":%s,"relative_humidity":60}},"next_1_hours":{"summary":{"symbol_code":"partlycloudy_day"}}}}]}}`, updated, valid, temperature)
}
func openMeteoJSON(temperature string) string {
	valid := time.Now().UTC().Add(70 * time.Minute).Truncate(time.Hour).Unix()
	return fmt.Sprintf(`{"latitude":21.25,"longitude":81.63,"hourly":{"time":[%d],"temperature_2m":[%s],"relative_humidity_2m":[65],"weather_code":[3]}}`, valid, temperature)
}
func TestMETForecastCacheAnd304(t *testing.T) {
	var calls atomic.Int32
	var conditional atomic.Bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("User-Agent") != "UWA/1.0 support@example.org" {
			t.Error("expected identifying user-agent")
		}
		if r.Header.Get("If-Modified-Since") != "" {
			conditional.Store(true)
			w.Header().Set("Expires", time.Now().Add(10*time.Minute).UTC().Format(http.TimeFormat))
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Expires", time.Now().Add(time.Second).UTC().Format(http.TimeFormat))
		w.Header().Set("Last-Modified", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
		fmt.Fprint(w, metJSON("23.5"))
	}))
	defer up.Close()
	a := metnorway.NewAdapter("UWA/1.0 support@example.org", up.URL).WithHTTPClient(up.Client())
	n := normalization.NewForecastNormalizer()
	raw, err := a.FetchForecast(context.Background(), 21.25, 81.63)
	if err != nil {
		t.Fatal(err)
	}
	out, err := n.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.DataKind != "model_forecast" || out.Points[0].Condition != "partly_cloudy" {
		t.Fatal(out)
	}
	_, err = a.FetchForecast(context.Background(), 21.25, 81.63)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cached response, calls=%d", calls.Load())
	}
	time.Sleep(1100 * time.Millisecond)
	third, err := a.FetchForecast(context.Background(), 21.25, 81.63)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Normalize(third); err != nil {
		t.Fatal(err)
	}
	if !conditional.Load() || calls.Load() != 2 {
		t.Fatalf("conditional request missing: %v, calls=%d", conditional.Load(), calls.Load())
	}
}
func TestForecastHandlerFallbackAndContract(t *testing.T) {
	var metCalls, omCalls atomic.Int32
	metUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { metCalls.Add(1); fmt.Fprint(w, metJSON("null")) }))
	defer metUp.Close()
	omUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		omCalls.Add(1)
		if r.URL.Query().Get("timeformat") != "unixtime" {
			t.Error("timeformat")
		}
		fmt.Fprint(w, openMeteoJSON("25.0"))
	}))
	defer omUp.Close()
	met := metnorway.NewAdapter("UWA/1.0 support@example.org", metUp.URL).WithHTTPClient(metUp.Client())
	om := openmeteo.NewAdapter(omUp.URL).WithHTTPClient(omUp.Client())
	reg := provider.NewRegistry()
	reg.Register(met)
	reg.Register(om)
	policy, err := providerpolicy.NewCatalog(providerpolicy.Document{Version: 1, Rules: []providerpolicy.Rule{
		{ProviderID: "met_norway", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"hourly_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
		{ProviderID: "open_meteo_forecast", Enabled: true, ReviewStatus: "operator_approved", Products: []string{"hourly_forecast"}, DeploymentUses: []string{"noncommercial"}, Coverage: providerpolicy.Coverage{Kind: "global"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.NewDecisionEngine(reg, reg.HealthEvaluator())
	engine.SetPolicyCatalog(policy, "noncommercial")
	handler := capability.NewForecastHandler(reg, engine, nil)
	response := httptest.NewRecorder()
	handler.GetForecast(response, httptest.NewRequest("GET", "/v1/forecast?lat=21.25&lon=81.63&policy=PRIORITY_FIRST", nil))
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var parsed struct {
		Data struct {
			DataKind string `json:"data_kind"`
			Points   []struct {
				ValidAt string `json:"valid_at"`
			} `json:"points"`
		} `json:"data"`
		Meta struct {
			Provider string `json:"provider"`
			Fallback bool   `json:"fallback"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data.DataKind != "model_forecast" || !parsed.Meta.Fallback || parsed.Meta.Provider != "open_meteo_forecast" || len(parsed.Data.Points) != 1 {
		t.Fatalf("incorrect fallback metadata %s", response.Body.String())
	}
	if metCalls.Load() == 0 || omCalls.Load() == 0 {
		t.Fatalf("both providers must be attempted: %d, %d", metCalls.Load(), omCalls.Load())
	}
	bad := httptest.NewRecorder()
	handler.GetForecast(bad, httptest.NewRequest("GET", "/v1/forecast?location=Raipur", nil))
	if bad.Code != 400 {
		t.Fatalf("text forecast should be rejected: %d", bad.Code)
	}
}
func TestResearchCandidatesDisabledByDefault(t *testing.T) {
	policy, err := providerpolicy.NewCatalog(providerpolicy.DefaultDocument())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geoapify", "met_norway", "open_meteo_forecast", "open_meteo_geocoding"} {
		if policy.PotentiallyEligible(name, "hourly_forecast", "noncommercial") {
			t.Fatalf("unexpectedly enabled provider %s", name)
		}
		if r, ok := policy.Rule(name); !ok || r.Enabled {
			t.Fatalf("missing disabled candidate %s", name)
		}
	}
	// Ensure our two forecast sources are not returned as observed weather.
	reg := provider.NewRegistry()
	reg.Register(metnorway.NewAdapter("UWA/1.0 support@example.org", ""))
	reg.Register(openmeteo.NewAdapter(""))
	if list, _ := reg.GetWeatherProviders(context.Background()); len(list) != 0 {
		t.Fatal("forecast adapters must not be current-weather providers")
	}
}
func TestSecretFreeTransportErrors(t *testing.T) {
	a := geoapify.NewAdapter("TOP_SECRET_DO_NOT_LOG", "https://invalid.example/v1/geocode")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.ForwardGeocode(ctx, provider.ForwardGeocodeRequest{Query: "X"})
	if err == nil || strings.Contains(err.Error(), "TOP_SECRET_DO_NOT_LOG") {
		t.Fatalf("unsafe error: %v", err)
	}
}
