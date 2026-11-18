package openmeteomarine

import (
	"context"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/producthttp"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Adapter struct {
	manifest provider.Manifest
	client   *http.Client
}

func NewAdapter(base string) *Adapter {
	if base == "" {
		base = "https://marine-api.open-meteo.com"
	}
	return &Adapter{client: producthttp.Client(), manifest: provider.Manifest{ID: "open_meteo_marine", Name: "Open-Meteo Marine Model", Version: "v1", Capabilities: []string{"marine"}, BaseURL: strings.TrimRight(base, "/"), Timeout: 6 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Marine forecast data by Open-Meteo", URL: "https://open-meteo.com/en/docs/marine-weather-api"}}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.client = c; return a }
func (a *Adapter) FetchMarine(ctx context.Context, lat, lon float64) (*provider.RawExtendedResponse, error) {
	q := url.Values{"latitude": {strconv.FormatFloat(lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(lon, 'f', 4, 64)}, "hourly": {"wave_height,wave_period,wave_direction"}, "forecast_days": {"2"}, "timezone": {"UTC"}, "timeformat": {"unixtime"}}
	raw, err := producthttp.Fetch(ctx, a.client, a.manifest.BaseURL, "/v1/marine", q, "")
	if err != nil {
		return nil, err
	}
	return &provider.RawExtendedResponse{ProviderID: a.manifest.ID, RawJSON: raw, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.MarineProvider = (*Adapter)(nil)
