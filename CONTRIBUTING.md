# Contributing

Thanks for helping improve Universal Weather API.

## Development

Use Go 1.27.4 or a newer supported security-patched release. The Go 1.22 directive in `go.mod` is the language compatibility floor.

```bash
go mod download
go mod verify
go test ./...
go vet ./...
```

Format Go changes with `gofmt` and add tests for behavior changes. Keep provider-specific logic in adapters, preserve canonical schemas and the lean v1 contract, and update [OpenAPI](docs/openapi.yaml) and user docs when routes or responses change. Significant architecture changes should have an [ADR](adr/). The [provider guide](docs/provider-guide.md) covers policy, attribution, and quotas. Never commit credentials, private test logs, or customer data.

Open a pull request with the behavior changed, relevant test results, and any remaining limits. Security issues should follow [SECURITY.md](SECURITY.md).
