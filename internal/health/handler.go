package health

import (
	"net/http"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/observability/logging"
)

type Checker interface {
	Check() (status string, details map[string]string, ok bool)
}

type Handler struct {
	version   string
	startTime time.Time
	checkers  []Checker
}

func NewHandler(version string, checkers ...Checker) *Handler {
	return &Handler{
		version:   version,
		startTime: time.Now().UTC(),
		checkers:  checkers,
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	reqID := logging.RequestIDFromContext(r.Context())
	uptime := time.Since(h.startTime).String()

	data := map[string]interface{}{
		"status":  "UP",
		"version": h.version,
		"uptime":  uptime,
	}

	api.WriteSuccess(w, http.StatusOK, data, api.Meta{
		RequestID: reqID,
		Timestamp: time.Now().UTC(),
	})
}

func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	reqID := logging.RequestIDFromContext(r.Context())

	allHealthy := true
	details := make(map[string]string)

	for _, c := range h.checkers {
		st, det, ok := c.Check()
		if !ok {
			allHealthy = false
		}
		for k, v := range det {
			details[k] = v
		}
		if !ok && details["state"] == "" {
			details["state"] = st
		}
	}

	if !allHealthy {
		api.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Service is not ready to receive traffic", reqID, true)
		return
	}

	data := map[string]interface{}{
		"status":  "READY",
		"checks":  details,
		"version": h.version,
	}

	api.WriteSuccess(w, http.StatusOK, data, api.Meta{
		RequestID: reqID,
		Timestamp: time.Now().UTC(),
	})
}

func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	reqID := logging.RequestIDFromContext(r.Context())
	data := map[string]string{
		"status": "ALIVE",
	}
	api.WriteSuccess(w, http.StatusOK, data, api.Meta{
		RequestID: reqID,
		Timestamp: time.Now().UTC(),
	})
}
