package capability

import (
	"net/url"
	"testing"
)

func TestForecastPointLimitsAndQueryValidation(t *testing.T) {
	tests := []struct {
		raw  string
		want int
		bad  bool
	}{
		{"lat=21.25&lon=81.63", 24, false},
		{"lat=21.25&lon=81.63&points=168", 168, false},
		{"lat=21.25&lon=81.63&points=1", 1, false},
		{"lat=21.25&lon=81.63&points=0", 0, true},
		{"lat=21.25&lon=81.63&points=169", 0, true},
		{"lat=21.25&lon=81.63&points=1.5", 0, true},
		{"lat=21.25&lon=81.63&points=2&points=3", 0, true},
		{"lat=NaN&lon=81.63&points=2", 0, true},
		{"lat=21.25&lon=81.63&hours=48", 0, true},
		{"lat=21.25&lon=81.63&points=%ZZ", 0, true},
	}
	for _, tc := range tests {
		u := &url.URL{RawQuery: tc.raw}
		_, points, err := parseForecastRequest(u)
		if (err != nil) != tc.bad || (!tc.bad && points != tc.want) {
			t.Errorf("%s: points=%d err=%v", tc.raw, points, err)
		}
	}
}
