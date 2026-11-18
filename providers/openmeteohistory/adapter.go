package openmeteohistory

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
		base = "https://archive-api.open-meteo.com"
	}
	return &Adapter{client: producthttp.Client(), manifest: provider.Manifest{ID: "open_meteo_history", Name: "Open-Meteo ERA5 Reanalysis", Version: "v1", Capabilities: []string{"history"}, BaseURL: strings.TrimRight(base, "/"), Timeout: 6 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Historical reanalysis by Open-Meteo / ECMWF ERA5", URL: "https://open-meteo.com/en/docs/historical-weather-api"}}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.client = c; return a }
func (a *Adapter) FetchHistory(ctx context.Context, lat, lon float64, start, end time.Time) (*provider.RawExtendedResponse, error) {
	q := url.Values{"latitude": {strconv.FormatFloat(lat, 'f', 4, 64)}, "longitude": {strconv.FormatFloat(lon, 'f', 4, 64)}, "start_date": {start.UTC().Format("2006-01-02")}, "end_date": {end.UTC().Format("2006-01-02")}, "hourly": {"temperature_2m,relative_humidity_2m,precipitation"}, "models": {"era5"}, "timezone": {"UTC"}, "timeformat": {"unixtime"}}
	raw, err := producthttp.Fetch(ctx, a.client, a.manifest.BaseURL, "/v1/archive", q, "")
	if err != nil {
		return nil, err
	}
	return &provider.RawExtendedResponse{ProviderID: a.manifest.ID, RawJSON: raw, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.HistoryProvider = (*Adapter)(nil)
