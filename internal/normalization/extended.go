package normalization

import (
	"encoding/json"
	"github.com/universal-api/platform/internal/provider"
	ext "github.com/universal-api/platform/schemas/extended/v1"
	"math"
	"time"
)

func validGeo(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsInf(lat, 0) && !math.IsNaN(lon) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func distanceKM(a, b, c, d float64) float64 {
	const radius = 6371.0
	rad := math.Pi / 180
	x := math.Sin((c - a) * rad / 2)
	y := math.Sin((d - b) * rad / 2)
	z := x*x + math.Cos(a*rad)*math.Cos(c*rad)*y*y
	if z > 1 {
		z = 1
	}
	return 2 * radius * math.Asin(math.Sqrt(z))
}
func real(v *float64, min, max float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= min && *v <= max
}
func NormalizeHistory(r *provider.RawExtendedResponse, start, end time.Time) (*ext.HistoryResponse, error) {
	if r == nil || r.ProviderID != "open_meteo_history" || !json.Valid(r.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var d struct {
		Latitude    *float64          `json:"latitude"`
		Longitude   *float64          `json:"longitude"`
		Error       bool              `json:"error"`
		HourlyUnits map[string]string `json:"hourly_units"`
		Hourly      *struct {
			Time          []int64    `json:"time"`
			Temperature   []*float64 `json:"temperature_2m"`
			Humidity      []*float64 `json:"relative_humidity_2m"`
			Precipitation []*float64 `json:"precipitation"`
		} `json:"hourly"`
	}
	if json.Unmarshal(r.RawJSON, &d) != nil || d.Error || d.Latitude == nil || d.Longitude == nil || !validGeo(*d.Latitude, *d.Longitude) || d.Hourly == nil {
		return nil, provider.ErrMalformedResponse
	}
	if u := d.HourlyUnits["temperature_2m"]; u != "°C" && u != "°Celsius" {
		return nil, provider.ErrMalformedResponse
	}
	h := d.Hourly
	n := len(h.Time)
	if n == 0 || n > 168 || len(h.Temperature) != n {
		return nil, provider.ErrMalformedResponse
	}
	if h.Humidity != nil && len(h.Humidity) != n {
		return nil, provider.ErrMalformedResponse
	}
	if h.Precipitation != nil && len(h.Precipitation) != n {
		return nil, provider.ErrMalformedResponse
	}
	for k, u := range map[string]string{"relative_humidity_2m": "%", "precipitation": "mm"} {
		if v, ok := d.HourlyUnits[k]; ok && v != u {
			return nil, provider.ErrMalformedResponse
		}
	}
	out := &ext.HistoryResponse{SchemaVersion: "history.v1", DataKind: "historical_reanalysis", Model: "ERA5", Location: ext.GeoPoint{Latitude: *d.Latitude, Longitude: *d.Longitude}, Points: make([]ext.HistoricalPoint, 0, n)}
	for i, epoch := range h.Time {
		t := time.Unix(epoch, 0).UTC()
		if t.Before(start) || !t.Before(end.Add(24*time.Hour)) || i > 0 && !t.After(out.Points[len(out.Points)-1].ObservedAt) || !real(h.Temperature[i], -120, 70) {
			return nil, provider.ErrMalformedResponse
		}
		point := ext.HistoricalPoint{ObservedAt: t, TemperatureC: *h.Temperature[i]}
		if h.Humidity != nil {
			if h.Humidity[i] != nil && !real(h.Humidity[i], 0, 100) {
				return nil, provider.ErrMalformedResponse
			}
			point.HumidityPercent = h.Humidity[i]
		}
		if h.Precipitation != nil {
			if h.Precipitation[i] != nil && !real(h.Precipitation[i], 0, 10000) {
				return nil, provider.ErrMalformedResponse
			}
			point.PrecipitationMM = h.Precipitation[i]
		}
		out.Points = append(out.Points, point)
	}
	return out, nil
}
func NormalizeMarine(r *provider.RawExtendedResponse, requestedLat, requestedLon float64) (*ext.MarineResponse, error) {
	if r == nil || r.ProviderID != "open_meteo_marine" || !json.Valid(r.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var d struct {
		Latitude    *float64          `json:"latitude"`
		Longitude   *float64          `json:"longitude"`
		Error       bool              `json:"error"`
		HourlyUnits map[string]string `json:"hourly_units"`
		Hourly      *struct {
			Time      []int64    `json:"time"`
			Height    []*float64 `json:"wave_height"`
			Period    []*float64 `json:"wave_period"`
			Direction []*float64 `json:"wave_direction"`
		} `json:"hourly"`
	}
	if json.Unmarshal(r.RawJSON, &d) != nil || d.Error || d.Latitude == nil || d.Longitude == nil || !validGeo(*d.Latitude, *d.Longitude) || d.Hourly == nil {
		return nil, provider.ErrMalformedResponse
	}
	// Never substitute a distant sea cell for an inland request without consent.
	if distanceKM(requestedLat, requestedLon, *d.Latitude, *d.Longitude) > 25 {
		return nil, provider.ErrProviderNotFound
	}
	if d.HourlyUnits["wave_height"] != "m" {
		return nil, provider.ErrMalformedResponse
	}
	for k, u := range map[string]string{"wave_period": "s", "wave_direction": "°"} {
		if v, ok := d.HourlyUnits[k]; ok && v != u {
			return nil, provider.ErrMalformedResponse
		}
	}
	h := d.Hourly
	n := len(h.Time)
	if n == 0 || n > 168 || len(h.Height) != n {
		return nil, provider.ErrMalformedResponse
	}
	if h.Period != nil && len(h.Period) != n {
		return nil, provider.ErrMalformedResponse
	}
	if h.Direction != nil && len(h.Direction) != n {
		return nil, provider.ErrMalformedResponse
	}
	out := &ext.MarineResponse{SchemaVersion: "marine.v1", DataKind: "marine_model_forecast", ModelGrid: ext.GeoPoint{Latitude: *d.Latitude, Longitude: *d.Longitude}, Points: make([]ext.MarinePoint, 0, n)}
	now := time.Now().UTC()
	for i, epoch := range h.Time {
		t := time.Unix(epoch, 0).UTC()
		if t.Before(now.Add(-2 * time.Hour)) {
			continue
		}
		if t.After(now.Add(8*24*time.Hour)) || len(out.Points) > 0 && !t.After(out.Points[len(out.Points)-1].ValidAt) {
			return nil, provider.ErrMalformedResponse
		}
		if !real(h.Height[i], 0, 100) {
			return nil, provider.ErrMalformedResponse
		}
		point := ext.MarinePoint{ValidAt: t, WaveHeightM: *h.Height[i]}
		if h.Period != nil {
			if h.Period[i] != nil && !real(h.Period[i], 0, 300) {
				return nil, provider.ErrMalformedResponse
			}
			point.WavePeriodS = h.Period[i]
		}
		if h.Direction != nil {
			if h.Direction[i] != nil && !real(h.Direction[i], 0, 360) {
				return nil, provider.ErrMalformedResponse
			}
			point.WaveDirectionDegrees = h.Direction[i]
		}
		out.Points = append(out.Points, point)
	}
	if len(out.Points) == 0 {
		return nil, provider.ErrProviderNotFound
	}
	return out, nil
}
func NormalizeAstronomy(r *provider.RawExtendedResponse, date string) (*ext.AstronomyResponse, error) {
	if r == nil || r.ProviderID != "sunrise_sunset" || !json.Valid(r.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var d struct {
		Date      string   `json:"date"`
		Timezone  string   `json:"tzid"`
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"lng"`
		SunStatus string   `json:"sun_status"`
		Sunrise   *string  `json:"sunrise"`
		Sunset    *string  `json:"sunset"`
		Noon      *string  `json:"solar_noon"`
		Moonrise  *string  `json:"moonrise"`
		Moonset   *string  `json:"moonset"`
		MoonPhase string   `json:"moon_phase"`
		Error     string   `json:"error"`
	}
	if json.Unmarshal(r.RawJSON, &d) != nil || d.Error != "" || d.Date != date || d.Timezone == "" || d.Latitude == nil || d.Longitude == nil || !validGeo(*d.Latitude, *d.Longitude) {
		return nil, provider.ErrMalformedResponse
	}
	if validateTimezone(d.Timezone) != nil {
		return nil, provider.ErrMalformedResponse
	}
	switch d.SunStatus {
	case "normal", "midnight_sun", "polar_night":
	default:
		return nil, provider.ErrMalformedResponse
	}
	parse := func(s *string) (*time.Time, error) {
		if s == nil {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, *s)
		if err != nil || t.IsZero() {
			return nil, provider.ErrMalformedResponse
		}
		return &t, nil
	}
	out := &ext.AstronomyResponse{SchemaVersion: "astronomy.v1", DataKind: "astronomical_calculation", Date: date, Timezone: d.Timezone, Location: ext.GeoPoint{Latitude: *d.Latitude, Longitude: *d.Longitude}, SunStatus: d.SunStatus, MoonPhase: d.MoonPhase}
	for _, x := range []struct {
		src *string
		dst **time.Time
	}{{d.Sunrise, &out.Sunrise}, {d.Sunset, &out.Sunset}, {d.Noon, &out.SolarNoon}, {d.Moonrise, &out.Moonrise}, {d.Moonset, &out.Moonset}} {
		var err error
		*x.dst, err = parse(x.src)
		if err != nil {
			return nil, err
		}
	}
	if d.SunStatus == "normal" && out.Sunrise == nil && out.Sunset == nil {
		return nil, provider.ErrMalformedResponse
	}
	return out, nil
}
func NormalizeAlerts(r *provider.RawExtendedResponse, lat, lon float64) (*ext.AlertsResponse, error) {
	if r == nil || r.ProviderID != "us_nws_alerts" || !json.Valid(r.RawJSON) {
		return nil, provider.ErrMalformedResponse
	}
	var d struct {
		Type     string `json:"type"`
		Features []struct {
			ID         string `json:"id"`
			Properties struct {
				ID              string  `json:"id"`
				Event           string  `json:"event"`
				Headline        string  `json:"headline"`
				Severity        string  `json:"severity"`
				Status          string  `json:"status"`
				Urgency         string  `json:"urgency"`
				Certainty       string  `json:"certainty"`
				AreaDescription string  `json:"areaDesc"`
				Effective       *string `json:"effective"`
				Expires         string  `json:"expires"`
				Description     string  `json:"description"`
				Instruction     string  `json:"instruction"`
			} `json:"properties"`
		} `json:"features"`
	}
	if json.Unmarshal(r.RawJSON, &d) != nil || d.Type != "FeatureCollection" || d.Features == nil || len(d.Features) > 100 {
		return nil, provider.ErrMalformedResponse
	}
	out := &ext.AlertsResponse{SchemaVersion: "alerts.v1", DataKind: "official_active_alerts", Location: ext.GeoPoint{Latitude: lat, Longitude: lon}, Alerts: make([]ext.Alert, 0, len(d.Features))}
	seen := make(map[string]bool)
	for _, f := range d.Features {
		p := f.Properties
		id := p.ID
		if id == "" {
			id = f.ID
		}
		if id == "" || p.Event == "" || len(id) > 2048 || len(p.Event) > 256 {
			return nil, provider.ErrMalformedResponse
		}
		expiry, err := time.Parse(time.RFC3339, p.Expires)
		if err != nil {
			return nil, provider.ErrMalformedResponse
		}
		var effective *time.Time
		if p.Effective != nil {
			x, e := time.Parse(time.RFC3339, *p.Effective)
			if e != nil {
				return nil, provider.ErrMalformedResponse
			}
			effective = &x
		}
		if seen[id] {
			return nil, provider.ErrMalformedResponse
		}
		seen[id] = true
		out.Alerts = append(out.Alerts, ext.Alert{ID: id, Event: p.Event, Headline: p.Headline, Severity: p.Severity, Status: p.Status, Urgency: p.Urgency, Certainty: p.Certainty, AreaDescription: p.AreaDescription, Effective: effective, Expires: expiry, Description: p.Description, Instruction: p.Instruction})
	}
	return out, nil
}
