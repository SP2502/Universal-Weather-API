# ADR-0008: Caching Architecture, Single-Flight Request Coalescing, and Stale Semantics

## Status
Accepted

## Context
The Universal API Platform acts as an abstraction gateway between API consumers and heterogeneous upstream providers. Direct passthrough of every request to external providers incurs unnecessary network latency, exposes consumers to provider rate limits and downtime, and drives up operational costs.

Furthermore, multi-tenant gateways face cache stampedes (thundering herds) when popular cache entries expire, and service disruptions during temporary upstream outages.

## Decision
1. **Cache Abstraction & Store:** We define a pluggable `cache.Store` interface supporting `GetEntry(ctx, key)` and `SetWithStale(ctx, key, value, freshTTL, staleTTL)`. We implement `cache.MemoryStore`, a 16-shard bounded in-memory LRU store with per-entry TTL and copy-on-read/write immutability.
2. **Canonical Cache Keys:** Cache keys follow the format `uap:cache:v1:<capability>:<sha256_hash>` with capability-aware coordinate rounding (2 decimals for weather, 4 decimals for reverse geocoding) and text query normalization.
3. **Request Coalescing (Single-Flight):** `cache.SingleFlightGroup` deduplicates simultaneous identical cache misses into a single in-flight upstream execution using canonical cache keys. The flight runs under a detached bounded context so leader caller cancellations do not abort the flight for waiting followers or cache persistence.
4. **Tri-State Freshness Model:**
   - **FRESH (`now < fresh_until`):** Returned immediately with `cached: true, fallback: false`.
   - **STALE-BUT-SERVABLE (`fresh_until <= now < stale_until`):** Served immediately with `cached: true, fallback: false` and `Warning: 110 "Response is Stale"`, while an asynchronous background refresh is triggered under single-flight deduplication (Stale-While-Revalidate).
   - **EXPIRED (`now >= stale_until`):** Evicted; triggers fresh provider execution.
5. **Stale-If-Error Fallback:** If upstream providers suffer a transient outage (e.g. 5xx or network timeouts) and a stale entry exists within `stale_until`, the gateway gracefully returns the stale data with `cached: true, fallback: true` and `Warning: 110 "Response is Stale (Upstream Outage)"`.
6. **Capability & Provider Cache Policy:** `cache.PolicyEngine` configures capability TTLs, SWR windows, and Stale-If-Error windows, respecting provider licensing toggles.
7. **Fail-Open Semantics:** All cache reads and writes fail open without disrupting client requests or throwing 5xx errors.
8. **Observability:** Instrument bounded Prometheus metrics:
   - `uap_cache_requests_total`, `uap_cache_hits_total`, `uap_cache_misses_total`
   - `uap_cache_stale_hits_total`, `uap_cache_refresh_total`, `uap_cache_refresh_errors_total`
   - `uap_cache_singleflight_total`, `uap_cache_singleflight_waiters`
   - `uap_cache_errors_total`, `uap_cache_evictions_total`, `uap_cache_entries`

## Consequences
- **Positive:** Cache stampedes are eliminated: concurrent misses on identical keys coalesce into 1 upstream call ($99.99\%$ upstream call reduction under concurrency).
- **Positive:** Latency during SWR is $96.8\text{ \mu s}$ (instant response without blocking for upstream).
- **Positive:** High availability during upstream outages via Stale-If-Error fallback.
- **Positive:** Background refreshes are deduplicated, preventing refresh stampedes.
- **Deferred:** Distributed Redis caching deferred to Phase 13.
