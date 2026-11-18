package capability

import (
	"net/url"
	"testing"
)

func BenchmarkPhase08CQueryShape(b *testing.B) {
	vals := url.Values{"lat": {"21.25"}, "lon": {"81.63"}, "provider": {"openweather"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := validateQueryShape("lat=21.25&lon=81.63&provider=openweather", vals, "location", "city", "q", "lat", "latitude", "lon", "longitude", "provider", "policy"); err != nil {
			b.Fatal(err)
		}
	}
}
