package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/provider"
	air "github.com/universal-api/platform/schemas/airquality/v1"
	loc "github.com/universal-api/platform/schemas/location/v1"
	"math"
	"time"
)

// NormalizeAirQuality requires genuine, finite PM2.5 values per retained
// sample. Unknown pollutant fields remain absent, not zero.
func NormalizeAirQuality(raw *provider.RawAirQualityResponse) (*air.Response, error) {
	if raw == nil || raw.ProviderID != "open_meteo_air_quality" || !json.Valid(raw.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var doc struct {
		Latitude    *float64          `json:"latitude"`
		Longitude   *float64          `json:"longitude"`
		Error       bool              `json:"error"`
		HourlyUnits map[string]string `json:"hourly_units"`
		Hourly      *struct {
			Time  []int64    `json:"time"`
			PM25  []*float64 `json:"pm2_5"`
			PM10  []*float64 `json:"pm10"`
			Ozone []*float64 `json:"ozone"`
			NO2   []*float64 `json:"nitrogen_dioxide"`
			SO2   []*float64 `json:"sulphur_dioxide"`
			CO    []*float64 `json:"carbon_monoxide"`
		} `json:"hourly"`
	}
	if err := json.Unmarshal(raw.RawJSON, &doc); err != nil || doc.Error || doc.Latitude == nil || doc.Longitude == nil || doc.Hourly == nil {
		return nil, provider.ErrMalformedResponse
	}
	if err := ValidateLocation(&loc.Location{Latitude: *doc.Latitude, Longitude: *doc.Longitude}); err != nil {
		return nil, provider.ErrMalformedResponse
	}
	// The API advertises micrograms/m³ for these pollutants. Never silently
	// reinterpret an upstream unit change as the canonical unit.
	for _, name := range []string{"pm2_5", "pm10", "ozone", "nitrogen_dioxide", "sulphur_dioxide", "carbon_monoxide"} {
		if unit, ok := doc.HourlyUnits[name]; ok && unit != "μg/m³" && unit != "µg/m³" {
			return nil, provider.ErrMalformedResponse
		}
	}
	h := doc.Hourly
	n := len(h.Time)
	if n == 0 || len(h.PM25) != n {
		return nil, provider.ErrMalformedResponse
	}
	for _, list := range [][]*float64{h.PM10, h.Ozone, h.NO2, h.SO2, h.CO} {
		if list != nil && len(list) != n {
			return nil, provider.ErrMalformedResponse
		}
	}
	out := &air.Response{SchemaVersion: "air_quality.v1", DataKind: "model_forecast", ModelSource: "CAMS", Location: loc.Location{Latitude: *doc.Latitude, Longitude: *doc.Longitude}, Points: make([]air.Point, 0, 24)}
	now := time.Now().UTC()
	toConcentration := func(value *float64) (*air.Concentration, error) {
		if value == nil {
			return nil, nil
		}
		if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100000 {
			return nil, errors.New("invalid concentration")
		}
		return &air.Concentration{Value: *value, Unit: "µg/m³"}, nil
	}
	for i, epoch := range h.Time {
		t := time.Unix(epoch, 0).UTC()
		if t.Before(now.Add(-time.Hour)) {
			continue
		}
		if t.After(now.Add(4 * 24 * time.Hour)) {
			return nil, provider.ErrMalformedResponse
		}
		if h.PM25[i] == nil {
			return nil, provider.ErrMalformedResponse
		}
		p := air.Point{ValidAt: t}
		values := []struct {
			raw    *float64
			target **air.Concentration
		}{{h.PM25[i], &p.PM25}}
		for _, f := range []struct {
			list   []*float64
			target **air.Concentration
		}{{h.PM10, &p.PM10}, {h.Ozone, &p.Ozone}, {h.NO2, &p.NitrogenDioxide}, {h.SO2, &p.SulphurDioxide}, {h.CO, &p.CarbonMonoxide}} {
			if f.list != nil {
				values = append(values, struct {
					raw    *float64
					target **air.Concentration
				}{f.list[i], f.target})
			}
		}
		for _, v := range values {
			c, e := toConcentration(v.raw)
			if e != nil {
				return nil, provider.ErrMalformedResponse
			}
			*v.target = c
		}
		if len(out.Points) > 0 && !t.After(out.Points[len(out.Points)-1].ValidAt) {
			return nil, provider.ErrMalformedResponse
		}
		out.Points = append(out.Points, p)
		if len(out.Points) == 24 {
			break
		}
	}
	if len(out.Points) == 0 {
		return nil, fmt.Errorf("%w: no usable air-quality forecast points", provider.ErrMalformedResponse)
	}
	return out, nil
}
