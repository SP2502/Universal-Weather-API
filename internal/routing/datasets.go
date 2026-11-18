package routing

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/observability/logging"
)

type datasetRoute struct {
	scope   string
	handler http.HandlerFunc
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

type datasetResult struct {
	Data json.RawMessage `json:"data"`
	Meta json.RawMessage `json:"meta"`
}

type datasetFailure struct {
	Status int             `json:"status"`
	Error  api.ErrorDetail `json:"error"`
}

func validSharedLocation(values url.Values) bool {
	for _, key := range []string{"lat", "lon", "location", "q"} {
		if len(values[key]) > 1 {
			return false
		}
	}
	hasLat, hasLon := values.Has("lat"), values.Has("lon")
	if hasLat != hasLon {
		return false
	}
	names := 0
	for _, key := range []string{"location", "q"} {
		if !values.Has(key) {
			continue
		}
		name := strings.TrimSpace(values.Get(key))
		if name == "" || !utf8.ValidString(name) || len([]rune(name)) > 500 {
			return false
		}
		for _, char := range name {
			if unicode.IsControl(char) {
				return false
			}
		}
		names++
	}
	if (hasLat && names != 0) || (!hasLat && names != 1) {
		return false
	}
	if hasLat {
		lat, latErr := strconv.ParseFloat(strings.TrimSpace(values.Get("lat")), 64)
		lon, lonErr := strconv.ParseFloat(strings.TrimSpace(values.Get("lon")), 64)
		if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return false
		}
	}
	return true
}

// datasetsHandler runs each product's existing policy, quota, cache, fallback,
// normalization and usage path. A caller must hold every requested scope before
// any product is executed, so a partial response cannot bypass authorization.
func datasetsHandler(routes map[string]datasetRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := logging.RequestIDFromContext(r.Context())
		raw := r.URL.Query()["datasets"]
		if len(raw) != 1 || strings.TrimSpace(raw[0]) == "" {
			api.WriteError(w, 400, "INVALID_DATASETS", "Provide one comma-separated datasets parameter", requestID, false)
			return
		}
		names := strings.Split(raw[0], ",")
		if len(names) == 0 || len(names) > 8 {
			api.WriteError(w, 400, "INVALID_DATASETS", "Request between one and eight datasets", requestID, false)
			return
		}
		caller := auth.CallerFromContext(r.Context())
		if caller == nil {
			api.WriteError(w, 401, "UNAUTHORIZED", "Unauthenticated request", requestID, false)
			return
		}
		seen := make(map[string]bool, len(names))
		for i, name := range names {
			name = strings.TrimSpace(name)
			route, ok := routes[name]
			if !ok || seen[name] {
				api.WriteError(w, 400, "INVALID_DATASETS", "Unknown or duplicate dataset", requestID, false)
				return
			}
			seen[name], names[i] = true, name
			allowed := false
			for _, scope := range caller.Scopes {
				if scope == "*" || scope == route.scope {
					allowed = true
					break
				}
			}
			if !allowed {
				api.WriteError(w, 403, "FORBIDDEN", "Insufficient scope for requested datasets", requestID, false)
				return
			}
		}
		sharedQuery, queryErr := url.ParseQuery(r.URL.RawQuery)
		if queryErr != nil || !validSharedLocation(sharedQuery) {
			api.WriteError(w, 400, "INVALID_LOCATION", "Provide one valid place name or a latitude and longitude pair", requestID, false)
			return
		}
		for key := range sharedQuery {
			prefix, option, scoped := strings.Cut(key, ".")
			if !scoped {
				continue
			}
			if !seen[prefix] || option == "" || strings.Contains(option, ".") || option == "datasets" || option == "lat" || option == "lon" || option == "location" || option == "q" || sharedQuery.Has(option) {
				api.WriteError(w, 400, "INVALID_DATASET_OPTION", "Use a requested dataset name and an unambiguous product option", requestID, false)
				return
			}
		}
		results := make(map[string]datasetResult, len(names))
		failures := make(map[string]datasetFailure)
		for _, name := range names {
			if r.Context().Err() != nil {
				failures[name] = datasetFailure{Status: http.StatusGatewayTimeout, Error: api.ErrorDetail{
					Code: "DATASET_TIMEOUT", Message: "Request ended before this dataset could be fetched", Status: http.StatusGatewayTimeout, Retryable: true,
				}}
				continue
			}
			productRequest := r.Clone(r.Context())
			productURL := *r.URL
			query := make(url.Values, len(sharedQuery))
			for key, values := range sharedQuery {
				prefix, option, scoped := strings.Cut(key, ".")
				if key == "datasets" {
					continue
				}
				if scoped {
					if prefix == name {
						query[option] = append([]string(nil), values...)
					}
					continue
				}
				query[key] = append([]string(nil), values...)
			}
			coordinates := query.Has("lat") || query.Has("lon")
			location, placeQuery := strings.TrimSpace(query.Get("location")), strings.TrimSpace(query.Get("q"))
			if name == "geocode" && location != "" && placeQuery == "" && !coordinates {
				query.Set("q", location)
				query.Del("location")
			} else if name != "geocode" && placeQuery != "" && location == "" && !coordinates {
				query.Set("location", placeQuery)
				query.Del("q")
			}
			productURL.RawQuery = query.Encode()
			productRequest.URL = &productURL
			buffer := &bufferedResponse{header: make(http.Header)}
			routes[name].handler.ServeHTTP(buffer, productRequest)
			if buffer.status >= 200 && buffer.status < 300 {
				var envelope struct {
					Data json.RawMessage `json:"data"`
					Meta json.RawMessage `json:"meta"`
				}
				if json.Unmarshal(buffer.body.Bytes(), &envelope) == nil && len(envelope.Data) > 0 {
					results[name] = datasetResult{Data: envelope.Data, Meta: envelope.Meta}
					continue
				}
				buffer.status = 502
			}
			failure := datasetFailure{Status: buffer.status, Error: api.ErrorDetail{Code: "DATASET_UNAVAILABLE", Message: "Dataset could not be delivered", Status: buffer.status}}
			var envelope api.ErrorEnvelope
			if json.Unmarshal(buffer.body.Bytes(), &envelope) == nil && envelope.Error.Code != "" {
				failure.Error = envelope.Error
			}
			failures[name] = failure
		}
		status := http.StatusOK
		if len(failures) > 0 {
			status = http.StatusMultiStatus
		}
		if len(results) == 0 {
			status = http.StatusServiceUnavailable
		}
		api.WriteJSON(w, status, struct {
			Data   map[string]datasetResult  `json:"data"`
			Errors map[string]datasetFailure `json:"errors,omitempty"`
			Meta   struct {
				RequestID string    `json:"request_id"`
				Timestamp time.Time `json:"timestamp"`
				Partial   bool      `json:"partial"`
			} `json:"meta"`
		}{Data: results, Errors: failures, Meta: struct {
			RequestID string    `json:"request_id"`
			Timestamp time.Time `json:"timestamp"`
			Partial   bool      `json:"partial"`
		}{requestID, time.Now().UTC(), len(failures) > 0 && len(results) > 0}})
	}
}
