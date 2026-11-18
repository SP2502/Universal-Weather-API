package provider

import (
	"context"
	"sort"
	"sync"
)

type Registry struct {
	mu              sync.RWMutex
	providers       map[string]Provider
	healthEvaluator *HealthEvaluator
}

func NewRegistry() *Registry {
	return &Registry{
		providers:       make(map[string]Provider),
		healthEvaluator: NewHealthEvaluator(),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Manifest().ID] = p
}

// IDs returns an immutable sorted snapshot for production budget validation.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) HealthEvaluator() *HealthEvaluator {
	return r.healthEvaluator
}

func (r *Registry) GetWeatherProviders(ctx context.Context) ([]WeatherProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []WeatherProvider
	for _, p := range r.providers {
		m := p.Manifest()
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		for _, capName := range m.Capabilities {
			if capName == "weather" {
				if wp, ok := p.(WeatherProvider); ok {
					result = append(result, wp)
				}
				break
			}
		}
	}

	if len(result) == 0 {
		return nil, ErrProviderNotFound
	}

	// FallbackPriority ascending, HealthScore descending, then ID ascending.
	sort.Slice(result, func(i, j int) bool {
		mi := result[i].Manifest()
		mj := result[j].Manifest()
		pi := mi.FallbackPriority
		pj := mj.FallbackPriority
		if pi != pj {
			return pi < pj
		}
		hi := r.healthEvaluator.GetHealth(mi.ID).Score
		hj := r.healthEvaluator.GetHealth(mj.ID).Score
		if hi != hj {
			return hi > hj
		}
		return mi.ID < mj.ID
	})

	return result, nil
}

func (r *Registry) ResolveWeatherProvider(ctx context.Context, preferredID string) (WeatherProvider, error) {
	providers, err := r.GetWeatherProviders(ctx)
	if err != nil {
		return nil, err
	}

	if preferredID != "" {
		for _, wp := range providers {
			if wp.Manifest().ID == preferredID {
				return wp, nil
			}
		}
		return nil, ErrProviderNotFound
	}

	return providers[0], nil
}

// GetForecastProviders returns only providers implementing model forecasts;
// they are intentionally excluded from current weather observation routing.
func (r *Registry) GetForecastProviders(ctx context.Context) ([]ForecastProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []ForecastProvider
	for _, p := range r.providers {
		m := p.Manifest()
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		for _, capability := range m.Capabilities {
			if capability == "forecast" {
				if fp, ok := p.(ForecastProvider); ok {
					result = append(result, fp)
				}
				break
			}
		}
	}
	if len(result) == 0 {
		return nil, ErrProviderNotFound
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Manifest(), result[j].Manifest()
		if a.FallbackPriority != b.FallbackPriority {
			return a.FallbackPriority < b.FallbackPriority
		}
		return a.ID < b.ID
	})
	return result, nil
}

func (r *Registry) GetGeocodingProviders(ctx context.Context) ([]GeocodingProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []GeocodingProvider
	for _, p := range r.providers {
		m := p.Manifest()
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		for _, capName := range m.Capabilities {
			if capName == "geocode" {
				if gp, ok := p.(GeocodingProvider); ok {
					result = append(result, gp)
				}
				break
			}
		}
	}

	if len(result) == 0 {
		return nil, ErrProviderNotFound
	}

	// FallbackPriority ascending, HealthScore descending, then ID ascending.
	sort.Slice(result, func(i, j int) bool {
		mi := result[i].Manifest()
		mj := result[j].Manifest()
		pi := mi.FallbackPriority
		pj := mj.FallbackPriority
		if pi != pj {
			return pi < pj
		}
		hi := r.healthEvaluator.GetHealth(mi.ID).Score
		hj := r.healthEvaluator.GetHealth(mj.ID).Score
		if hi != hj {
			return hi > hj
		}
		return mi.ID < mj.ID
	})

	return result, nil
}

func (r *Registry) ResolveGeocodingProvider(ctx context.Context, preferredID string) (GeocodingProvider, error) {
	providers, err := r.GetGeocodingProviders(ctx)
	if err != nil {
		return nil, err
	}

	if preferredID != "" {
		for _, gp := range providers {
			if gp.Manifest().ID == preferredID {
				return gp, nil
			}
		}
		return nil, ErrProviderNotFound
	}

	return providers[0], nil
}

// GetAirQualityProviders selects only explicit model air-quality providers.
func (r *Registry) GetAirQualityProviders(ctx context.Context) ([]AirQualityProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []AirQualityProvider
	for _, p := range r.providers {
		m := p.Manifest()
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		for _, c := range m.Capabilities {
			if c == "air_quality" {
				if aq, ok := p.(AirQualityProvider); ok {
					result = append(result, aq)
				}
				break
			}
		}
	}
	if len(result) == 0 {
		return nil, ErrProviderNotFound
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Manifest(), result[j].Manifest()
		if a.FallbackPriority != b.FallbackPriority {
			return a.FallbackPriority < b.FallbackPriority
		}
		return a.ID < b.ID
	})
	return result, nil
}

// GetExtendedProviders returns only adapters implementing their advertised
// Phase 07D interface. A manifest alone cannot make an adapter executable.
func (r *Registry) GetExtendedProviders(capability string) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Provider
	for _, p := range r.providers {
		m := p.Manifest()
		if m.Status == StatusDisabled || m.Status == StatusRemoved {
			continue
		}
		supported := false
		for _, capName := range m.Capabilities {
			if capName == capability {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		switch capability {
		case "history":
			_, supported = p.(HistoryProvider)
		case "astronomy":
			_, supported = p.(AstronomyProvider)
		case "marine":
			_, supported = p.(MarineProvider)
		case "alerts":
			_, supported = p.(AlertsProvider)
		default:
			supported = false
		}
		if supported {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Manifest(), out[j].Manifest()
		if a.FallbackPriority != b.FallbackPriority {
			return a.FallbackPriority < b.FallbackPriority
		}
		return a.ID < b.ID
	})
	return out
}
