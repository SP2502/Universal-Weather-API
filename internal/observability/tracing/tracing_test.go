package tracing

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTracing_IDs(t *testing.T) {
	tid := NewTraceID()
	if tid.IsZero() || len(tid.String()) != 32 {
		t.Fatalf("expected 32-char non-zero hex string for TraceID, got %s", tid.String())
	}

	sid := NewSpanID()
	if sid.IsZero() || len(sid.String()) != 16 {
		t.Fatalf("expected 16-char non-zero hex string for SpanID, got %s", sid.String())
	}
}

func TestTracing_W3CTraceParentParsing(t *testing.T) {
	validHeader := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tp, err := ParseTraceParent(validHeader)
	if err != nil {
		t.Fatalf("unexpected error parsing valid traceparent: %v", err)
	}

	if tp.Version != "00" {
		t.Errorf("expected version 00, got %s", tp.Version)
	}
	if tp.TraceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace ID mismatch: %s", tp.TraceID.String())
	}
	if tp.ParentID.String() != "00f067aa0ba902b7" {
		t.Errorf("parent ID mismatch: %s", tp.ParentID.String())
	}
	if tp.Flags != "01" {
		t.Errorf("flags mismatch: %s", tp.Flags)
	}

	// Re-formatted string matches input
	if tp.String() != validHeader {
		t.Errorf("expected %s, got %s", validHeader, tp.String())
	}

	// Invalid header formats
	invalidHeaders := []string{
		"invalid-header",
		"00-short-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-short-01",
		"00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-00f067aa0ba902b7-01",
	}

	for _, invalid := range invalidHeaders {
		if _, err := ParseTraceParent(invalid); err == nil {
			t.Errorf("expected error for invalid header %q, got nil", invalid)
		}
	}
}

func TestTracing_SpanHierarchyAndContext(t *testing.T) {
	tracer := NewTracer("test-service")

	// 1. Root span with incoming traceparent
	incomingTP, _ := ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rootCtx := WithTraceParent(context.Background(), incomingTP)

	ctx1, rootSpan := tracer.StartSpan(rootCtx, "HTTP GET /v1/weather")
	if rootSpan.TraceID() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("root span did not adopt incoming trace ID: %s", rootSpan.TraceID())
	}
	if rootSpan.ParentID() != "00f067aa0ba902b7" {
		t.Errorf("root span did not adopt incoming parent span ID: %s", rootSpan.ParentID())
	}

	// 2. Child span (e.g. Capability)
	ctx2, childSpan1 := tracer.StartSpan(ctx1, "Capability Weather")
	if childSpan1.TraceID() != rootSpan.TraceID() {
		t.Errorf("child span trace ID mismatch: %s != %s", childSpan1.TraceID(), rootSpan.TraceID())
	}
	if childSpan1.ParentID() != rootSpan.SpanID() {
		t.Errorf("child span parent ID did not match parent span ID: %s != %s", childSpan1.ParentID(), rootSpan.SpanID())
	}

	// 3. Grandchild span (e.g. Provider OpenWeather)
	_, childSpan2 := tracer.StartSpan(ctx2, "Provider openweather")
	if childSpan2.TraceID() != rootSpan.TraceID() {
		t.Errorf("grandchild span trace ID mismatch")
	}
	if childSpan2.ParentID() != childSpan1.SpanID() {
		t.Errorf("grandchild parent ID did not match child span ID")
	}

	// Span attributes and status
	childSpan2.SetAttribute("http.status", "200")
	if childSpan2.GetAttribute("http.status") != "200" {
		t.Errorf("attribute not set correctly")
	}

	childSpan2.SetStatus(StatusOK, "Success")
	if childSpan2.Status() != StatusOK {
		t.Errorf("status not set correctly")
	}

	time.Sleep(2 * time.Millisecond)
	childSpan2.End()

	if childSpan2.Duration() < 1*time.Millisecond {
		t.Errorf("expected positive duration for ended span")
	}

	// Context helper extraction
	if TraceIDFromContext(ctx1) != rootSpan.TraceID() {
		t.Errorf("TraceIDFromContext mismatch")
	}
	if SpanIDFromContext(ctx1) != rootSpan.SpanID() {
		t.Errorf("SpanIDFromContext mismatch")
	}
	if SpanFromContext(ctx1) == nil {
		t.Errorf("SpanFromContext returned nil")
	}
}

func TestTracing_RootSpanGenerationWithoutHeader(t *testing.T) {
	tracer := NewTracer("test-service")

	_, span := tracer.StartSpan(context.Background(), "Root")
	defer span.End()

	if span.TraceID() == "" || strings.HasPrefix(span.TraceID(), "00000000") {
		t.Errorf("expected valid non-zero generated TraceID, got %s", span.TraceID())
	}
	if span.ParentID() != "0000000000000000" {
		t.Errorf("expected zero ParentID for root span, got %s", span.ParentID())
	}
}
