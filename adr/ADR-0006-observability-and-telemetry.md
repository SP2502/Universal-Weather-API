# ADR-0006: Observability, Distributed Tracing, and Production Metrics

## Status
Accepted

## Context
As an infrastructure gateway multiplexing client requests to multiple upstream providers, the Universal API Platform requires deep runtime visibility into request lifecycles, upstream latencies, circuit breaker transitions, and normalization errors.

Key engineering requirements:
1. **Prometheus Compatibility**: Standard `/metrics` scrape endpoint formatting metrics in Prometheus text exposition format (0.0.4).
2. **Cardinality Safety**: Metric labels must remain strictly bounded (`capability`, `provider`, `status_class`, `method`). Unbounded identifiers (API keys, IP addresses, client query coordinates, user strings, request IDs) must never be attached as Prometheus labels.
3. **W3C Distributed Tracing**: Trace context propagation using standard `traceparent` headers (`00-<trace_id>-<span_id>-<flags>`), maintaining span hierarchy across Gateway -> Capability Handler -> Reliability Engine -> Upstream Provider Adapter.
4. **Context-Aware Logging**: Automatic injection of `trace_id` and `span_id` into structured `log/slog` entries.

## Decision
1. Implemented a zero-external-dependency, thread-safe metrics registry in `internal/observability/metrics` supporting Counters, Gauges, and exponential-bucket Histograms with Prometheus exposition format rendering.
2. Implemented W3C Trace Context and distributed span management in `internal/observability/tracing` with CSPRNG-generated 16-byte `TraceID` and 8-byte `SpanID`.
3. Integrated `Telemetry` middleware in `internal/http/middleware/telemetry.go` to manage root request spans, track HTTP request duration / throughput metrics, and set `X-Trace-ID` and `traceparent` response headers.
4. Instrumented the Reliability Engine (`internal/reliability/executor.go`), Circuit Breakers (`circuitbreaker.go`), Bulkheads (`bulkhead.go`), and Capability handlers (`weather.go`, `geocoding.go`) to record provider spans, execution attempts, latencies, circuit states, and normalization errors.
5. Enhanced `internal/observability/logging/logger.go` with contextual `trace_id` and `span_id` correlation.

## Consequences
- Gateway throughput, latency distributions, provider failovers, circuit states, and error rates are now fully observable via standard Prometheus scrapers (`GET /metrics`).
- Distributed request tracing connects incoming client requests through to specific upstream provider execution attempts.
- Cardinality safety is strictly preserved across all metric dimensions.
