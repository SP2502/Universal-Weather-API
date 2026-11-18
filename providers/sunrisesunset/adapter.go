package sunrisesunset

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
		base = "https://api.sunrise-sunset.org"
	}
	return &Adapter{client: producthttp.Client(), manifest: provider.Manifest{ID: "sunrise_sunset", Name: "Sunrise-Sunset.org Astronomical Calculations", Version: "v2", Capabilities: []string{"astronomy"}, BaseURL: strings.TrimRight(base, "/"), Timeout: 6 * time.Second, FallbackPriority: 2, Status: provider.StatusActive, Attribution: provider.Attribution{Required: true, Text: "Sun data by Sunrise-Sunset.org", URL: "https://sunrise-sunset.org/"}}}
}
func (a *Adapter) Manifest() provider.Manifest            { return a.manifest }
func (a *Adapter) WithHTTPClient(c *http.Client) *Adapter { a.client = c; return a }
func (a *Adapter) FetchAstronomy(ctx context.Context, lat, lon float64, date string) (*provider.RawExtendedResponse, error) {
	q := url.Values{"lat": {strconv.FormatFloat(lat, 'f', 4, 64)}, "lng": {strconv.FormatFloat(lon, 'f', 4, 64)}, "date": {date}, "time_format": {"iso8601"}}
	raw, err := producthttp.Fetch(ctx, a.client, a.manifest.BaseURL, "/v2", q, "")
	if err != nil {
		return nil, err
	}
	return &provider.RawExtendedResponse{ProviderID: a.manifest.ID, RawJSON: raw, RetrievedAt: time.Now().UTC()}, nil
}

var _ provider.AstronomyProvider = (*Adapter)(nil)
