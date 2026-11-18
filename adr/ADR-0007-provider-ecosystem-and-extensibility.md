# ADR-0007: Provider Ecosystem, Manifests, and Extensibility Architecture

## Status
Accepted

## Context
A core value proposition of the Universal API Platform is multi-provider abstraction and extensibility across capabilities (Weather, Geocoding, and future domains). The platform requires a structured, secure, and predictable model for adding and maintaining upstream provider adapters.

We evaluated five architectural models for provider extensibility:
1. **Model A (Compile-Time Go Registration)**: Standard Go packages registering implementations at gateway bootstrap.
2. **Model B (Dynamic Go Plugins via `.so`)**: Runtime binary loading using Go's `plugin` package.
3. **Model C (Out-of-Process Worker Adapters via gRPC/IPC)**: Running providers in separate processes/containers.
4. **Model D (Declarative HTTP Mapping Engine)**: Generic YAML/JSON template engines without Go code.
5. **Model E (Hybrid Compile-Time Registry + Declarative Manifests)**: Compile-time Go adapters driven by explicit declarative `Manifest` structs and verified via automated conformance test suites.

### Evaluation of Options
- **Model B (Go Plugins)** was **rejected** due to memory unsafety, lack of cross-compilation support on Windows/macOS, and tight compiler toolchain version lock-in.
- **Model C (Out-of-Process)** provides strong isolation but introduces premature operational complexity, IPC serialization overhead, and sidecar management. Deferred to future scale phases if untrusted multi-tenant execution is needed.
- **Model D (Declarative Only)** lacks the expressiveness required for complex provider authentication (OAuth, HMAC signatures, dynamic tokens) and nuanced canonical schema normalization.

## Decision
We adopted **Model E (Hybrid Compile-Time Registry + Declarative Manifests)**:
1. **Explicit Provider Contract**: Every provider implements `provider.Provider` and capability-specific interfaces (`WeatherProvider`, `GeocodingProvider`).
2. **Declarative Manifests**: Each provider defines a structured `provider.Manifest` specifying identity, capabilities, timeouts, priority, lifecycle status (`ACTIVE`, `DEGRADED`, `DISABLED`, `DEPRECATED`), and mandatory legal attribution.
3. **Reusable Conformance Test Harness**: Built `internal/provider/conformance` allowing any provider adapter to be automatically verified against manifest validity, timeout compliance, LimitReader body bounding (1 MB), and error classification.
4. **Health Evaluation & Dynamic Prioritization**: `HealthEvaluator` tracks real-time circuit breaker states and success/failure ratios, calculating dynamic health scores ($0.0 \dots 1.0$) for adaptive fallback ordering.
5. **Credential Isolation**: Providers declare credential requirements via manifest keys (`CredentialsRequired`), which are resolved via `SecretStore` during gateway bootstrap and injected into adapters without exposing them to metadata or logs.

## Consequences
- Contributors can implement and register new providers by following a uniform, well-documented recipe without modifying core gateway routing or capability logic.
- Static single-binary compilation is preserved across all operating systems (Windows, Linux, macOS).
- Conformance tests prevent broken adapters, unbounded memory allocations, and improper error classifications from reaching production.
