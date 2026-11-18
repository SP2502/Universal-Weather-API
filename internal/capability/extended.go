package capability

import (
	"context"
	"errors"
	"fmt"
	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	"net/http"
	"net/url"
	"time"
)

type ExtendedHandler struct {
	registry *provider.Registry
	decision *decision.DecisionEngine
	executor *reliability.Executor
	tracker  usage.UsageTracker
}

func NewExtendedHandler(reg *provider.Registry, eng *decision.DecisionEngine, t usage.UsageTracker) *ExtendedHandler {
	e := reliability.NewExecutor(4)
	if reg != nil {
		e.SetHealthEvaluator(reg.HealthEvaluator())
	}
	return &ExtendedHandler{registry: reg, decision: eng, executor: e, tracker: t}
}
func (h *ExtendedHandler) SetQuotaGate(g reliability.QuotaGate) { h.executor.SetQuotaGate(g) }
func (h *ExtendedHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, "history", "historical_reanalysis")
}
func (h *ExtendedHandler) GetAstronomy(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, "astronomy", "astronomy")
}
func (h *ExtendedHandler) GetMarine(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, "marine", "marine_forecast")
}
func (h *ExtendedHandler) GetAlerts(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, "alerts", "active_alerts")
}

func (h *ExtendedHandler) serve(w http.ResponseWriter, r *http.Request, capability, product string) {
	id := logging.RequestIDFromContext(r.Context())
	start := time.Now()
	parsed, from, to, date, err := parseExtendedRequest(r.URL, capability)
	if err != nil {
		api.WriteError(w, 400, "INVALID_"+capability+"_REQUEST", err.Error(), id, false)
		return
	}
	if parsed.latitude == nil || parsed.longitude == nil {
		api.WriteError(w, 400, "COORDINATES_REQUIRED", "Use latitude and longitude, or resolve a place through /v1/geocode", id, false)
		return
	}
	req := decision.RequestContext{Capability: capability, Product: product, Latitude: parsed.latitude, Longitude: parsed.longitude, PreferredProvider: parsed.provider, PolicyMode: parsed.policy, AllowDegraded: true}
	selected, _, err := h.decision.SelectProviders(r.Context(), req)
	if err != nil || len(selected) == 0 {
		api.WriteError(w, 503, "PROVIDER_NOT_AVAILABLE", "No eligible provider for this location and product", id, true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	ctx = reliability.WithQuotaProduct(ctx, product)
	data, meta, err := reliability.ExecuteWithFallback[any](ctx, h.executor, selected, reliability.DefaultRetryPolicy(), func(call context.Context, p provider.Provider) (any, error) {
		var raw *provider.RawExtendedResponse
		var e error
		switch capability {
		case "history":
			x, ok := p.(provider.HistoryProvider)
			if !ok {
				return nil, provider.ErrProviderNotFound
			}
			raw, e = x.FetchHistory(call, *parsed.latitude, *parsed.longitude, from, to)
		case "astronomy":
			x, ok := p.(provider.AstronomyProvider)
			if !ok {
				return nil, provider.ErrProviderNotFound
			}
			raw, e = x.FetchAstronomy(call, *parsed.latitude, *parsed.longitude, date)
		case "marine":
			x, ok := p.(provider.MarineProvider)
			if !ok {
				return nil, provider.ErrProviderNotFound
			}
			raw, e = x.FetchMarine(call, *parsed.latitude, *parsed.longitude)
		case "alerts":
			x, ok := p.(provider.AlertsProvider)
			if !ok {
				return nil, provider.ErrProviderNotFound
			}
			raw, e = x.FetchAlerts(call, *parsed.latitude, *parsed.longitude)
		default:
			return nil, provider.ErrProviderNotFound
		}
		if e != nil {
			return nil, e
		}
		if raw == nil || raw.ProviderID != p.Manifest().ID {
			return nil, provider.ErrMalformedResponse
		}
		switch capability {
		case "history":
			return normalization.NormalizeHistory(raw, from, to)
		case "astronomy":
			return normalization.NormalizeAstronomy(raw, date)
		case "marine":
			return normalization.NormalizeMarine(raw, *parsed.latitude, *parsed.longitude)
		case "alerts":
			return normalization.NormalizeAlerts(raw, *parsed.latitude, *parsed.longitude)
		}
		return nil, provider.ErrProviderNotFound
	})
	status := 200
	if err != nil {
		status = 502
		if errors.Is(err, context.DeadlineExceeded) {
			status = 504
		}
	}
	if caller := auth.CallerFromContext(r.Context()); caller != nil && h.tracker != nil {
		src := "unknown"
		if meta != nil {
			src = meta.ProviderID
		}
		h.tracker.Record(r.Context(), caller.KeyID, capability, src, id, status, int(time.Since(start).Milliseconds()))
	}
	if err != nil {
		api.WriteError(w, status, "UPSTREAM_"+capability+"_FAILED", "Eligible upstream sources failed or returned invalid data", id, true)
		return
	}
	attrs := []api.AttributionItem{}
	if p, ok := h.registry.Get(meta.ProviderID); ok {
		a := p.Manifest().Attribution
		if a.Required {
			attrs = append(attrs, api.AttributionItem{Provider: meta.ProviderID, Text: a.Text, URL: a.URL})
		}
	}
	api.WriteSuccess(w, 200, data, api.Meta{RequestID: id, Provider: meta.ProviderID, Fallback: meta.FallbackUsed, Attribution: attrs})
}
func parseExtendedRequest(u *url.URL, capability string) (weatherRequest, time.Time, time.Time, string, error) {
	var empty weatherRequest
	vals, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return empty, time.Time{}, time.Time{}, "", fmt.Errorf("malformed query encoding")
	}
	allowed := []string{"lat", "latitude", "lon", "longitude", "provider", "policy"}
	if capability == "history" {
		allowed = append(allowed, "start_date", "end_date")
	}
	if capability == "astronomy" {
		allowed = append(allowed, "date")
	}
	if err = validateQueryShape(u.RawQuery, vals, allowed...); err != nil {
		return empty, time.Time{}, time.Time{}, "", err
	}
	var from, to time.Time
	date := ""
	if capability == "history" {
		start, hasStart, e := oneOf(vals, "start_date")
		if e != nil || !hasStart {
			return empty, from, to, date, fmt.Errorf("start_date is required")
		}
		end, hasEnd, e := oneOf(vals, "end_date")
		if e != nil || !hasEnd {
			return empty, from, to, date, fmt.Errorf("end_date is required")
		}
		from, e = time.Parse("2006-01-02", start)
		if e != nil || from.Format("2006-01-02") != start {
			return empty, from, to, date, fmt.Errorf("invalid start_date")
		}
		to, e = time.Parse("2006-01-02", end)
		if e != nil || to.Format("2006-01-02") != end {
			return empty, from, to, date, fmt.Errorf("invalid end_date")
		}
		if from.Before(time.Date(1940, 1, 1, 0, 0, 0, 0, time.UTC)) || to.Before(from) || to.Sub(from) > 6*24*time.Hour || to.After(time.Now().UTC().AddDate(0, 0, -7)) {
			return empty, from, to, date, fmt.Errorf("history range must be 1 to 7 UTC dates, at least seven days old, from 1940")
		}
		vals.Del("start_date")
		vals.Del("end_date")
	}
	if capability == "astronomy" {
		date, _, err = oneOf(vals, "date")
		if err != nil || date == "" {
			return empty, from, to, date, fmt.Errorf("explicit date YYYY-MM-DD is required")
		}
		t, e := time.Parse("2006-01-02", date)
		if e != nil || t.Format("2006-01-02") != date || t.Year() < 1900 || t.Year() > 2100 {
			return empty, from, to, date, fmt.Errorf("invalid astronomy date")
		}
		vals.Del("date")
	}
	clone := *u
	clone.RawQuery = vals.Encode()
	parsed, err := parseWeatherRequest(&clone)
	return parsed, from, to, date, err
}
