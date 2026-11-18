package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	SpanContextKey contextKey = "uap_trace_span"
)

// TraceID represents a 16-byte (32 hex character) unique trace identifier.
type TraceID [16]byte

func (t TraceID) String() string {
	return hex.EncodeToString(t[:])
}

func (t TraceID) IsZero() bool {
	return t == TraceID{}
}

// SpanID represents an 8-byte (16 hex character) span identifier.
type SpanID [8]byte

func (s SpanID) String() string {
	return hex.EncodeToString(s[:])
}

func (s SpanID) IsZero() bool {
	return s == SpanID{}
}

// NewTraceID generates a cryptographically random 16-byte TraceID.
func NewTraceID() TraceID {
	var tid TraceID
	_, _ = rand.Read(tid[:])
	return tid
}

// NewSpanID generates a cryptographically random 8-byte SpanID.
func NewSpanID() SpanID {
	var sid SpanID
	_, _ = rand.Read(sid[:])
	return sid
}

// TraceParent represents a W3C traceparent header: 00-<trace_id>-<parent_id>-<flags>
type TraceParent struct {
	Version  string
	TraceID  TraceID
	ParentID SpanID
	Flags    string
}

func (tp TraceParent) String() string {
	version := tp.Version
	if version == "" {
		version = "00"
	}
	flags := tp.Flags
	if flags == "" {
		flags = "01" // sampled
	}
	return fmt.Sprintf("%s-%s-%s-%s", version, tp.TraceID.String(), tp.ParentID.String(), flags)
}

// ParseTraceParent parses a W3C traceparent header value.
func ParseTraceParent(header string) (*TraceParent, error) {
	header = strings.TrimSpace(header)
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid traceparent header format: %s", header)
	}

	version := parts[0]
	traceIDHex := parts[1]
	parentIDHex := parts[2]
	flags := parts[3]

	if len(traceIDHex) != 32 || len(parentIDHex) != 16 {
		return nil, fmt.Errorf("invalid traceparent segment lengths")
	}

	traceBytes, err := hex.DecodeString(traceIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid trace ID hex: %w", err)
	}

	spanBytes, err := hex.DecodeString(parentIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid parent span ID hex: %w", err)
	}

	var tid TraceID
	copy(tid[:], traceBytes)

	var sid SpanID
	copy(sid[:], spanBytes)

	return &TraceParent{
		Version:  version,
		TraceID:  tid,
		ParentID: sid,
		Flags:    flags,
	}, nil
}

// SpanStatus represents status of span execution.
type SpanStatus string

const (
	StatusUnset SpanStatus = "UNSET"
	StatusOK    SpanStatus = "OK"
	StatusError SpanStatus = "ERROR"
)

// Span represents a single operation within a trace.
type Span struct {
	mu         sync.RWMutex
	name       string
	traceID    TraceID
	spanID     SpanID
	parentID   SpanID
	startTime  time.Time
	endTime    time.Time
	status     SpanStatus
	statusDesc string
	attributes map[string]string
}

func (s *Span) TraceID() string {
	return s.traceID.String()
}

func (s *Span) SpanID() string {
	return s.spanID.String()
}

func (s *Span) ParentID() string {
	return s.parentID.String()
}

func (s *Span) Name() string {
	return s.name
}

func (s *Span) StartTime() time.Time {
	return s.startTime
}

func (s *Span) EndTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endTime
}

func (s *Span) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.endTime.IsZero() {
		return time.Since(s.startTime)
	}
	return s.endTime.Sub(s.startTime)
}

func (s *Span) Status() SpanStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Span) SetStatus(status SpanStatus, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.statusDesc = description
}

func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attributes == nil {
		s.attributes = make(map[string]string)
	}
	s.attributes[key] = value
}

func (s *Span) GetAttribute(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.attributes == nil {
		return ""
	}
	return s.attributes[key]
}

func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endTime.IsZero() {
		s.endTime = time.Now().UTC()
	}
}

// Tracer creates and propagates spans across contexts.
type Tracer struct {
	serviceName string
}

func NewTracer(serviceName string) *Tracer {
	if serviceName == "" {
		serviceName = "universal-api-platform"
	}
	return &Tracer{
		serviceName: serviceName,
	}
}

var DefaultTracer = NewTracer("universal-api-platform")

// StartSpan creates a new span. If the context contains a parent span or trace ID, it links to it.
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	var traceID TraceID
	var parentID SpanID

	if parentSpan := SpanFromContext(ctx); parentSpan != nil {
		traceID = parentSpan.traceID
		parentID = parentSpan.spanID
	} else if tp, ok := ctx.Value(contextKey("traceparent_parsed")).(*TraceParent); ok && tp != nil {
		traceID = tp.TraceID
		parentID = tp.ParentID
	} else {
		traceID = NewTraceID()
	}

	spanID := NewSpanID()

	span := &Span{
		name:       name,
		traceID:    traceID,
		spanID:     spanID,
		parentID:   parentID,
		startTime:  time.Now().UTC(),
		status:     StatusUnset,
		attributes: make(map[string]string),
	}

	span.SetAttribute("service.name", t.serviceName)

	newCtx := context.WithValue(ctx, SpanContextKey, span)
	return newCtx, span
}

// Context helpers
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if span, ok := ctx.Value(SpanContextKey).(*Span); ok {
		return span
	}
	return nil
}

func TraceIDFromContext(ctx context.Context) string {
	if span := SpanFromContext(ctx); span != nil {
		return span.TraceID()
	}
	if tid, ok := ctx.Value(contextKey("trace_id")).(string); ok {
		return tid
	}
	return ""
}

func SpanIDFromContext(ctx context.Context) string {
	if span := SpanFromContext(ctx); span != nil {
		return span.SpanID()
	}
	return ""
}

func WithTraceParent(ctx context.Context, tp *TraceParent) context.Context {
	return context.WithValue(ctx, contextKey("traceparent_parsed"), tp)
}
