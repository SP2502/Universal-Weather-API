# ADR-0009: Intelligent Provider Routing & Decision Engine

## Status
Accepted (Phase 11)

## Context
In previous phases, provider selection was statically determined by the registry using hardcoded fallback priorities and simple health filtering. As the platform supports multiple providers per capability (e.g. OpenWeather, WeatherAPI for weather; Nominatim, LocationIQ for geocoding), the routing layer must dynamically answer:

> "For this specific request, which provider should be attempted first, which providers are acceptable fallbacks, and why?"

The system requires multi-factor optimization (availability, latency, cost, priority, geographic coverage, rate limits) while maintaining strict provider-agnosticism within capability business handlers.

## Decision
1. **Dedicated Provider Decision Engine (`internal/routing/decision`):**
   - Created `DecisionEngine` separating routing decision-making from both the `provider.Registry` ("what exists") and `reliability.Executor` ("how execution and recovery happen").
   - Implemented `ProviderCandidate` and `RequestContext` models.

2. **Hard Constraints & Eligibility Filtering Before Scoring:**
   - Excludes disabled (`StatusDisabled`) or removed (`StatusRemoved`) providers.
   - Excludes degraded (`StatusDegraded`) providers if request policy forbids degraded traffic.
   - Excludes providers whose circuit breakers are currently `OPEN`.
   - Excludes providers whose geographic coverage does not match the request `country` parameter.
   - Enforces preferred provider constraint when specified.

3. **Multi-Factor Scoring & Policy Optimization:**
   - Evaluates a normalized score $[0, 100]$ based on active policy mode:
     - `AVAILABILITY_FIRST` (Default): Health Score ($45\%$), Priority ($30\%$), Rate-Limit State ($15\%$), Latency ($10\%$).
     - `LATENCY_FIRST`: Latency ($45\%$), Health Score ($30\%$), Priority ($15\%$), Rate-Limit ($10\%$).
     - `COST_FIRST`: Cost Class ($45\%$), Health Score ($30\%$), Priority ($15\%$), Latency ($10\%$).
     - `PRIORITY_FIRST`: Configured Priority ($60\%$), Health Score ($30\%$), Latency ($10\%$).

4. **Deterministic Tie-Breaking:**
   - Guaranteed deterministic ordering: `Score (descending) -> Configured Priority (ascending) -> Provider ID (alphanumeric ascending)`.

5. **Operational Health Feedback Loop:**
   - `reliability.Executor` records execution outcomes (success latency, failure/timeout, circuit state changes) into `provider.HealthEvaluator` in real-time, enabling immediate dynamic failover on subsequent requests.

6. **Explainability & Observability:**
   - Emits `DecisionExplanation` with selected provider, ordered execution plan, candidate scores, and exclusion reasons.
   - Instrumented bounded Prometheus metrics (`uap_provider_routing_decisions_total`, `uap_provider_routing_failures_total`).

## Consequences
- Capability handlers (`WeatherHandler`, `GeocodingHandler`) remain completely decoupled from provider identities.
- Sub-10 microsecond routing overhead ($9.08\text{ \mu s/op}$).
- Dynamic, self-healing routing when providers degrade or recover.
