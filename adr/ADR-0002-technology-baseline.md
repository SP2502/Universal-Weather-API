# ADR-0002: Go Standard Library Baseline & Zero Framework Bloat

## Context
API gateways require extreme stability, high performance, and minimal dependency vulnerabilities.

## Decision
Use idiomatic Go (Go 1.22+) using the standard library (\
et/http\, \log/slog\, \context\) as the core runtime.

## Consequences
- High performance and minimal CVE footprint.
- Full control over memory allocations and middleware chaining.
