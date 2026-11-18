# Local quickstart

These steps start a loopback development gateway with memory stores. A real weather response requires an approved upstream account and provider policy. See the [README](../README.md) for product scope and release status.

## Prerequisites

Install Go 1.27.4 or a newer supported security-patched release. From the repository root, run `go mod download` and `go test ./...`.

## Windows PowerShell

In the terminal where you start the gateway:

```powershell
$env:APP_DEV_CLIENT_KEY = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
$env:OPENWEATHER_API_KEY = '<your OpenWeather key>'
go run ./cmd/gateway --check-config
go run ./cmd/gateway
```

In another terminal, check the process and make a request. Copy the generated development key from the first terminal's environment into `$env:UAP_API_KEY` locally; do not paste it into a public log.

```powershell
Invoke-RestMethod http://127.0.0.1:8080/liveness
$env:UAP_API_KEY = '<same generated development client key>'
Invoke-RestMethod -Headers @{ 'X-API-Key' = $env:UAP_API_KEY } 'http://127.0.0.1:8080/v1/weather?lat=51.5074&lon=-0.1278'
```

## Ubuntu / Bash

```bash
export APP_DEV_CLIENT_KEY="$(openssl rand -hex 32)"
export OPENWEATHER_API_KEY='<your OpenWeather key>'
go run ./cmd/gateway --check-config
go run ./cmd/gateway
```

In another terminal, check `curl http://127.0.0.1:8080/liveness`. Copy the development key into `UAP_API_KEY` in that terminal and request:

```bash
curl -H "X-API-Key: $UAP_API_KEY" 'http://127.0.0.1:8080/v1/weather?lat=51.5074&lon=-0.1278'
```

The weather call can return an unavailable-provider error when no eligible upstream is enabled. OpenWeather and WeatherAPI supply current weather; LocationIQ supplies geocoding. Forecast needs its own eligible forecast provider. Never exceed your provider plan's allowance. See [provider setup](provider-guide.md), [configuration](configuration.md), [authentication](authentication.md), and [API reference](api-reference.md).
