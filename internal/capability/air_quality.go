package capability

import (
	"context"
	"errors"
	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	air "github.com/universal-api/platform/schemas/airquality/v1"
	"net/http"
	"time"
)

// AirQualityHandler is exclusively a model-forecast product, not a measured
// station observation/AQI product. No assumptions about missing pollutants.
type AirQualityHandler struct {
	registry *provider.Registry
	decision *decision.DecisionEngine
	executor *reliability.Executor
	tracker  usage.UsageTracker
}

func NewAirQualityHandler(reg *provider.Registry, eng *decision.DecisionEngine, t usage.UsageTracker) *AirQualityHandler {
	e := reliability.NewExecutor(4)
	if reg != nil {
		e.SetHealthEvaluator(reg.HealthEvaluator())
	}
	return &AirQualityHandler{registry: reg, decision: eng, executor: e, tracker: t}
}
func (h *AirQualityHandler) SetQuotaGate(g reliability.QuotaGate) { h.executor.SetQuotaGate(g) }
func (h *AirQualityHandler) GetAirQuality(w http.ResponseWriter, r *http.Request) {
	id := logging.RequestIDFromContext(r.Context())
	start := time.Now()
	parsed, err := parseWeatherRequest(r.URL)
	if err != nil {
		api.WriteError(w, 400, "INVALID_AIR_QUALITY_REQUEST", err.Error(), id, false)
		return
	}
	if parsed.latitude == nil || parsed.longitude == nil {
		api.WriteError(w, 400, "AIR_QUALITY_COORDINATES_REQUIRED", "Resolve place names through /v1/geocode before requesting air quality", id, false)
		return
	}
	req := decision.RequestContext{Capability: "air_quality", Product: "air_quality_forecast", Latitude: parsed.latitude, Longitude: parsed.longitude, PreferredProvider: parsed.provider, PolicyMode: parsed.policy, AllowDegraded: true}
	selected, _, err := h.decision.SelectProviders(r.Context(), req)
	if err != nil || len(selected) == 0 {
		api.WriteError(w, 503, "AIR_QUALITY_PROVIDER_NOT_AVAILABLE", "No eligible modeled air-quality provider", id, true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	ctx = reliability.WithQuotaProduct(ctx, "air_quality_forecast")
	result, meta, err := reliability.ExecuteWithFallback[*air.Response](ctx, h.executor, selected, reliability.DefaultRetryPolicy(), func(attempt context.Context, p provider.Provider) (*air.Response, error) {
		upstream, ok := p.(provider.AirQualityProvider)
		if !ok {
			return nil, provider.ErrProviderNotFound
		}
		raw, e := upstream.FetchAirQuality(attempt, *parsed.latitude, *parsed.longitude)
		if e != nil {
			return nil, e
		}
		if raw == nil || raw.ProviderID != p.Manifest().ID {
			return nil, provider.ErrMalformedResponse
		}
		return normalization.NormalizeAirQuality(raw)
	})
	status := 200
	if err != nil {
		status = 502
		if errors.Is(err, context.DeadlineExceeded) {
			status = 504
		}
	}
	if caller := auth.CallerFromContext(r.Context()); caller != nil && h.tracker != nil {
		p := "unknown"
		if meta != nil {
			p = meta.ProviderID
		}
		if p == "" {
			p = "unknown"
		}
		h.tracker.Record(r.Context(), caller.KeyID, "air_quality", p, id, status, int(time.Since(start).Milliseconds()))
	}
	if err != nil {
		api.WriteError(w, status, "AIR_QUALITY_UPSTREAM_FAILED", "All eligible air-quality providers failed", id, true)
		return
	}
	var attribution []api.AttributionItem
	if p, ok := h.registry.Get(meta.ProviderID); ok {
		m := p.Manifest()
		if m.Attribution.Required {
			attribution = append(attribution, api.AttributionItem{Provider: m.ID, Text: m.Attribution.Text, URL: m.Attribution.URL})
		}
	}
	api.WriteSuccess(w, 200, result, api.Meta{RequestID: id, Timestamp: time.Now().UTC(), Provider: meta.ProviderID, Fallback: meta.FallbackUsed, Attribution: attribution})
}
