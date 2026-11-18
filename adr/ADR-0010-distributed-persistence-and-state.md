# ADR-0010: Distributed Persistence, State Management, and Health Checking

## Status
Accepted

## Context
The Universal API Platform was previously operating with in-memory stores for API key authentication, rate limiting, and caching. While effective for single-process local development, this created a horizontal scaling bottleneck where multi-node gateway clusters would suffer from API key desynchronization, rate-limit multiplication across pods, and cache fragmentation.

Additionally, technical debt (DEBT-001) existed where readiness health probes (`/readiness`) returned static responses without actively verifying database or cache connectivity.

## Decision

### 1. Database & Persistence Layer (`internal/database`)
- Implement `PostgresStore` (`internal/database/postgres.go`) utilizing standard library SQL connection pooling (`database/sql`).
- Implement automated schema migrations applying `migrations/000001_init_schema.up.sql` on startup.
- Support constant-time key hash queries (`GetAPIKeyByHash`), active status verification, expiration deadlines, and zero-downtime key rotation with grace period coexistence.
- Implement transactional batch usage record insertion (`RecordUsageBatch`) to maximize database throughput under load.
- Retain `MemoryStore` as a lightweight zero-dependency default for local development (`APP_DB_DRIVER=memory`).

### 2. Distributed Caching & Rate Limiting (`internal/cache`, `internal/http/middleware`)
- Implement `RedisStore` (`internal/cache/redis.go`) implementing `cache.Store` using pure Go RESP connection pooling.
- Serialize freshness boundaries (`FreshUntil`, `StaleUntil`) to preserve Tri-State SWR and Stale-If-Error semantics across distributed cluster nodes.
- Implement **Fail-Open Resilience**: if Redis connection fails or times out, seamlessly fall back to embedded in-memory cache without throwing 5xx errors to client traffic.
- Implement distributed rate limiting in middleware with automated fail-open fallback.

### 3. Production Secret Management (`internal/secrets`)
- Implement `FileSecretStore` (`internal/secrets/file.go`) reading credentials from mounted volumes (Kubernetes secrets / Docker secret files) with directory traversal protection.
- Implement `ChainedSecretStore` (`internal/secrets/chained.go`) resolving credentials in priority order: `File` $\to$ `Env` $\to$ `Memory`.

### 4. Active Health Checking (`internal/health`)
- Implement `DatabaseChecker`, `CacheChecker`, and `ProviderHealthChecker` in `internal/health/checkers.go`.
- Wire checkers into `/readiness` so orchestration systems (Kubernetes/ECS) automatically halt traffic dispatch if database or critical dependencies become unreachable.

### 5. High-Throughput Asynchronous Usage Batching (`internal/usage`)
- Implement `BatchTracker` with a bounded ring buffer queue, background timer/batch flushes, and graceful shutdown draining to decouple HTTP request latency from database insert latencies.

## Consequences

### Positive
- The gateway is horizontally scalable and cloud-native without state fragmentation.
- Fail-open architecture ensures external network blips in Redis/DB do not cascade into gateway downtime.
- Zero external dependency bloat (adheres strictly to ADR-0002).
- Complete operational visibility via active `/readiness` probes.
