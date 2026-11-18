# ADR-0011 — Deterministic, Rule-Based Geographic Routing Only

**Status:** Accepted — 2026-11-03

## Decision

Universal Weather API is a self-hosted Go weather and geocoding gateway. **It does not use and must not depend on LLMs, chat-completion endpoints, generative AI APIs, embeddings, vector search or model inference to answer weather requests or select providers.** AI generated metadata is not a runtime authority for weather truth, provider licensing, coverage or availability.

The current reviewed source has **no LLM SDK, API-key configuration, model service or LLM request path**. No such feature was deleted in this update because none existed in the baseline source package.

## Deterministic request flow

1. Authenticate caller, check scope and validate the canonical request.
2. If latitude/longitude are supplied, validate finite coordinate ranges and use those coordinates directly. Do **not** geocode or reinterpret explicit coordinates.
3. If a place name is supplied, use a permitted, deterministic forward-geocoding adapter when strict resolution is enabled; validate candidate coordinates, distinguish ambiguous/no-result responses, and never invent a location. The current mode is **opt-in** and is not yet mandatory for all place-name requests.
4. Build a product-specific request context. Check policy, operator approval, deployment licence, credentials, geographic coverage, product availability and upstream quotas using configured rules and known facts. Unknown eligibility must not be guessed from language or prompt interpretation.
5. Among eligible providers, apply the configured deterministic ranking policy (availability, latency, cost or priority). Runtime health changes may change the ranking; that is *dynamic, rule-based behavior*, not AI inference. Use fixed score quantization and stable ID tie-breaking.
6. Execute the highest ranked provider with a bounded deadline. Parse, normalize and validate its response. An HTTP 200 with invalid data is a failure, not success.
7. On an eligible failure, try the next provider within the remaining deadline and quota budget. Recover opened circuits through bounded, timed half-open probes. Respect explicit provider pinning, licensing and geographic eligibility; do not override them merely to produce an answer.
8. Return a versioned canonical response with truthful origin/provenance, observation versus forecast timestamps, attribution and cache/fallback state. If there is no valid permitted provider, return a documented error.

## Non-goals

- No LLM APIs or prompting in the gateway execution path.
- No probabilistic guessing of a country from a place-name string or a coordinate without an approved boundary index.
- No synthetic weather data or invented fallback answers.
- No assumption that every provider supports every region/product.
- No dynamic provider rank based on unsupported accuracy claims.

## Implementation guardrails

`scripts/check_no_llm_dependencies.py` checks production Go source and `go.mod` for recognizable model-client SDKs and endpoints. CI runs it. It is a guardrail, not proof that a Go codebase can never access an AI service.

## Remaining operational caveats

- The currently implemented strict place resolver must be enabled by the operator; the non-strict legacy route may pass text to upstream weather providers without a common geocoding step.
- Country-level routing requires a vetted country polygon dataset or trusted resolved country metadata. A region-limited provider may be ineligible without this evidence.
- The repository still needs genuine PostgreSQL/Redis acceptance, live upstream tests under approved limits, and Windows service lifecycle tests before production certification.
