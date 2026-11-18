package capability

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestWeatherCacheRequiredPresencePreservesGenuineZeros(t *testing.T) {
	base := fmt.Sprintf(`{"integrity_version":1,"provider_id":"openweather","data":{"schema_version":"weather.v1","location":%s,"temperature":%s,"humidity":%s,"condition":"clear","observed_at":%q}}`, "%s", "%s", "%s", time.Now().UTC().Format(time.RFC3339))
	for _, tc := range []struct {
		name, loc, temp, humidity string
		ok                        bool
	}{{"valid-zero", `{"latitude":0,"longitude":0}`, `{"value":0,"unit":"celsius"}`, `{"value":0,"unit":"percent"}`, true}, {"missing-temp", `{"latitude":0,"longitude":0}`, `{"unit":"celsius"}`, `null`, false}, {"null-temp", `{"latitude":0,"longitude":0}`, `{"value":null,"unit":"celsius"}`, `null`, false}, {"missing-lat", `{"longitude":0}`, `{"value":0,"unit":"celsius"}`, `null`, false}, {"missing-humidity-value", `{"latitude":0,"longitude":0}`, `{"value":0,"unit":"celsius"}`, `{"unit":"percent"}`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var item CachedWeatherPayload
			e := json.Unmarshal([]byte(fmt.Sprintf(base, tc.loc, tc.temp, tc.humidity)), &item)
			if (e == nil) != tc.ok {
				t.Fatalf("presence validation=%v", e)
			}
			if tc.ok && (item.Data.Temperature.Value != 0 || item.Data.Humidity.Value != 0) {
				t.Fatal("genuine zero lost")
			}
		})
	}
}
