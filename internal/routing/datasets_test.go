package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/universal-api/platform/internal/auth"
)

func TestDatasetsStopsStartingProductsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondCalls := 0
	routes := map[string]datasetRoute{
		"weather": {scope: "weather:read", handler: func(w http.ResponseWriter, _ *http.Request) {
			cancel()
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"schema_version":"weather.v1"},"meta":{}}`))
		}},
		"forecast": {scope: "forecast:read", handler: func(w http.ResponseWriter, _ *http.Request) {
			secondCalls++
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"schema_version":"forecast.v1"},"meta":{}}`))
		}},
	}
	r := httptest.NewRequest("GET", "/v1/datasets?datasets=weather,forecast&lat=51&lon=0", nil)
	r = r.WithContext(auth.WithCaller(ctx, &auth.Caller{Scopes: []string{"weather:read", "forecast:read"}}))
	w := httptest.NewRecorder()
	datasetsHandler(routes)(w, r)
	if secondCalls != 0 {
		t.Fatalf("started %d additional products after cancellation", secondCalls)
	}
	var body struct {
		Errors map[string]datasetFailure `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 207 || body.Errors["forecast"].Error.Code != "DATASET_TIMEOUT" {
		t.Fatalf("cancellation response status=%d errors=%+v", w.Code, body.Errors)
	}
}

func TestDatasetsRejectsInvalidSharedLocationBeforeProducts(t *testing.T) {
	calls := 0
	routes := map[string]datasetRoute{"weather": {scope: "weather:read", handler: func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":{},"meta":{}}`))
	}}}
	for _, query := range []string{
		"datasets=weather&location=London&lat=51&lon=0",
		"datasets=weather&lat=51",
		"datasets=weather&lat=999&lon=0",
		"datasets=weather&location=London&q=Paris",
		"datasets=weather",
	} {
		r := httptest.NewRequest("GET", "/v1/datasets?"+query, nil)
		r = r.WithContext(auth.WithCaller(r.Context(), &auth.Caller{Scopes: []string{"weather:read"}}))
		w := httptest.NewRecorder()
		datasetsHandler(routes)(w, r)
		if w.Code != 400 || calls != 0 {
			t.Fatalf("invalid %q status=%d calls=%d", query, w.Code, calls)
		}
	}
}

func TestDatasetsCombinedPartialAndScopes(t *testing.T) {
	calls := 0
	failForecast := true
	routes := map[string]datasetRoute{
		"weather": {scope: "weather:read", handler: func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"schema_version":"weather.v1"},"meta":{"provider":"a"}}`))
		}},
		"forecast": {scope: "forecast:read", handler: func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			if !failForecast {
				w.WriteHeader(200)
				_, _ = w.Write([]byte(`{"data":{"schema_version":"forecast.v1"},"meta":{"provider":"b"}}`))
				return
			}
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":"PROVIDER_UNAVAILABLE","message":"No source","status":503}}`))
		}},
	}
	handler := datasetsHandler(routes)
	request := func(path string, scopes ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r = r.WithContext(auth.WithCaller(r.Context(), &auth.Caller{Scopes: scopes}))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	partial := request("/v1/datasets?datasets=weather,forecast&lat=51&lon=0", "weather:read", "forecast:read")
	if partial.Code != 207 || calls != 2 {
		t.Fatalf("partial response status=%d calls=%d", partial.Code, calls)
	}
	var body struct {
		Data   map[string]datasetResult  `json:"data"`
		Errors map[string]datasetFailure `json:"errors"`
		Meta   struct {
			Partial bool `json:"partial"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(partial.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Meta.Partial || len(body.Data) != 1 || len(body.Errors) != 1 || body.Errors["forecast"].Error.Code != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("incorrect partial response: %+v", body)
	}
	if string(body.Data["weather"].Data) != `{"schema_version":"weather.v1"}` {
		t.Fatalf("weather data changed: %s", body.Data["weather"].Data)
	}
	failForecast = false
	complete := request("/v1/datasets?datasets=weather,forecast&lat=51&lon=0", "weather:read", "forecast:read")
	if complete.Code != 200 {
		t.Fatalf("complete status=%d", complete.Code)
	}
	body.Data, body.Errors, body.Meta.Partial = nil, nil, false
	if err := json.Unmarshal(complete.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Meta.Partial || len(body.Data) != 2 || len(body.Errors) != 0 {
		t.Fatalf("incorrect complete response: %+v", body)
	}
	forbidden := request("/v1/datasets?datasets=weather,forecast&lat=51&lon=0", "weather:read")
	if forbidden.Code != 403 || calls != 4 {
		t.Fatalf("scope bypass status=%d calls=%d", forbidden.Code, calls)
	}
	invalid := request("/v1/datasets?datasets=weather,weather&lat=51&lon=0", "weather:read")
	if invalid.Code != 400 || calls != 4 {
		t.Fatalf("duplicate accepted status=%d calls=%d", invalid.Code, calls)
	}
}
