#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p dist
for target in linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os="${target%/*}" arch="${target#*/}" suffix=""
  [[ "$os" == windows ]] && suffix='.exe'
  echo "Building ${os}/${arch}..."
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "dist/gateway-${os}-${arch}${suffix}" ./cmd/gateway
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "dist/uapctl-${os}-${arch}${suffix}" ./cmd/uapctl
done
python3 scripts/generate_sbom.py > dist/source-sbom.spdx.json
(cd dist && sha256sum gateway-* uapctl-* source-sbom.spdx.json > SHA256SUMS)
echo 'Artifacts and checksums: dist/'
