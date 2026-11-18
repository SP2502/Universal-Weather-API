#!/usr/bin/env bash
# UWA Phase 08C: isolated, deterministic performance instrumentation.
# Requires a real Go toolchain and a resolvable genuine github.com/lib/pq module.
# Never supplies provider keys or calls production upstreams.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/artifacts/perf-phase08c}"
mkdir -p "$OUT"
cd "$ROOT"
printf 'Host: %s\nGo: %s\n' "$(uname -a)" "$(go version)" > "$OUT/environment.txt"
printf '\n[1/3] Synthetic gateway and path benchmarks\n'
go test -run '^$' -bench 'Benchmark(Baseline_GatewayWeather_Sequential|Cache_GatewayWeather_Hit_Sequential|Baseline_GatewayWeather_Parallel|Cache_GatewayWeather_Hit_Parallel|DecisionEngine_ProviderSelection)$' -benchtime=500ms -count=3 -benchmem ./tests/integration | tee "$OUT/gateway.txt"
printf '\n[2/3] Small hotspot benchmarks\n'
go test -run '^$' -bench '^BenchmarkPhase08C' -benchtime=500ms -count=3 -benchmem ./internal/cache ./internal/capability ./internal/observability/metrics | tee "$OUT/hotspots.txt"
printf '\n[3/3] Opt-in synthetic percentile distribution\n'
UWA_PERF_RUN=1 go test -run '^TestPhase08CLatencyDistribution$' -count=1 -v ./tests/integration | tee "$OUT/latency.txt"
printf '\nSaved results under %s\n' "$OUT"
printf 'NOTE: httptest + in-memory auth; not real PostgreSQL/Redis/network acceptance.\n'
