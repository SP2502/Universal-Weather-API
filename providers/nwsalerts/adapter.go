package nwsalerts

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
	agent    string
}

func NewAdapter(agent, base string) *Adapter {
	if base == "" {
		base = "https://api.weather.gov"
	}
	return &Adapter{agent: agent, client: producthttp.Client(), manifest: provider.Manifest{ID: "us_nws_alerts", Name: "US NWS Active Weather Alerts", Version: "v1", Capabilities: []string{"alerts"}, BaseURL: strings.TrimRight(base, "/"), Timeout: 6 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Official alerts from the US National Weather Service", URL: "https://www.weather.gov/"}}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.client = c; return a }
func (a *Adapter) FetchAlerts(ctx context.Context, lat, lon float64) (*provider.RawExtendedResponse, error) {
	q := url.Values{"point": {strconv.FormatFloat(lat, 'f', 4, 64) + "," + strconv.FormatFloat(lon, 'f', 4, 64)}}
	raw, err := producthttp.Fetch(ctx, a.client, a.manifest.BaseURL, "/alerts/active", q, a.agent, "application/geo+json")
	if err != nil {
		return nil, err
	}
	return &provider.RawExtendedResponse{ProviderID: a.manifest.ID, RawJSON: raw, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.AlertsProvider = (*Adapter)(nil)
