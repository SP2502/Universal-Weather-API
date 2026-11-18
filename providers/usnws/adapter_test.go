package usnws

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUSNWSPointsGridAndCache(t *testing.T) {
	var pointCalls atomic.Int32
	var gridCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "@") {
			t.Errorf("missing identification")
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/points/"):
			pointCalls.Add(1)
			fmt.Fprintf(w, `{"properties":{"forecastHourly":%q}}`, server.URL+"/gridpoints/TOP/31,80/forecast/hourly")
		case r.URL.Path == "/gridpoints/TOP/31,80/forecast/hourly":
			gridCalls.Add(1)
			fmt.Fprint(w, `{"properties":{"periods":[{"startTime":"2099-01-01T00:00:00Z","temperature":40,"temperatureUnit":"F"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a := NewAdapter("UWA-test (team@example.org)", server.URL).WithHTTPClient(server.Client())
	for i := 0; i < 2; i++ {
		raw, err := a.FetchForecast(context.Background(), 39.75, -97.09)
		if err != nil || raw == nil || raw.Latitude == nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if pointCalls.Load() != 1 || gridCalls.Load() != 2 {
		t.Fatalf("point/grid calls: %d/%d", pointCalls.Load(), gridCalls.Load())
	}
}
func TestUSNWSRejectsUntrustedGridURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"properties":{"forecastHourly":"https://evil.example/gridpoints/TOP/1,1/forecast/hourly"}}`)
	}))
	defer server.Close()
	a := NewAdapter("UWA-test (team@example.org)", server.URL).WithHTTPClient(server.Client())
	if _, err := a.FetchForecast(context.Background(), 40, -100); err == nil {
		t.Fatal("accepted off-origin forecast URL")
	}
}
