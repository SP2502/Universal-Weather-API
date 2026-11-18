// Package georesolve provides opt-in canonical place resolution. It never
// silently invokes a public geocoder: the operator must enable it and supply
// a policy-eligible adapter. Missing or ambiguous locations are not guessed.
package georesolve

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

var ErrAmbiguousLocation = provider.ErrAmbiguousLocation
var ErrUnresolvedLocation = errors.New("location could not be resolved")

type Resolver struct {
	engine     *decision.DecisionEngine
	normalizer *normalization.GeocodingNormalizer
	executor   *reliability.Executor
}

func New(reg *provider.Registry, engine *decision.DecisionEngine) *Resolver {
	ex := reliability.NewExecutor(4)
	if reg != nil {
		ex.SetHealthEvaluator(reg.HealthEvaluator())
	}
	return &Resolver{engine: engine, normalizer: normalization.NewGeocodingNormalizer(), executor: ex}
}
func (r *Resolver) SetQuotaGate(g reliability.QuotaGate) { r.executor.SetQuotaGate(g) }
func (r *Resolver) Resolve(ctx context.Context, place string) (*locv1.Location, error) {
	if r == nil || r.engine == nil {
		return nil, ErrUnresolvedLocation
	}
	plan, _, err := r.engine.SelectProviders(ctx, decision.RequestContext{Capability: "geocode", Product: "forward_geocoding", AllowDegraded: true, PolicyMode: decision.PolicyAvailabilityFirst})
	if err != nil {
		return nil, fmt.Errorf("%w: no permitted geocoding provider", ErrUnresolvedLocation)
	}
	bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	bounded = reliability.WithQuotaProduct(bounded, "forward_geocoding")
	canonical, _, err := reliability.ExecuteWithFallback[*locv1.GeocodeResponse](bounded, r.executor, plan, reliability.DefaultRetryPolicy(), func(opCtx context.Context, p provider.Provider) (*locv1.GeocodeResponse, error) {
		gp, ok := p.(provider.GeocodingProvider)
		if !ok {
			return nil, provider.ErrMalformedResponse
		}
		raw, err := gp.ForwardGeocode(opCtx, provider.ForwardGeocodeRequest{Query: place, Limit: 2})
		if err != nil {
			return nil, err
		}
		if raw == nil || raw.ProviderID != p.Manifest().ID {
			return nil, provider.ErrMalformedResponse
		}
		normalized, err := r.normalizer.Normalize(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid geocoding result", provider.ErrMalformedResponse)
		}
		if len(normalized.Results) == 0 {
			return nil, provider.ErrProviderNotFound
		}
		// Ambiguity is a query problem, not an upstream health failure. Returning
		// one of several candidates without user disambiguation would be unsafe.
		if len(normalized.Results) > 1 {
			return nil, ErrAmbiguousLocation
		}
		return normalized, nil
	})
	if err != nil {
		if errors.Is(err, ErrAmbiguousLocation) {
			return nil, ErrAmbiguousLocation
		}
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrUnresolvedLocation, err)
	}
	if canonical == nil || len(canonical.Results) != 1 {
		return nil, ErrUnresolvedLocation
	}
	return &canonical.Results[0], nil
}
