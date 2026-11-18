package provider

import (
	"net/http"
	"testing"
	"time"
)

func TestPhase07B_ForecastFreshTTL(t *testing.T) {
	now := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		header    http.Header
		want      time.Duration
		specified bool
	}{
		{http.Header{}, 0, false},
		{http.Header{"Cache-Control": []string{"public, max-age=120"}, "Age": []string{"20"}}, 100 * time.Second, true},
		{http.Header{"Cache-Control": []string{"no-store, max-age=500"}}, 0, true},
		{http.Header{"Cache-Control": []string{"private"}}, 0, true},
		{http.Header{"Vary": []string{"*"}}, 0, true},
		{http.Header{"Expires": []string{now.Add(45 * time.Second).Format(http.TimeFormat)}}, 45 * time.Second, true},
	}
	for _, tc := range cases {
		ttl := ForecastFreshTTL(tc.header, now)
		if (ttl != nil) != tc.specified || (tc.specified && *ttl != tc.want) {
			t.Errorf("headers %v ttl=%v", tc.header, ttl)
		}
	}
}
