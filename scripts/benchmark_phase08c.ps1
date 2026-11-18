# Windows Phase 08C synthetic performance suite. Does not install services.
param([string]$OutputDirectory = "artifacts/perf-phase08c")
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $root
try {
    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
    $goVersion = & go version
    if ($LASTEXITCODE -ne 0) { throw 'Go is unavailable' }
    "$goVersion`n$([Environment]::OSVersion.VersionString)" | Set-Content (Join-Path $OutputDirectory 'environment.txt')
    & go test -run '^$' -bench 'Benchmark(Baseline_GatewayWeather_Sequential|Cache_GatewayWeather_Hit_Sequential|Baseline_GatewayWeather_Parallel|Cache_GatewayWeather_Hit_Parallel|DecisionEngine_ProviderSelection)$' -benchtime=500ms -count=3 -benchmem ./tests/integration | Tee-Object -FilePath (Join-Path $OutputDirectory 'gateway.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Gateway benchmarks failed' }
    & go test -run '^$' -bench '^BenchmarkPhase08C' -benchtime=500ms -count=3 -benchmem ./internal/cache ./internal/capability ./internal/observability/metrics | Tee-Object -FilePath (Join-Path $OutputDirectory 'hotspots.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Hotspot benchmarks failed' }
    $env:UWA_PERF_RUN = '1'
    try {
        & go test -run '^TestPhase08CLatencyDistribution$' -count=1 -v ./tests/integration | Tee-Object -FilePath (Join-Path $OutputDirectory 'latency.txt')
        if ($LASTEXITCODE -ne 0) { throw 'Synthetic distribution sample failed' }
    } finally { Remove-Item Env:UWA_PERF_RUN -ErrorAction SilentlyContinue }
    Write-Host 'Synthetic results captured. Real infrastructure acceptance is a separate Phase 09 gate.'
} finally { Pop-Location }
