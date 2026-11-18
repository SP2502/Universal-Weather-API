package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/georesolve"
	"github.com/universal-api/platform/internal/normalization"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/provider"
	"github.com/universal-api/platform/internal/reliability"
	"github.com/universal-api/platform/internal/routing/decision"
	"github.com/universal-api/platform/internal/usage"
	forecast "github.com/universal-api/platform/schemas/forecast/v1"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
)

// CachedForecastPayload retains full, untruncated source data and provenance.
// The per-request points parameter only affects the response representation.
type CachedForecastPayload struct {
	Data         *forecast.ForecastResponse `json:"data"`
	ProviderID   string                     `json:"provider_id"`
	FallbackUsed bool                       `json:"fallback_used"`
	Attribution  []api.AttributionItem      `json:"attribution,omitempty"`
}

type forecastFetch struct {
	Data      *forecast.ForecastResponse
	OriginTTL *time.Duration
}

type ForecastHandler struct {
	registry      *provider.Registry
	decision      *decision.DecisionEngine
	normalizer    *normalization.ForecastNormalizer
	executor      *reliability.Executor
	tracker       usage.UsageTracker
	cacheStore    cache.Store
	cachePolicy   *cache.PolicyEngine
	flight        *cache.SingleFlightGroup
	placeResolver interface {
		Resolve(context.Context, string) (*locv1.Location, error)
	}
}

func NewForecastHandler(reg *provider.Registry, eng *decision.DecisionEngine, tracker usage.UsageTracker) *ForecastHandler {
	e := reliability.NewExecutor(4)
	if reg != nil {
		e.SetHealthEvaluator(reg.HealthEvaluator())
	}
	return &ForecastHandler{registry: reg, decision: eng, normalizer: normalization.NewForecastNormalizer(), executor: e, tracker: tracker, flight: cache.NewSingleFlightGroup()}
}
func (h *ForecastHandler) SetQuotaGate(g reliability.QuotaGate) { h.executor.SetQuotaGate(g) }

// Place resolution uses the same explicitly configured strict geocoder as weather.
// It is not silently enabled: operator account terms and quotas must be reviewed.
func (h *ForecastHandler) SetPlaceResolver(r interface {
	Resolve(context.Context, string) (*locv1.Location, error)
}) {
	h.placeResolver = r
}

// ConfigureCache deliberately remains optional for embedders and tests.
func (h *ForecastHandler) ConfigureCache(store cache.Store, policy *cache.PolicyEngine) {
	h.cacheStore, h.cachePolicy = store, policy
}

func (h *ForecastHandler) cachePermitted(req decision.RequestContext, id string, candidates []provider.Provider) bool {
	if !h.decision.PolicyAllows(req, id) {
		return false
	}
	for _, p := range candidates {
		if p.Manifest().ID == id {
			return true
		}
	}
	return false
}

func (h *ForecastHandler) readCache(ctx context.Context, key string, req decision.RequestContext, candidates []provider.Provider) *CachedForecastPayload {
	if h.cacheStore == nil || h.cachePolicy == nil {
		return nil
	}
	entry, err := h.cacheStore.GetEntry(ctx, key)
	if err != nil || entry == nil || entry.State != cache.StateFresh {
		return nil
	}
	var hit CachedForecastPayload
	if json.Unmarshal(entry.Value, &hit) != nil || hit.ProviderID == "" || hit.Data == nil ||
		normalization.ValidateForecast(hit.Data) != nil || normalization.ValidateRequestedLocation(hit.ProviderID, req.Latitude, req.Longitude, &hit.Data.Location) != nil || !h.cachePermitted(req, hit.ProviderID, candidates) {
		return nil
	}
	// A valid but aged response must retain at least one upcoming forecast.
	// No expired/past-only prediction is returned as fresh solely due to TTL.
	upcoming := false
	now := time.Now().UTC()
	for _, point := range hit.Data.Points {
		if point.ValidAt.After(now) {
			upcoming = true
			break
		}
	}
	if !upcoming {
		return nil
	}
	ok, _ := h.cachePolicy.Evaluate("forecast", hit.ProviderID)
	if !ok {
		return nil
	}
	return &hit
}

func (h *ForecastHandler) GetForecast(w http.ResponseWriter, r *http.Request) {
	h.serveForecast(w, r, false)
}

// GetDailyForecast reuses the SAME validated full hourly payload and upstream
// quota admission. No new vendor daily product or additional call is implied.
func (h *ForecastHandler) GetDailyForecast(w http.ResponseWriter, r *http.Request) {
	h.serveForecast(w, r, true)
}

func (h *ForecastHandler) serveForecast(w http.ResponseWriter, r *http.Request, daily bool) {
	start := time.Now()
	id := logging.RequestIDFromContext(r.Context())
	caller := auth.CallerFromContext(r.Context())
	var parsed weatherRequest
	var pointLimit int
	var err error
	if daily {
		parsed, pointLimit, err = parseDailyForecastRequest(r.URL)
	} else {
		parsed, pointLimit, err = parseForecastRequest(r.URL)
	}
	if err != nil {
		api.WriteError(w, 400, "INVALID_FORECAST_REQUEST", err.Error(), id, false)
		return
	}
	resolvedCountry := ""
	if parsed.latitude == nil && h.placeResolver != nil {
		place, resolveErr := h.placeResolver.Resolve(r.Context(), parsed.location)
		if resolveErr != nil || place == nil {
			status, code := http.StatusBadGateway, "PLACE_RESOLUTION_FAILED"
			if errors.Is(resolveErr, georesolve.ErrAmbiguousLocation) {
				status, code = http.StatusUnprocessableEntity, "AMBIGUOUS_LOCATION"
			}
			api.WriteError(w, status, code, "Unable to resolve an unambiguous location; use explicit coordinates", id, false)
			return
		}
		lat, lon := place.Latitude, place.Longitude
		parsed.latitude, parsed.longitude = &lat, &lon
		resolvedCountry = place.CountryCode
	}
	if parsed.latitude == nil || parsed.longitude == nil {
		api.WriteError(w, 400, "FORECAST_COORDINATES_REQUIRED", "Forecast requires latitude and longitude; enable strict place resolution or call /v1/geocode", id, false)
		return
	}
	req := decision.RequestContext{Capability: "forecast", Product: "hourly_forecast", Latitude: parsed.latitude, Longitude: parsed.longitude, CountryCode: resolvedCountry, PreferredProvider: parsed.provider, PolicyMode: parsed.policy, AllowDegraded: true}
	selected, _, err := h.decision.SelectProviders(r.Context(), req)
	if err != nil || len(selected) == 0 {
		api.WriteError(w, 503, "FORECAST_PROVIDER_NOT_AVAILABLE", "No eligible forecast provider for these coordinates", id, true)
		return
	}
	key := cache.BuildForecastCacheKey(cache.ForecastKeyParams{Latitude: *parsed.latitude, Longitude: *parsed.longitude, Product: "hourly_forecast", PreferredProvider: parsed.provider, PolicyMode: string(parsed.policy)})
	send := func(item *CachedForecastPayload, cached bool) {
		var response any
		if daily {
			summary, summaryErr := normalization.AggregateForecastDays(item.Data, pointLimit)
			if summaryErr != nil {
				api.WriteError(w, 502, "FORECAST_DAILY_INVALID", "Insufficient valid forecast samples", id, true)
				return
			}
			response = summary
		} else {
			copyForecast := *item.Data
			// Do not mutate the cache or share slices: the original 168 points
			// remain available to later callers requesting a larger window.
			if len(copyForecast.Points) > pointLimit {
				copyForecast.Points = copyForecast.Points[:pointLimit]
			}
			response = &copyForecast
		}
		elapsed := int(time.Since(start).Milliseconds())
		if caller != nil && h.tracker != nil {
			h.tracker.Record(r.Context(), caller.KeyID, "forecast", item.ProviderID, id, 200, elapsed)
		}
		api.WriteSuccess(w, 200, response, api.Meta{RequestID: id, Timestamp: time.Now().UTC(), Provider: item.ProviderID, Fallback: item.FallbackUsed, Cached: cached, Attribution: item.Attribution})
	}
	if hit := h.readCache(r.Context(), key, req, selected); hit != nil {
		send(hit, true)
		return
	}
	fetch := func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		ctx = reliability.WithQuotaProduct(ctx, "hourly_forecast")
		result, metadata, err := reliability.ExecuteWithFallback[*forecastFetch](ctx, h.executor, selected, reliability.DefaultRetryPolicy(), func(callCtx context.Context, p provider.Provider) (*forecastFetch, error) {
			upstream, ok := p.(provider.ForecastProvider)
			if !ok {
				return nil, provider.ErrProviderNotFound
			}
			raw, err := upstream.FetchForecast(callCtx, *parsed.latitude, *parsed.longitude)
			if err != nil {
				return nil, err
			}
			if raw == nil || raw.ProviderID != p.Manifest().ID {
				return nil, provider.ErrMalformedResponse
			}
			resp, err := h.normalizer.Normalize(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid canonical forecast", provider.ErrMalformedResponse)
			}
			if err := normalization.ValidateRequestedLocation(p.Manifest().ID, parsed.latitude, parsed.longitude, &resp.Location); err != nil {
				return nil, fmt.Errorf("%w: invalid forecast location", provider.ErrMalformedResponse)
			}
			return &forecastFetch{Data: resp, OriginTTL: raw.FreshTTL}, nil
		})
		if err != nil {
			return nil, err
		}
		item := CachedForecastPayload{Data: result.Data, ProviderID: metadata.ProviderID, FallbackUsed: metadata.FallbackUsed}
		if p, ok := h.registry.Get(metadata.ProviderID); ok {
			m := p.Manifest()
			if m.Attribution.Required {
				item.Attribution = []api.AttributionItem{{Provider: m.ID, Text: m.Attribution.Text, URL: m.Attribution.URL}}
			}
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		if h.cacheStore != nil && h.cachePolicy != nil && h.cachePermitted(req, item.ProviderID, selected) {
			policy := h.cachePolicy.EvaluatePolicy("forecast", item.ProviderID)
			ttl := policy.FreshTTL
			// Origin expiration is an additional strict ceiling. No stale
			// forecast serves after TTL: fresh/stale expiry are identical.
			if result.OriginTTL != nil && *result.OriginTTL < ttl {
				ttl = *result.OriginTTL
			}
			if policy.IsCacheable && ttl > 0 {
				if err := h.cacheStore.SetWithStale(ctx, key, encoded, ttl, ttl); err != nil {
					slog.Warn("forecast cache write failed", "error", err)
				}
			}
		}
		return encoded, nil
	}
	var serialized []byte
	if h.cacheStore != nil && h.cachePolicy != nil {
		// In-process deduplication avoids duplicate upstream calls/quotas.
		serialized, _, err = h.flight.Do(r.Context(), key, 12*time.Second, fetch)
	} else {
		serialized, err = fetch(r.Context())
	}
	if err != nil {
		status, code := http.StatusBadGateway, "FORECAST_UPSTREAM_FAILED"
		if errors.Is(err, context.DeadlineExceeded) {
			status, code = http.StatusGatewayTimeout, "FORECAST_TIMEOUT"
		}
		if caller != nil && h.tracker != nil {
			h.tracker.Record(r.Context(), caller.KeyID, "forecast", "unknown", id, status, int(time.Since(start).Milliseconds()))
		}
		api.WriteError(w, status, code, "All eligible forecast providers failed", id, true)
		return
	}
	var item CachedForecastPayload
	if json.Unmarshal(serialized, &item) != nil || item.Data == nil {
		api.WriteError(w, 502, "FORECAST_INVALID_RESULT", "Invalid gateway forecast result", id, true)
		return
	}
	if !h.cachePermitted(req, item.ProviderID, selected) {
		api.WriteError(w, 503, "PROVIDER_POLICY_CHANGED", "Provider eligibility changed during request", id, true)
		return
	}
	send(&item, false)
}

// parseForecastRequest exposes a bounded number of forecast time steps, NOT
// an assumed hourly duration: some sources publish 3-hourly or irregular data.
// A separate copy of the URL keeps the strict common validation unchanged.
func parseForecastRequest(u *url.URL) (weatherRequest, int, error) {
	var empty weatherRequest
	vals, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return empty, 0, fmt.Errorf("malformed query encoding")
	}
	if err := validateQueryShape(u.RawQuery, vals, "location", "city", "q", "lat", "latitude", "lon", "longitude", "provider", "policy", "points"); err != nil {
		return empty, 0, err
	}
	count := 24
	if input, present := vals["points"]; present {
		count, err = strconv.Atoi(input[0])
		if err != nil || count < 1 || count > normalization.MaxForecastPoints {
			return empty, 0, fmt.Errorf("points must be an integer between 1 and %d", normalization.MaxForecastPoints)
		}
		vals.Del("points")
	}
	clone := *u
	clone.RawQuery = vals.Encode()
	parsed, err := parseWeatherRequest(&clone)
	return parsed, count, err
}

// A day limit is a limit on UTC calendar dates, not number of samples.
// The source query is still parsed by the strict forecast parser.
func parseDailyForecastRequest(u *url.URL) (weatherRequest, int, error) {
	var empty weatherRequest
	vals, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return empty, 0, fmt.Errorf("malformed query encoding")
	}
	if err := validateQueryShape(u.RawQuery, vals, "location", "city", "q", "lat", "latitude", "lon", "longitude", "provider", "policy", "days"); err != nil {
		return empty, 0, err
	}
	days := 7
	if x, present := vals["days"]; present {
		days, err = strconv.Atoi(x[0])
		if err != nil || days < 1 || days > 7 {
			return empty, 0, fmt.Errorf("days must be an integer between 1 and 7")
		}
		vals.Del("days")
	}
	clone := *u
	clone.RawQuery = vals.Encode()
	parsed, _, err := parseForecastRequest(&clone)
	return parsed, days, err
}
