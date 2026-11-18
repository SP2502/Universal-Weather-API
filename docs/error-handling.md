# Error Handling & Resilience Guide

The Universal API Platform provides a consistent, structured canonical error model across all endpoints.

---

## 1. Canonical Error Response Format

All errors return JSON with an `error` envelope containing machine-readable codes and actionable diagnostic metadata:

```json
{
  "error": {
    "code": "INVALID_QUERY_PARAMETER",
    "message": "Missing required query parameter: location",
    "request_id": "req-1724670000000000000",
    "retryable": false
  }
}
```

### Error Fields

- `code` (string): Machine-readable category code for programmatic error handling.
- `message` (string): Human-friendly explanation sanitized to prevent leakage of internal credentials or stack traces.
- `request_id` (string): Correlation ID matching the `X-Request-ID` response header for support and logging inquiries.
- `retryable` (boolean): When `true`, indicates that the request failed due to a transient condition (e.g. rate limit, temporary upstream outage) and retrying after backoff may succeed.

---

## 2. Standard Error Code Catalog

| HTTP Status | Error Code | Description | Retryable? |
| :--- | :--- | :--- | :--- |
| `400 Bad Request` | `INVALID_QUERY_PARAMETER` | Required query parameter missing or malformed. | `false` |
| `400 Bad Request` | `GEOCODING_INVALID_REQUEST` | Query text too long (>500 chars) or coordinates out of bounds. | `false` |
| `401 Unauthorized` | `UNAUTHORIZED` | API key missing, empty, or invalid. | `false` |
| `401 Unauthorized` | `KEY_REVOKED` | The provided API key has been revoked. | `false` |
| `401 Unauthorized` | `KEY_EXPIRED` | The provided API key has passed its expiration timestamp. | `false` |
| `403 Forbidden` | `INSUFFICIENT_SCOPE` | API key lacks the required permission scope. | `false` |
| `413 Payload Too Large`| `PAYLOAD_TOO_LARGE` | Incoming request body exceeds 64 KB ceiling. | `false` |
| `429 Too Many Requests`| `RATE_LIMIT_EXCEEDED` | Client exceeded token-bucket request rate limits. | `true` |
| `502 Bad Gateway` | `UPSTREAM_ERROR` | All upstream providers in the fallback chain failed to respond. | `true` |
| `502 Bad Gateway` | `GEOCODING_PROVIDER_ERROR`| Upstream geocoding providers failed to respond. | `true` |
| `503 Service Unavailable`| `PROVIDER_NOT_AVAILABLE`| No active providers registered for the requested capability. | `true` |
| `500 Internal Error` | `NORMALIZATION_ERROR` | Upstream provider responded with unparseable or malformed data. | `false` |

---

## 3. Client Retry Best Practices

When handling errors with `retryable: true`:
1. **Respect `Retry-After` Header**: On `HTTP 429 Too Many Requests`, check the `Retry-After` header for the minimum seconds to wait.
2. **Exponential Backoff with Full Jitter**:
   $$\text{sleep} = \text{random}(0, \min(\text{max\_backoff}, \text{base} \times 2^{\text{attempt}}))$$
3. **Limit Maximum Retries**: Do not retry more than 3–4 times to prevent cascading retry storms.
