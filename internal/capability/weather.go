package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/georesolve"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

type CachedWeatherPayload struct {
	IntegrityVersion int        `json:"integrity_version"`
	OriginValidUntil *time.Time `json:"origin_valid_until,omitempty"`

	Data         *weatherv1.WeatherResponse `json:"data"`
	ProviderID   string                     `json:"provider_id"`
	FallbackUsed bool                       `json:"fallback_used"`
	Attribution  []api.AttributionItem      `json:"attribution"`
}

type WeatherHandler struct {
	registry       *provider.Registry
	decisionEngine *decision.DecisionEngine
	normalizer     *normalization.WeatherNormalizer
	executor       *reliability.Executor
	retryPolicy    reliability.RetryPolicy
	tracker        usage.UsageTracker
	cacheStore     cache.Store
	cachePolicy    *cache.PolicyEngine
	singleflight   *cache.SingleFlightGroup
	refreshFlight  *cache.SingleFlightGroup
	refreshSem     chan struct{}
	placeResolver  interface {
		Resolve(context.Context, string) (*locv1.Location, error)
	}
}

func NewWeatherHandler(
	registry *provider.Registry,
	decisionEngine *decision.DecisionEngine,
	normalizer *normalization.WeatherNormalizer,
	tracker usage.UsageTracker,
	cacheStore cache.Store,
	cachePolicy *cache.PolicyEngine,
) *WeatherHandler {
	executor := reliability.NewExecutor(4)
	if registry != nil {
		executor.SetHealthEvaluator(registry.HealthEvaluator())
	}
	if decisionEngine == nil && registry != nil {
		decisionEngine = decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	}

	return &WeatherHandler{
		registry:       registry,
		decisionEngine: decisionEngine,
		normalizer:     normalizer,
		executor:       executor,
		retryPolicy:    reliability.DefaultRetryPolicy(),
		tracker:        tracker,
		cacheStore:     cacheStore,
		cachePolicy:    cachePolicy,
		singleflight:   cache.NewSingleFlightGroup(),
		refreshFlight:  cache.NewSingleFlightGroup(),
		refreshSem:     make(chan struct{}, 32),
	}
}

// SetPlaceResolver opts in to canonical place-name resolution. The default
// remains disabled to avoid unexpected geocoding charges or public-service use.
func (h *WeatherHandler) SetPlaceResolver(resolver interface {
	Resolve(context.Context, string) (*locv1.Location, error)
}) {
	h.placeResolver = resolver
}

// fetchCanonicalWeather validates every provider response inside its attempt.
// Neither a decoded HTTP 200 nor a nil payload can close the circuit.
// SetQuotaGate configures per-attempt admission for weather providers.
func (h *WeatherHandler) SetQuotaGate(gate reliability.QuotaGate) { h.executor.SetQuotaGate(gate) }

type weatherFetch struct {
	Data      *weatherv1.WeatherResponse
	OriginTTL *time.Duration
}

func (h *WeatherHandler) validWeatherPayload(item CachedWeatherPayload, req decision.RequestContext, retained bool) bool {
	if retained && item.IntegrityVersion != 1 {
		return false
	} // invalidate pre-repair shared entries
	if normalization.ValidateWeather(item.Data) != nil || !h.decisionEngine.PolicyAllows(req, item.ProviderID) {
		return false
	}
	if item.Data.SourceCondition != nil && item.Data.SourceCondition.Provider != item.ProviderID {
		return false
	}
	if normalization.ValidateRequestedLocation(item.ProviderID, req.Latitude, req.Longitude, &item.Data.Location) != nil {
		return false
	}
	return !retained || item.OriginValidUntil == nil || time.Now().Before(*item.OriginValidUntil)
}

func (h *WeatherHandler) fetchCanonicalWeather(ctx context.Context, p provider.Provider, location string, req decision.RequestContext) (*weatherFetch, error) {
	wp, ok := p.(provider.WeatherProvider)
	if !ok {
		return nil, fmt.Errorf("%w: provider lacks weather capability", provider.ErrMalformedResponse)
	}
	raw, err := wp.FetchWeather(ctx, location)
	if err != nil {
		return nil, err
	}
	if raw == nil || raw.ProviderID != p.Manifest().ID {
		return nil, fmt.Errorf("%w: missing or inconsistent provider identity", provider.ErrMalformedResponse)
	}
	canonical, err := h.normalizer.Normalize(raw)
	if err != nil {
		metrics.DefaultMetrics.NormalizationErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"}, metrics.Label{Name: "provider", Value: p.Manifest().ID})
		return nil, fmt.Errorf("%w: weather canonical validation: %v", provider.ErrMalformedResponse, err)
	}
	if err := normalization.ValidateRequestedLocation(p.Manifest().ID, req.Latitude, req.Longitude, &canonical.Location); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}
	return &weatherFetch{Data: canonical, OriginTTL: raw.FreshTTL}, nil
}

func (h *WeatherHandler) GetWeather(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := logging.RequestIDFromContext(r.Context())
	caller := auth.CallerFromContext(r.Context())

	validated, validationErr := parseWeatherRequest(r.URL)
	if validationErr != nil {
		api.WriteError(w, http.StatusBadRequest, "INVALID_QUERY_PARAMETER", validationErr.Error(), reqID, false)
		return
	}
	location, parsedLat, parsedLon := validated.location, validated.latitude, validated.longitude
	resolvedCountry := ""
	if h.placeResolver != nil && parsedLat == nil {
		place, err := h.placeResolver.Resolve(r.Context(), location)
		if err != nil {
			status, code := http.StatusBadGateway, "PLACE_RESOLUTION_FAILED"
			if errors.Is(err, georesolve.ErrAmbiguousLocation) {
				status, code = http.StatusUnprocessableEntity, "AMBIGUOUS_LOCATION"
			}
			api.WriteError(w, status, code, "Unable to resolve an unambiguous location; use explicit coordinates", reqID, false)
			return
		}
		if place == nil {
			api.WriteError(w, http.StatusBadGateway, "PLACE_RESOLUTION_FAILED", "Location resolver returned no coordinates", reqID, false)
			return
		}
		lat, lon := place.Latitude, place.Longitude
		parsedLat, parsedLon = &lat, &lon
		resolvedCountry = place.CountryCode
		location = fmt.Sprintf("%.6f,%.6f", lat, lon)
	}
	preferredProvider, policyMode := validated.provider, validated.policy

	// Construct the same geographic/product policy context for cache reads,
	// foreground requests, and background refreshes. A geographic provider
	// may be selected only when its coverage can be proven from location data.
	reqCtx := decision.RequestContext{
		Capability: "weather", Product: "current_weather",
		PreferredProvider: preferredProvider, AllowDegraded: true,
		PolicyMode: policyMode, Latitude: parsedLat, Longitude: parsedLon, CountryCode: resolvedCountry,
	}

	// 1. Build canonical cache key with coordinate rounding if coordinates are present
	keyParams := cache.WeatherKeyParams{
		Location:          location,
		Latitude:          parsedLat,
		Longitude:         parsedLon,
		SchemaVersion:     "weather.v1",
		Unit:              "celsius",
		PreferredProvider: preferredProvider,
		PolicyMode:        string(policyMode),
	}
	cacheKey := cache.BuildWeatherCacheKey(keyParams)

	var policyEval cache.PolicyEvaluation
	if h.cachePolicy != nil {
		policyEval = h.cachePolicy.EvaluatePolicy("weather", preferredProvider)
	}

	// 2. Check Cache Layer (Fresh vs Stale)
	if h.cacheStore != nil && policyEval.IsCacheable {
		entry, err := h.cacheStore.GetEntry(r.Context(), cacheKey)
		if err != nil {
			// Fail-open: log cache read failure and continue to provider execution
			slog.WarnContext(r.Context(), "Cache GET error; falling back to provider execution", "request_id", reqID, "error", err)
			metrics.DefaultMetrics.CacheErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
		} else if entry != nil {
			switch entry.State {
			case cache.StateFresh:
				// --- FRESH CACHE HIT ---
				var cachedItem CachedWeatherPayload
				if unmarshalErr := json.Unmarshal(entry.Value, &cachedItem); unmarshalErr == nil && cachedItem.Data != nil && h.cachePolicy.EvaluatePolicy("weather", cachedItem.ProviderID).IsCacheable && h.validWeatherPayload(cachedItem, reqCtx, true) {
					durationMs := int(time.Since(start).Milliseconds())
					metrics.DefaultMetrics.CacheRequestsTotal.Inc(
						metrics.Label{Name: "capability", Value: "weather"},
						metrics.Label{Name: "result", Value: "hit"},
					)
					metrics.DefaultMetrics.CacheHitsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})

					if caller != nil && h.tracker != nil {
						h.tracker.Record(r.Context(), caller.KeyID, "weather", cachedItem.ProviderID, reqID, http.StatusOK, durationMs)
					}

					meta := api.Meta{
						RequestID:   reqID,
						Timestamp:   time.Now().UTC(),
						Provider:    cachedItem.ProviderID,
						Cached:      true,
						Fallback:    cachedItem.FallbackUsed,
						Attribution: cachedItem.Attribution,
					}
					api.WriteSuccess(w, http.StatusOK, cachedItem.Data, meta)
					return
				}

			case cache.StateStale:
				if policyEval.CanServeSWR(entry, time.Now()) {
					// --- STALE-WHILE-REVALIDATE ---
					var cachedItem CachedWeatherPayload
					if unmarshalErr := json.Unmarshal(entry.Value, &cachedItem); unmarshalErr == nil && cachedItem.Data != nil && h.cachePolicy.EvaluatePolicy("weather", cachedItem.ProviderID).IsCacheable && h.validWeatherPayload(cachedItem, reqCtx, true) {
						durationMs := int(time.Since(start).Milliseconds())
						metrics.DefaultMetrics.CacheRequestsTotal.Inc(
							metrics.Label{Name: "capability", Value: "weather"},
							metrics.Label{Name: "result", Value: "stale_hit"},
						)
						metrics.DefaultMetrics.CacheStaleHitsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})

						// Asynchronously trigger background refresh under single-flight protection
						h.triggerBackgroundRefresh(cacheKey, location, reqCtx, policyEval)

						if caller != nil && h.tracker != nil {
							h.tracker.Record(r.Context(), caller.KeyID, "weather", cachedItem.ProviderID, reqID, http.StatusOK, durationMs)
						}

						w.Header().Set("Warning", `110 "Response is Stale"`)
						meta := api.Meta{
							RequestID:   reqID,
							Timestamp:   time.Now().UTC(),
							Provider:    cachedItem.ProviderID,
							Cached:      true,
							Fallback:    cachedItem.FallbackUsed,
							Attribution: cachedItem.Attribution,
						}
						api.WriteSuccess(w, http.StatusOK, cachedItem.Data, meta)
						return
					}
				}
			}
		}

		metrics.DefaultMetrics.CacheRequestsTotal.Inc(
			metrics.Label{Name: "capability", Value: "weather"},
			metrics.Label{Name: "result", Value: "miss"},
		)
		metrics.DefaultMetrics.CacheMissesTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
	}

	// 3. Intelligent Provider Selection via Decision Engine
	providers, explanation, err := h.decisionEngine.SelectProviders(r.Context(), reqCtx)
	if err != nil || len(providers) == 0 {
		metrics.DefaultMetrics.ProviderRoutingFailuresTotal.Inc(
			metrics.Label{Name: "capability", Value: "weather"},
			metrics.Label{Name: "reason", Value: "no_eligible_providers"},
		)
		if h.serveStaleIfError(w, r, cacheKey, policyEval, reqCtx, caller, reqID, time.Since(start)) {
			return
		}
		api.WriteError(w, http.StatusServiceUnavailable, "PROVIDER_NOT_AVAILABLE", "No eligible providers available for weather", reqID, true)
		return
	}

	metrics.DefaultMetrics.ProviderRoutingDecisionsTotal.Inc(
		metrics.Label{Name: "capability", Value: "weather"},
		metrics.Label{Name: "policy", Value: string(explanation.PolicyMode)},
		metrics.Label{Name: "selected_provider", Value: explanation.SelectedProvider},
	)

	// 4. Request Coalescing (Single-Flight): Deduplicate identical concurrent cache misses
	flightTimeout := 10 * time.Second
	metrics.DefaultMetrics.CacheSingleflightTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})

	serializedPayload, shared, execErr := h.singleflight.Do(r.Context(), cacheKey, flightTimeout, func(execCtx context.Context) ([]byte, error) {
		ctx, span := tracing.DefaultTracer.StartSpan(execCtx, "Capability Weather")
		defer span.End()
		span.SetAttribute("capability", "weather")
		span.SetAttribute("routing.selected_provider", explanation.SelectedProvider)
		span.SetAttribute("routing.policy_mode", string(explanation.PolicyMode))

		ctx = reliability.WithQuotaProduct(ctx, "current_weather")
		canonicalData, execMeta, err := reliability.ExecuteWithFallback[*weatherFetch](
			ctx, h.executor, providers, h.retryPolicy,
			func(opCtx context.Context, p provider.Provider) (*weatherFetch, error) {
				return h.fetchCanonicalWeather(opCtx, p, location, reqCtx)
			},
		)
		if err != nil {
			span.SetStatus(tracing.StatusError, err.Error())
			return nil, err
		}

		manifest, _ := h.registry.Get(execMeta.ProviderID)
		var attributions []api.AttributionItem
		if manifest != nil && manifest.Manifest().Attribution.Required {
			m := manifest.Manifest()
			attributions = append(attributions, api.AttributionItem{
				Provider: m.ID,
				Text:     m.Attribution.Text,
				URL:      m.Attribution.URL,
			})
		}

		cacheItem := CachedWeatherPayload{
			IntegrityVersion: 1,
			Data:             canonicalData.Data,
			ProviderID:       execMeta.ProviderID,
			FallbackUsed:     execMeta.FallbackUsed,
			Attribution:      attributions,
		}

		if canonicalData.OriginTTL != nil {
			until := time.Now().Add(*canonicalData.OriginTTL)
			cacheItem.OriginValidUntil = &until
		}
		bytes, jsonErr := json.Marshal(cacheItem)
		if jsonErr != nil {
			return nil, jsonErr
		}

		// Store in cache with fresh & stale TTLs
		if h.cacheStore != nil && policyEval.IsCacheable && (canonicalData.OriginTTL == nil || *canonicalData.OriginTTL > 0) {
			provCacheable, provTTL := h.cachePolicy.Evaluate("weather", execMeta.ProviderID)
			if provCacheable {
				freshTTL := provTTL
				if freshTTL <= 0 {
					freshTTL = policyEval.FreshTTL
				}
				staleTTL := policyEval.StaleTTL
				if staleTTL < freshTTL {
					staleTTL = freshTTL
				}
				if canonicalData.OriginTTL != nil {
					if freshTTL > *canonicalData.OriginTTL {
						freshTTL = *canonicalData.OriginTTL
					}
					if staleTTL > *canonicalData.OriginTTL {
						staleTTL = *canonicalData.OriginTTL
					}
				}
				if setErr := h.cacheStore.SetWithStale(execCtx, cacheKey, bytes, freshTTL, staleTTL); setErr != nil {
					slog.Warn("Cache SET error (fail-open)", "error", setErr)
					metrics.DefaultMetrics.CacheErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
				}
			}
		}

		return bytes, nil
	})

	if shared {
		metrics.DefaultMetrics.CacheSingleflightWaiters.Inc(metrics.Label{Name: "capability", Value: "weather"})
	}

	durationMs := int(time.Since(start).Milliseconds())

	// 5. Handle Execution Failures & Stale-If-Error Fallback
	if execErr != nil {
		slog.WarnContext(r.Context(), "Upstream weather execution failed", "request_id", reqID, "error", execErr.Error())

		// Reuse retained data only inside the independently bounded SIE window.
		if h.serveStaleIfError(w, r, cacheKey, policyEval, reqCtx, caller, reqID, time.Since(start)) {
			return
		}
		status, code, message := http.StatusBadGateway, "UPSTREAM_ERROR", "All upstream weather providers failed to respond"
		if errors.Is(execErr, context.DeadlineExceeded) {
			status, code, message = http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "Upstream weather request timed out"
		}

		if caller != nil && h.tracker != nil {
			h.tracker.Record(r.Context(), caller.KeyID, "weather", "unknown", reqID, status, durationMs)
		}
		api.WriteError(w, status, code, message, reqID, true)
		return
	}

	// 6. Return Universal Success Response
	var resultItem CachedWeatherPayload
	if err := json.Unmarshal(serializedPayload, &resultItem); err != nil {
		api.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to deserialize execution payload", reqID, false)
		return
	}

	// Shared executions can finish across an operator policy reload.
	// Recheck the actual provider identity before delivering the payload.
	if !h.validWeatherPayload(resultItem, reqCtx, false) {
		api.WriteError(w, http.StatusServiceUnavailable, "PROVIDER_POLICY_CHANGED", "Provider eligibility changed during request", reqID, true)
		return
	}

	if caller != nil && h.tracker != nil {
		h.tracker.Record(r.Context(), caller.KeyID, "weather", resultItem.ProviderID, reqID, http.StatusOK, durationMs)
	}

	meta := api.Meta{
		RequestID:   reqID,
		Timestamp:   time.Now().UTC(),
		Provider:    resultItem.ProviderID,
		Cached:      false,
		Fallback:    resultItem.FallbackUsed,
		Attribution: resultItem.Attribution,
	}

	api.WriteSuccess(w, http.StatusOK, resultItem.Data, meta)
}

func (h *WeatherHandler) triggerBackgroundRefresh(cacheKey, location string, reqCtx decision.RequestContext, policyEval cache.PolicyEvaluation) {
	select {
	case h.refreshSem <- struct{}{}:
		go func() {
			defer func() { <-h.refreshSem }()
			// Single-flight on background refresh prevents refresh stampedes
			_, _, _ = h.refreshFlight.Do(context.Background(), cacheKey, 10*time.Second, func(bgCtx context.Context) ([]byte, error) {
				metrics.DefaultMetrics.CacheRefreshTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})

				providers, _, err := h.decisionEngine.SelectProviders(bgCtx, reqCtx)
				if err != nil || len(providers) == 0 {
					metrics.DefaultMetrics.CacheRefreshErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
					return nil, err
				}

				bgCtx = reliability.WithQuotaProduct(bgCtx, "current_weather")
				canonicalData, execMeta, err := reliability.ExecuteWithFallback[*weatherFetch](
					bgCtx, h.executor, providers, h.retryPolicy,
					func(opCtx context.Context, p provider.Provider) (*weatherFetch, error) {
						return h.fetchCanonicalWeather(opCtx, p, location, reqCtx)
					},
				)
				if err != nil {
					metrics.DefaultMetrics.CacheRefreshErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
					return nil, err
				}

				manifest, _ := h.registry.Get(execMeta.ProviderID)
				var attributions []api.AttributionItem
				if manifest != nil && manifest.Manifest().Attribution.Required {
					m := manifest.Manifest()
					attributions = append(attributions, api.AttributionItem{
						Provider: m.ID,
						Text:     m.Attribution.Text,
						URL:      m.Attribution.URL,
					})
				}

				cacheItem := CachedWeatherPayload{
					IntegrityVersion: 1,
					Data:             canonicalData.Data,
					ProviderID:       execMeta.ProviderID,
					FallbackUsed:     execMeta.FallbackUsed,
					Attribution:      attributions,
				}

				if canonicalData.OriginTTL != nil {
					until := time.Now().Add(*canonicalData.OriginTTL)
					cacheItem.OriginValidUntil = &until
				}
				bytes, jsonErr := json.Marshal(cacheItem)
				if jsonErr != nil {
					return nil, jsonErr
				}

				if h.cacheStore != nil && h.cachePolicy != nil {
					providerPolicy := h.cachePolicy.EvaluatePolicy("weather", execMeta.ProviderID)
					if providerPolicy.IsCacheable && (canonicalData.OriginTTL == nil || *canonicalData.OriginTTL > 0) {
						if canonicalData.OriginTTL != nil {
							if providerPolicy.FreshTTL > *canonicalData.OriginTTL {
								providerPolicy.FreshTTL = *canonicalData.OriginTTL
							}
							if providerPolicy.StaleTTL > *canonicalData.OriginTTL {
								providerPolicy.StaleTTL = *canonicalData.OriginTTL
							}
						}
						if err := h.cacheStore.SetWithStale(bgCtx, cacheKey, bytes, providerPolicy.FreshTTL, providerPolicy.StaleTTL); err != nil {
							metrics.DefaultMetrics.CacheRefreshErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
							slog.Warn("Background weather cache write failed", "error", err)
						}
					}
				}

				return bytes, nil
			})
		}()
	default:
		// Pool saturated; skip background refresh to protect server concurrency
		slog.Debug("Background cache refresh worker pool full; skipping cycle", "cache_key", cacheKey)
	}
}

// serveStaleIfError is used for both execution failure and no eligible
// providers. Retention past SWR is not permission to return stale on success.
func (h *WeatherHandler) serveStaleIfError(w http.ResponseWriter, r *http.Request, key string, policy cache.PolicyEvaluation, reqCtx decision.RequestContext, caller *auth.Caller, reqID string, duration time.Duration) bool {
	if h.cacheStore == nil || r.Context().Err() != nil || !policy.AllowStaleIfError {
		return false
	}
	entry, err := h.cacheStore.GetEntry(r.Context(), key)
	if err != nil || !policy.CanServeSIE(entry, time.Now()) {
		return false
	}
	var item CachedWeatherPayload
	if err := json.Unmarshal(entry.Value, &item); err != nil || item.Data == nil {
		return false
	}
	if h.cachePolicy == nil || !h.cachePolicy.EvaluatePolicy("weather", item.ProviderID).IsCacheable || !h.validWeatherPayload(item, reqCtx, true) {
		return false
	}
	metrics.DefaultMetrics.CacheStaleHitsTotal.Inc(metrics.Label{Name: "capability", Value: "weather"})
	if caller != nil && h.tracker != nil {
		h.tracker.Record(r.Context(), caller.KeyID, "weather", item.ProviderID, reqID, http.StatusOK, int(duration.Milliseconds()))
	}
	w.Header().Set("Warning", `110 "Response is Stale (Upstream Outage)"`)
	api.WriteSuccess(w, http.StatusOK, item.Data, api.Meta{
		RequestID: reqID, Timestamp: time.Now().UTC(), Provider: item.ProviderID,
		Cached: true, Fallback: true, Attribution: item.Attribution,
	})
	return true
}
