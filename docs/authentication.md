# Authentication and client keys

Capability endpoints require a UWA client key. This key is separate from every upstream provider credential. Send it in `X-API-Key` or `Authorization: Bearer <key>`; never put it in a URL or commit it to source.

| Scope | Routes |
| --- | --- |
| `weather:read` | `/v1/weather`; weather in `/v1/datasets` |
| `forecast:read` | `/v1/forecast`, `/v1/forecast/daily`; forecast datasets |
| `geocode:read` | `/v1/geocode`; geocode datasets |
| `astronomy:read` | `/v1/astronomy`; astronomy datasets |

A combined datasets request needs the scope for **each** requested product. Missing, expired, or revoked keys are rejected. A valid key without a required scope receives 403. If the authoritative authentication store cannot be reached, the gateway returns 503.

## Local development

No key is created automatically. `APP_DEV_CLIENT_KEY` can seed a unique client key only with `APP_ENV=development`, a loopback listener, and memory persistence. Generate a new random 24–512 character value; see the [quickstart](quickstart.md). Do not use this shortcut on a shared or persistent deployment.

## Staging and production

Use PostgreSQL and `uapctl create-key --name NAME --scopes weather:read,geocode:read` to provision a scoped key. The command displays the plaintext once; store it securely. Review expiry and rotation for your deployment. Use `uapctl doctor --json` for offline diagnostics. See [configuration](configuration.md) and [security policy](../SECURITY.md).
