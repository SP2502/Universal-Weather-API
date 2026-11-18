package integration

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/providers/locationiq"
	"github.com/universal-api/platform/providers/nominatim"
	"github.com/universal-api/platform/providers/openweather"
	"github.com/universal-api/platform/providers/weatherapi"
)

type brokenRoundTripper struct{}

func (brokenRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic connection failed")
}

// net/http wraps failed transport requests in url.Error, which includes the
// full credential-bearing query URL. No upstream secret may cross adapter's
// error boundary, including while the gateway is failing over.
func TestProviderTransportErrorsDoNotLeakURLCredentials(t *testing.T) {
	const secret = "TEST_KEY_PLEASE_DO_NOT_LOG"
	client := &http.Client{Transport: brokenRoundTripper{}}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"openweather", func() error {
			_, e := openweather.NewAdapter(secret, "https://api.example.test").WithHTTPClient(client).FetchWeather(ctx, "21.25,81.63")
			return e
		}},
		{"weatherapi", func() error {
			_, e := weatherapi.NewAdapter(secret, "https://api.example.test").WithHTTPClient(client).FetchWeather(ctx, "21.25,81.63")
			return e
		}},
		{"locationiq-forward", func() error {
			_, e := locationiq.NewAdapter(secret, "https://api.example.test").WithHTTPClient(client).ForwardGeocode(ctx, provider.ForwardGeocodeRequest{Query: "Raipur"})
			return e
		}},
		{"locationiq-reverse", func() error {
			_, e := locationiq.NewAdapter(secret, "https://api.example.test").WithHTTPClient(client).ReverseGeocode(ctx, provider.ReverseGeocodeRequest{Latitude: 21.25, Longitude: 81.63})
			return e
		}},
		{"nominatim-address-privacy", func() error {
			_, e := nominatim.NewAdapter("https://api.example.test").WithHTTPClient(client).ForwardGeocode(ctx, provider.ForwardGeocodeRequest{Query: "SensitiveStreetIdentifier"})
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.call()
			if !errors.Is(e, provider.ErrProviderUnavailable) || strings.Contains(e.Error(), secret) || strings.Contains(e.Error(), "api.example.test") {
				t.Fatalf("transport errors must be classified and redact request URLs: %v", e)
			}
		})
	}
}
