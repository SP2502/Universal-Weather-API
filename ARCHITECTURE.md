# Architecture Overview

## Modular Monolith
Universal API Platform is designed as a modular monolith in Go.

`	ext
cmd/gateway/
  └── main.go                  # Application entry point & lifecycle

internal/
  ├── api/                     # Canonical envelope & RFC-7807 errors
  ├── config/                  # Validated configuration engine
  ├── health/                  # Liveness, readiness, and health probes
  ├── http/
  │    ├── middleware/         # RequestID, Logger, Recovery
  │    └── server/             # Production HTTP server with graceful shutdown
  └── observability/
       └── logging/            # Zero-leak structured logger
`

## Data Plane vs Control Plane
- **Data Plane**: Fast path request handling, authentication, routing, capability execution, fallback, normalization, response delivery.
- **Control Plane**: Provider registry, configuration, API key administration, audit logging.
