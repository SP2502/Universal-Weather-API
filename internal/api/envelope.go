package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Meta struct {
	RequestID   string            `json:"request_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Provider    string            `json:"provider,omitempty"`
	Cached      bool              `json:"cached"`
	Fallback    bool              `json:"fallback"`
	Sources     []string          `json:"sources,omitempty"`
	Attribution []AttributionItem `json:"attribution,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
}

type AttributionItem struct {
	Provider string `json:"provider"`
	Text     string `json:"text"`
	URL      string `json:"url,omitempty"`
}

type ResponseEnvelope struct {
	Data interface{} `json:"data"`
	Meta Meta        `json:"meta"`
}

type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Status    int    `json:"status"`
	RequestID string `json:"request_id,omitempty"`
	Retryable bool   `json:"retryable"`
}

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, payload interface{}) {
	// Encode before committing the HTTP status. Invalid values (including NaN)
	// must never result in a 200 response with an empty or truncated JSON body.
	encoded, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to serialize JSON response", "error", err)
		status = http.StatusInternalServerError
		encoded = []byte(`{"error":{"code":"SERIALIZATION_ERROR","message":"Failed to serialize response","status":500,"retryable":false}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoded = append(encoded, '\n')
	_, _ = w.Write(encoded)
}

func WriteSuccess(w http.ResponseWriter, status int, data interface{}, meta Meta) {
	if meta.Timestamp.IsZero() {
		meta.Timestamp = time.Now().UTC()
	}
	WriteJSON(w, status, ResponseEnvelope{
		Data: data,
		Meta: meta,
	})
}

func WriteError(w http.ResponseWriter, status int, code, message, requestID string, retryable bool) {
	WriteJSON(w, status, ErrorEnvelope{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Status:    status,
			RequestID: requestID,
			Retryable: retryable,
		},
	})
}
