# ADR-0003: Canonical Response Envelope & Error Format

## Context
Downstream providers use diverse, inconsistent error formats, status codes, and payload envelopes.

## Decision
Enforce a uniform public envelope:
- Success: \{\"data\": {...}, \"meta\": {\"request_id\": \"...\", \"timestamp\": \"...\", \"provider\": \"...\", \"cached\": false, \"fallback\": false}}\
- Error: \{\"error\": {\"code\": \"...\", \"message\": \"...\", \"status\": 500, \"request_id\": \"...\", \"retryable\": false}}\
"@ -Encoding UTF8

Set-Content -Path "C:\Users\kkpcs\.gemini\antigravity\scratch\universal-api-platform\docs\architecture\overview.md" -Value @"
# System Architecture

Detailed documentation of the Universal API Platform architecture.

## Request Lifecycle
1. Client connects via HTTPS.
2. Ingress HTTP Server receives connection.
3. Middleware assigns / validates \X-Request-ID\.
4. Structured logging captures request start.
5. Router directs request to Capability Router.
6. Downstream Provider Adapter executes upstream call.
7. Normalizer produces canonical schema.
8. Meta envelope attached and returned.
