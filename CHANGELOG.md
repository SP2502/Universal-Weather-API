# Changelog

This project is a public source preview. Production deployment acceptance is still pending; historical development notes remain available in Git history.

## v0.1.0-preview.1 — 19 November 2026

- Self-hosted Go gateway with a lean v1 API for current weather, forecasts, geocoding, astronomy, and combined datasets.
- Scoped client keys, canonical versioned responses, provider provenance, bounded fallback, cache validation, and explicit provider policy and quotas.
- Native Ubuntu systemd and Windows SCM delivery, Docker packaging, and release checks.
- Local acceptance exercised disposable PostgreSQL and Redis, nine upstream services across the implemented data categories, local HTTPS, native service lifecycle, and fixture-backed load. See [release status](docs/release-status.md) for scope and remaining gates.

The [OpenAPI specification](docs/openapi.yaml) and [README](README.md) describe the candidate public contract.
