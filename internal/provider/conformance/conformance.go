package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/provider"
)

// ConformanceSuite runs standard contract and reliability conformance assertions on any Provider.
type ConformanceSuite struct {
	Provider provider.Provider
}

func NewConformanceSuite(p provider.Provider) *ConformanceSuite {
	return &ConformanceSuite{Provider: p}
}

// RunAll executes the entire conformance suite against the provider.
func (cs *ConformanceSuite) RunAll(t *testing.T) {
	t.Run("ManifestValidity", cs.TestManifestValidity)
}

func (cs *ConformanceSuite) TestManifestValidity(t *testing.T) {
	m := cs.Provider.Manifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("provider manifest failed validation: %v", err)
	}

	if m.ID == "" {
		t.Errorf("manifest ID must not be empty")
	}
	if m.Name == "" {
		t.Errorf("manifest Name must not be empty")
	}
	if len(m.Capabilities) == 0 {
		t.Errorf("manifest must declare at least one capability")
	}
	if m.Timeout <= 0 {
		t.Errorf("manifest Timeout must be > 0, got %v", m.Timeout)
	}
	if m.FallbackPriority < 1 {
		t.Errorf("manifest FallbackPriority must be >= 1, got %d", m.FallbackPriority)
	}
	if m.Attribution.Required && m.Attribution.Text == "" {
		t.Errorf("attribution text required when Attribution.Required is true")
	}
}

// Helper to create a fake upstream server responding with a specific status code and body
func NewMockUpstreamServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

// Helper to create a hanging server to test context deadline cancellation
func NewSlowUpstreamServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

// Helper to create an oversized upstream server returning > 1MB of data
func NewOversizedUpstreamServer(sizeMB int) *httptest.Server {
	chunk := make([]byte, 1024*1024)
	for i := range chunk {
		chunk[i] = 'A'
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < sizeMB; i++ {
			_, _ = w.Write(chunk)
		}
	}))
}

// TestContextDeadlineCancellation tests that the provider adapter respects context cancellation
func TestContextDeadlineCancellation(t *testing.T, op func(ctx context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := op(ctx)
	if err == nil {
		t.Errorf("expected error when context deadline exceeded, got nil")
	}
}

// VerifyWeatherProviderErrorResponses tests that a weather provider correctly maps HTTP error statuses
func VerifyWeatherProviderErrorResponses(t *testing.T, createAdapter func(baseURL string) provider.WeatherProvider) {
	t.Run("Status401_AuthenticationFailure", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusUnauthorized, `{"error":"unauthorized"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.FetchWeather(context.Background(), "London")
		if err == nil {
			t.Errorf("expected error for 401, got nil")
		}
	})

	t.Run("Status429_RateLimited", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.FetchWeather(context.Background(), "London")
		if err == nil {
			t.Errorf("expected error for 429, got nil")
		}
	})

	t.Run("Status500_ServerUnavailable", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusInternalServerError, `{"error":"internal server error"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.FetchWeather(context.Background(), "London")
		if err == nil {
			t.Errorf("expected error for 500, got nil")
		}
	})

	t.Run("MalformedJSON", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusOK, `{invalid-json`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.FetchWeather(context.Background(), "London")
		if err == nil {
			t.Errorf("expected error for malformed json, got nil")
		}
	})
}

// VerifyGeocodingProviderErrorResponses tests that a geocoding provider correctly maps HTTP error statuses
func VerifyGeocodingProviderErrorResponses(t *testing.T, createAdapter func(baseURL string) provider.GeocodingProvider) {
	t.Run("Forward_Status429_RateLimited", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Paris"})
		if err == nil {
			t.Errorf("expected error for 429, got nil")
		}
	})

	t.Run("Forward_Status500_ServerUnavailable", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusInternalServerError, `{"error":"internal error"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Paris"})
		if err == nil {
			t.Errorf("expected error for 500, got nil")
		}
	})

	t.Run("Forward_MalformedJSON", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusOK, `{broken-json`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.ForwardGeocode(context.Background(), provider.ForwardGeocodeRequest{Query: "Paris"})
		if err == nil {
			t.Errorf("expected error for malformed json, got nil")
		}
	})

	t.Run("Reverse_Status500_ServerUnavailable", func(t *testing.T) {
		ts := NewMockUpstreamServer(http.StatusInternalServerError, `{"error":"internal error"}`)
		defer ts.Close()

		p := createAdapter(ts.URL)
		_, err := p.ReverseGeocode(context.Background(), provider.ReverseGeocodeRequest{Latitude: 48.8566, Longitude: 2.3522})
		if err == nil {
			t.Errorf("expected error for 500, got nil")
		}
	})
}
