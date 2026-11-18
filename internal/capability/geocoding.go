package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/cache"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
	"github.com/universal-api/platform/internal/observability/tracing"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

type GeocodingHandler struct {
	registry       *provider.Registry
	decisionEngine *decision.DecisionEngine
	normalizer     *normalization.GeocodingNormalizer
	executor       *reliability.Executor
	retryPolicy    reliability.RetryPolicy
	tracker        usage.UsageTracker
	cacheStore     cache.Store
	cacheAllowed   map[string]bool
	cacheTTL       time.Duration
}

func NewGeocodingHandler(
	registry *provider.Registry,
	decisionEngine *decision.DecisionEngine,
	normalizer *normalization.GeocodingNormalizer,
	tracker usage.UsageTracker,
) *GeocodingHandler {
	executor := reliability.NewExecutor(4)
	if registry != nil {
		executor.SetHealthEvaluator(registry.HealthEvaluator())
	}
	if decisionEngine == nil && registry != nil {
		decisionEngine = decision.NewDecisionEngine(registry, registry.HealthEvaluator())
	}

	return &GeocodingHandler{
		registry:       registry,
		decisionEngine: decisionEngine,
		normalizer:     normalizer,
		executor:       executor,
		retryPolicy:    reliability.DefaultRetryPolicy(),
		tracker:        tracker,
	}
}

// ConfigureCache permits geocoding cache only for explicitly authorized
// upstream provider IDs. It is disabled by default; terms are operator-owned.
func (h *GeocodingHandler) ConfigureCache(store cache.Store, permitted []string, ttl time.Duration) {
	h.cacheStore = store
	h.cacheAllowed = make(map[string]bool, len(permitted))
	for _, id := range permitted {
		id = strings.TrimSpace(id)
		if id != "" {
			h.cacheAllowed[id] = true
		}
	}
	if ttl <= 0 || ttl > time.Hour {
		ttl = 10 * time.Minute
	}
	h.cacheTTL = ttl
}

type geocodeCacheItem struct {
	Data     *locv1.GeocodeResponse `json:"data"`
	Provider string                 `json:"provider"`
	Fallback bool                   `json:"fallback"`
}

func (h *GeocodingHandler) validCachePayload(c geocodeCacheItem, req decision.RequestContext, candidates []provider.Provider) bool {
	if c.Data == nil || c.Data.SchemaVersion != "location.v1" || c.Data.Results == nil || !h.cacheAllowed[c.Provider] || !h.decisionEngine.PolicyAllows(req, c.Provider) {
		return false
	}
	found := false
	for _, p := range candidates {
		if p.Manifest().ID == c.Provider {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	for _, l := range c.Data.Results {
		if normalization.ValidateLocation(&l) != nil {
			return false
		}
	}
	return true
}

// fetchCanonicalGeocode prevents invalid coordinates/response shape from
// counting as success and gives an alternative geocoder a chance to recover.
// SetQuotaGate configures per-attempt admission for geocoding providers.
func (h *GeocodingHandler) SetQuotaGate(gate reliability.QuotaGate) { h.executor.SetQuotaGate(gate) }

func (h *GeocodingHandler) fetchCanonicalGeocode(ctx context.Context, p provider.Provider, validated geocodeRequest) (*locv1.GeocodeResponse, error) {
	gp, ok := p.(provider.GeocodingProvider)
	if !ok {
		return nil, fmt.Errorf("%w: provider lacks geocoding capability", provider.ErrMalformedResponse)
	}
	var raw *provider.RawGeocodeResponse
	var err error
	if validated.forward {
		raw, err = gp.ForwardGeocode(ctx, provider.ForwardGeocodeRequest{Query: validated.query, CountryCode: validated.country, Limit: validated.limit})
	} else {
		raw, err = gp.ReverseGeocode(ctx, provider.ReverseGeocodeRequest{Latitude: validated.latitude, Longitude: validated.longitude})
	}
	if err != nil {
		return nil, err
	}
	if raw == nil || raw.ProviderID != p.Manifest().ID {
		return nil, fmt.Errorf("%w: missing or inconsistent provider identity", provider.ErrMalformedResponse)
	}
	canonical, err := h.normalizer.Normalize(raw)
	if err != nil {
		metrics.DefaultMetrics.NormalizationErrorsTotal.Inc(metrics.Label{Name: "capability", Value: "geocode"}, metrics.Label{Name: "provider", Value: p.Manifest().ID})
		return nil, fmt.Errorf("%w: geocoding canonical validation: %v", provider.ErrMalformedResponse, err)
	}
	return canonical, nil
}

func (h *GeocodingHandler) Geocode(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := logging.RequestIDFromContext(r.Context())
	caller := auth.CallerFromContext(r.Context())

	validated, validationErr := parseGeocodeRequest(r.URL)
	if validationErr != nil {
		api.WriteError(w, http.StatusBadRequest, "GEOCODING_INVALID_REQUEST", validationErr.Error(), reqID, false)
		return
	}
	preferredProvider, policyMode, countryCode := validated.provider, validated.policy, validated.country
	isForward := validated.forward

	// Validate before selecting providers to avoid 503 masking malformed input.
	product := "reverse_geocoding"
	if isForward {
		product = "forward_geocoding"
	}
	reqCtx := decision.RequestContext{
		Capability: "geocode", Product: product, PreferredProvider: preferredProvider,
		CountryCode: countryCode, AllowDegraded: true, PolicyMode: policyMode,
	}
	if !isForward {
		reqCtx.Latitude = &validated.latitude
		reqCtx.Longitude = &validated.longitude
	}
	providers, explanation, err := h.decisionEngine.SelectProviders(r.Context(), reqCtx)
	if err != nil || len(providers) == 0 {
		metrics.DefaultMetrics.ProviderRoutingFailuresTotal.Inc(
			metrics.Label{Name: "capability", Value: "geocode"},
			metrics.Label{Name: "reason", Value: "no_eligible_providers"},
		)
		api.WriteError(w, http.StatusServiceUnavailable, "PROVIDER_NOT_AVAILABLE", "No eligible providers available for geocode", reqID, true)
		return
	}
	// A warmed entry remains conditional on current routing-policy eligibility.
	// Keys separate response limit, preferred provider, routing mode and
	// query/coordinates; a provider cannot bypass caller constraints via cache.
	cacheKey := ""
	if h.cacheStore != nil && len(h.cacheAllowed) > 0 {
		mode := "reverse"
		if isForward {
			mode = "forward"
		}
		cacheKey = cache.BuildGeocodeV2Key(cache.GeocodeV2KeyParams{Query: validated.query, Country: validated.country, Latitude: validated.latitude, Longitude: validated.longitude, Mode: mode, Limit: validated.limit, PreferredProvider: preferredProvider, Policy: string(policyMode)})
		if entry, cacheErr := h.cacheStore.GetEntry(r.Context(), cacheKey); cacheErr == nil && entry != nil && entry.State == cache.StateFresh {
			var hit geocodeCacheItem
			if json.Unmarshal(entry.Value, &hit) == nil && h.validCachePayload(hit, reqCtx, providers) {
				var attributions []api.AttributionItem
				if p, ok := h.registry.Get(hit.Provider); ok {
					m := p.Manifest()
					if m.Attribution.Required {
						attributions = append(attributions, api.AttributionItem{Provider: m.ID, Text: m.Attribution.Text, URL: m.Attribution.URL})
					}
				}
				if caller != nil && h.tracker != nil {
					h.tracker.Record(r.Context(), caller.KeyID, "geocode", hit.Provider, reqID, 200, int(time.Since(start).Milliseconds()))
				}
				api.WriteSuccess(w, 200, hit.Data, api.Meta{RequestID: reqID, Timestamp: time.Now().UTC(), Provider: hit.Provider, Cached: true, Fallback: hit.Fallback, Attribution: attributions})
				return
			}
		}
	}
	metrics.DefaultMetrics.ProviderRoutingDecisionsTotal.Inc(
		metrics.Label{Name: "capability", Value: "geocode"},
		metrics.Label{Name: "policy", Value: string(explanation.PolicyMode)},
		metrics.Label{Name: "selected_provider", Value: explanation.SelectedProvider},
	)

	ctx, span := tracing.DefaultTracer.StartSpan(r.Context(), "Capability Geocode")
	defer span.End()
	span.SetAttribute("capability", "geocode")
	span.SetAttribute("routing.selected_provider", explanation.SelectedProvider)
	span.SetAttribute("routing.policy_mode", string(explanation.PolicyMode))

	if isForward {
		span.SetAttribute("geocode.mode", "forward")
	} else {
		span.SetAttribute("geocode.mode", "reverse")
	}

	// Execute with Reliability Engine using the dynamically ordered provider list
	// Geocoding has an independent bounded budget below the server write timeout.
	boundedCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	boundedCtx = reliability.WithQuotaProduct(boundedCtx, product)
	canonicalData, execMeta, err := reliability.ExecuteWithFallback[*locv1.GeocodeResponse](
		boundedCtx, h.executor, providers, h.retryPolicy,
		func(opCtx context.Context, p provider.Provider) (*locv1.GeocodeResponse, error) {
			return h.fetchCanonicalGeocode(opCtx, p, validated)
		},
	)

	durationMs := int(time.Since(start).Milliseconds())

	if err != nil {
		span.SetStatus(tracing.StatusError, err.Error())
		slog.WarnContext(r.Context(), "All geocoding providers failed", "request_id", reqID, "error", err.Error())
		status, code, message := http.StatusBadGateway, "GEOCODING_PROVIDER_ERROR", "All upstream geocoding providers failed to respond"
		if errors.Is(err, context.DeadlineExceeded) {
			status, code, message = http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "Upstream geocoding request timed out"
		}
		if caller != nil && h.tracker != nil {
			h.tracker.Record(r.Context(), caller.KeyID, "geocode", "unknown", reqID, status, durationMs)
		}
		api.WriteError(w, status, code, message, reqID, true)
		return
	}

	span.SetStatus(tracing.StatusOK, "OK")
	span.SetAttribute("provider.selected", execMeta.ProviderID)

	if caller != nil && h.tracker != nil {
		h.tracker.Record(r.Context(), caller.KeyID, "geocode", execMeta.ProviderID, reqID, http.StatusOK, durationMs)
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

	if cacheKey != "" && h.cacheAllowed[execMeta.ProviderID] && len(canonicalData.Results) > 0 {
		record := geocodeCacheItem{Data: canonicalData, Provider: execMeta.ProviderID, Fallback: execMeta.FallbackUsed}
		if encoded, encErr := json.Marshal(record); encErr == nil {
			if setErr := h.cacheStore.Set(r.Context(), cacheKey, encoded, h.cacheTTL); setErr != nil {
				slog.WarnContext(r.Context(), "geocode cache write unavailable", "error", setErr)
			}
		}
	}
	meta := api.Meta{
		RequestID:   reqID,
		Timestamp:   time.Now().UTC(),
		Provider:    execMeta.ProviderID,
		Cached:      false,
		Fallback:    execMeta.FallbackUsed,
		Attribution: attributions,
	}

	api.WriteSuccess(w, http.StatusOK, canonicalData, meta)
}
