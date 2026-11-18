# Linux & Windows server operation

## Supported delivery models

- Linux native executable (amd64/arm64) as a console process or systemd service.
- Windows native executable (amd64/arm64) as a console process or an actual Windows SCM service (`--service`).
- Linux OCI containers via the existing multi-stage Dockerfile and Docker Compose.
- No Rust, Node, Node.js runtime, third-party Windows service wrapper or Node installer is required on target machines.

**Verification status:** Disposable Windows SCM install/update/rollback/uninstall and Ubuntu systemd lifecycle passed on real local hosts, as summarized in [release status](release-status.md). Those checks do not certify every target host or a public deployment. PostgreSQL is mandatory in staging/production. Upstream-provider credentials are also mandatory for service startup in those profiles.

## Runtime options

```
gateway [--env-file /path/to/gateway.env] [--check-config] [--service]
```

- `--env-file` loads KEY=value settings. Existing process environment variables override file settings. The file parser does not expand shell variables or execute code. Invalid or duplicate entries fail fast. Never log or commit secrets.
- `--check-config` checks syntax and configuration only, not PostgreSQL connectivity, provider API keys, external network, or permission to bind the port.
- `--service` is for the Windows SCM only. Do not set it in Linux systemd ExecStart.

### New environment configuration

| Variable | Default | Function |
|---|---|---|
| `APP_BIND_HOST` | `127.0.0.1` | Explicit private listener by default. Set `0.0.0.0` only when a protected reverse proxy/network boundary is in place. |
| `APP_PORT` | `8080` | HTTP port. |
| `APP_READ_HEADER_TIMEOUT` | `5s` | Bounds time to read headers. |
| `APP_MAX_HEADER_BYTES` | `1048576` | Maximum HTTP request header bytes (4KiB–8MiB). |
| `APP_LOG_FILE` | unset | When set, logs are written to a 10MiB rotating file with five backups; ensure the service identity has write permission. |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | Graceful HTTP server shutdown deadline. |

Keep `APP_LOG_FILE` unset on Linux systemd to use journald. On Windows set an absolute writable location; the supplied installer provisions a separate protected logs directory.

### Linux systemd

1. Build a Linux executable: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gateway-linux-amd64 ./cmd/gateway`.
2. Install as root: `sudo deployments/linux/install.sh ./gateway-linux-amd64`.
3. Edit `/etc/universal-weather-api/gateway.env` with valid PostgreSQL DSN and at least one provider key; protect this file (root-only 0600).
4. Start: `sudo systemctl start universal-weather-api`.
5. Inspect: `systemctl status universal-weather-api`, `journalctl -u universal-weather-api -f`, `curl -f http://127.0.0.1:8080/readiness`.

The unit uses `Type=notify` and a systemd watchdog. READY indicates that the network listener bound successfully, not that dependencies or providers are healthy; `/readiness` independently indicates gateway capability status. Watchdog heartbeat monitors process liveness, not downstream data correctness. Shutdown uses systemd SIGTERM -> Go context cancellation -> `http.Server.Shutdown` -> owned store/cache close. The unit runs as an unprivileged `uwa` account, has an isolated writable state directory, and does not expose port 8080 publicly unless configured.

### Windows Service Control Manager

1. Build an executable on a Go build machine: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o gateway.exe ./cmd/gateway`.
2. In an elevated PowerShell session: `./deployments/windows/install-service.ps1 -GatewayExe ./gateway.exe`.
3. Edit `C:\ProgramData\UniversalWeatherAPI\gateway.env` (the installer restricts ACLs) to provide real PostgreSQL and upstream-provider configuration.
4. Start service: `Start-Service UniversalWeatherAPI`.
5. Check: `Get-Service UniversalWeatherAPI`, `Get-Content C:\ProgramData\UniversalWeatherAPI\logs\gateway.log -Tail 30`, `Invoke-WebRequest http://127.0.0.1:8080/readiness`.
6. To uninstall service only (preserves secrets/logs): `./deployments/windows/uninstall-service.ps1`.

The executable implements native SCM start, stop, shutdown and explicit startup/error state handling. The installer configures delayed automatic start, LocalService privileges and restart-on-failure. Avoid placing credentials in process command-line arguments or registry ImagePath. The installer does not start automatically unless `-Start` is explicitly passed. Installation under LocalService assumes your PostgreSQL access and any file permissions are provisioned accordingly. For remote traffic, put HTTPS ingress (e.g. IIS/reverse proxy) in front and explicitly bind/access-control the gateway.

## Operational and security limitations

- Direct HTTPS termination, certificate renewal and reverse proxy configuration are external deployment concerns; the gateway's listener remains HTTP.
- The local service checks do not establish production capacity, public-domain TLS behavior, or provider account eligibility; see [release status](release-status.md).
- The service package was exercised with bounded live upstream calls, disposable Windows installation and real PostgreSQL migrations.
- The deployment scripts should be reviewed by a local administrator before running as root/Administrator.

## Controlled upgrades

- Linux (as root): `sudo deployments/linux/update.sh ./new-gateway-linux-amd64` stops, backs up, swaps and restarts the service. A failed start triggers binary rollback. Database changes are not rolled back.
- Windows (elevated PowerShell): `./deployments/windows/update-service.ps1 -GatewayExe ./new-gateway.exe` performs a stop, backup, replacement and restart check with binary rollback on failure.

Both upgrade scripts preserve the configuration, logs and data. They do not offer coordinated multi-node rolling upgrades or reversible SQL migrations. Validate database compatibility and create a backup before applying upgrades. The Windows rollback path passed a disposable local SCM test; repeat acceptance on each target deployment.
