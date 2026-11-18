# ADR-0004: Capability-Agnostic Reliability and Fallback Engine

## Context
Upstream third-party provider APIs suffer from transient network errors, latency spikes, rate limits, and outages. Unbounded retries or fallback loops can exhaust gateway resources and explode upstream API costs.

## Decision
We implement a reusable, capability-agnostic reliability layer in `internal/reliability` wrapping provider execution:
1. **Request Deadline & Bounded Provider Timeout**: Every client request enforces a parent deadline; provider calls receive a bounded timeout context.
2. **Circuit Breakers**: Concurrency-safe state machines (`CLOSED`, `OPEN`, `HALF_OPEN`) per provider to isolate degraded upstreams.
3. **Bulkheads**: Semaphore-based concurrency limiters per provider to prevent one failing provider from starving system goroutines.
4. **Jittered Exponential Backoff**: Retries use exponential backoff with randomized jitter to prevent thundering herds.
5. **Amplification Protection**: Total attempts across primary, retries, and fallbacks are strictly capped by `MaxTotalAttemptsPerRequest`.