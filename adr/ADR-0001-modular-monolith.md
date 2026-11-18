# ADR-0001: Adopt Modular Monolith Architecture

## Context
We need to design the Universal API Platform to support multiple downstream providers and capabilities while maintaining high reliability, ease of deployment, and clean code organization.

## Decision
We choose a **Modular Monolith** architecture in Go rather than microservices.

## Consequences
### Positive
- Single binary deployment, simplified operations and local development.
- In-memory function call latency between layers.
- Shared type safety and unified testing.

### Negative
- Requires strict package boundaries to avoid tight coupling.
