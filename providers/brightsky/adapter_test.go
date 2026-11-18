package brightsky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrightSkyQueryIsBounded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/weather" || r.URL.Query().Get("max_dist") != "12000" || r.URL.Query().Get("lat") != "52.5200" || r.URL.Query().Get("date") == "" || r.URL.Query().Get("last_date") == "" {
			t.Errorf("bad URL: %v", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		strings.NewReader(`{"weather":[],"sources":[]}`).WriteTo(w)
	}))
	defer ts.Close()
	a := NewAdapter(ts.URL, 12000).WithHTTPClient(ts.Client())
	if _, err := a.FetchForecast(context.Background(), 52.52, 13.405); err != nil {
		t.Fatal(err)
	}
}
func TestBrightSkyNoBodySizeBypass(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", 1048577))) }))
	defer ts.Close()
	a := NewAdapter(ts.URL, 25000).WithHTTPClient(ts.Client())
	if _, err := a.FetchForecast(context.Background(), 52, 13); err == nil {
		t.Fatal("oversized response accepted")
	}
}
