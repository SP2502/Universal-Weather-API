package provider

import (
	"context"
	"testing"
	"time"
)

type dummyWeatherProvider struct {
	m Manifest
}

func (d *dummyWeatherProvider) Manifest() Manifest { return d.m }
func (d *dummyWeatherProvider) FetchWeather(ctx context.Context, location string) (*RawWeatherResponse, error) {
	return &RawWeatherResponse{ProviderID: d.m.ID, RetrievedAt: time.Now()}, nil
}

func TestRegistryOrderingAndFiltering(t *testing.T) {
	reg := NewRegistry()

	p1 := &dummyWeatherProvider{
		m: Manifest{
			ID:               "provider-secondary",
			Capabilities:     []string{"weather"},
			FallbackPriority: 2,
			Status:           StatusActive,
		},
	}
	p2 := &dummyWeatherProvider{
		m: Manifest{
			ID:               "provider-primary",
			Capabilities:     []string{"weather"},
			FallbackPriority: 1,
			Status:           StatusActive,
		},
	}
	pDisabled := &dummyWeatherProvider{
		m: Manifest{
			ID:               "provider-disabled",
			Capabilities:     []string{"weather"},
			FallbackPriority: 0,
			Status:           StatusDisabled,
		},
	}

	reg.Register(p1)
	reg.Register(p2)
	reg.Register(pDisabled)

	// Primary should be provider-primary
	resolved, err := reg.ResolveWeatherProvider(context.Background(), "")
	if err != nil {
		t.Fatalf("expected resolved provider, got err: %v", err)
	}
	if resolved.Manifest().ID != "provider-primary" {
		t.Errorf("expected provider-primary, got %s", resolved.Manifest().ID)
	}

	// Request specific secondary
	resolvedSec, err := reg.ResolveWeatherProvider(context.Background(), "provider-secondary")
	if err != nil {
		t.Fatalf("expected secondary provider, got err: %v", err)
	}
	if resolvedSec.Manifest().ID != "provider-secondary" {
		t.Errorf("expected provider-secondary, got %s", resolvedSec.Manifest().ID)
	}

	// Disabled provider should not resolve
	_, err = reg.ResolveWeatherProvider(context.Background(), "provider-disabled")
	if err != ErrProviderNotFound {
		t.Errorf("expected ErrProviderNotFound for disabled provider, got %v", err)
	}
}

type dummyGeocodingProvider struct{ m Manifest }

func (d *dummyGeocodingProvider) Manifest() Manifest { return d.m }
func (d *dummyGeocodingProvider) ForwardGeocode(context.Context, ForwardGeocodeRequest) (*RawGeocodeResponse, error) {
	return nil, nil
}
func (d *dummyGeocodingProvider) ReverseGeocode(context.Context, ReverseGeocodeRequest) (*RawGeocodeResponse, error) {
	return nil, nil
}

func TestRegistryEqualRankUsesProviderID(t *testing.T) {
	reg := NewRegistry()
	for _, id := range []string{"zeta", "alpha", "middle"} {
		reg.Register(&dummyWeatherProvider{m: Manifest{ID: id, Capabilities: []string{"weather"}, FallbackPriority: 1, Status: StatusActive}})
	}
	for _, id := range []string{"geo-zeta", "geo-alpha", "geo-middle"} {
		reg.Register(&dummyGeocodingProvider{m: Manifest{ID: id, Capabilities: []string{"geocode"}, FallbackPriority: 1, Status: StatusActive}})
	}
	for attempt := 0; attempt < 40; attempt++ {
		weather, err := reg.GetWeatherProviders(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"alpha", "middle", "zeta"} {
			if got := weather[i].Manifest().ID; got != want {
				t.Fatalf("weather attempt %d, position %d: got %s, want %s", attempt, i, got, want)
			}
		}
		geocoding, err := reg.GetGeocodingProviders(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"geo-alpha", "geo-middle", "geo-zeta"} {
			if got := geocoding[i].Manifest().ID; got != want {
				t.Fatalf("geocoding attempt %d, position %d: got %s, want %s", attempt, i, got, want)
			}
		}
	}
}
