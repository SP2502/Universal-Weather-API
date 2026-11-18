param(
    [string]$OutputDirectory = 'dist'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location -LiteralPath $root
$versionText = & go version
if ($LASTEXITCODE -ne 0 -or $versionText -notmatch 'go(\d+\.\d+\.\d+)') {
    throw 'Unable to determine the Go toolchain version'
}
if ([version]$Matches[1] -lt [version]'1.27.4') {
    throw "Go 1.27.4 or newer is required for this release build; found $versionText"
}
$destination = Join-Path $root $OutputDirectory
New-Item -ItemType Directory -Path $destination -Force | Out-Null
$env:CGO_ENABLED = '0'
$targets = @(
    @{ os = 'linux'; arch = 'amd64' },
    @{ os = 'linux'; arch = 'arm64' },
    @{ os = 'windows'; arch = 'amd64' },
    @{ os = 'windows'; arch = 'arm64' }
)
foreach ($target in $targets) {
    $env:GOOS = $target.os
    $env:GOARCH = $target.arch
    $suffix = if ($target.os -eq 'windows') { '.exe' } else { '' }
    foreach ($app in @('gateway', 'uapctl')) {
        $name = "$app-$($target.os)-$($target.arch)$suffix"
        Write-Output "Building $name"
        & go build -p 2 -trimpath -ldflags '-s -w' -o (Join-Path $destination $name) "./cmd/$app"
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $name" }
    }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
$licenses = Join-Path $destination 'licenses'
New-Item -ItemType Directory -Path $licenses -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'LICENSE') -Destination (Join-Path $destination 'LICENSE') -Force
$goRoot = (& go env GOROOT).Trim()
$pqRoot = (& go list -m -f '{{.Dir}}' github.com/lib/pq).Trim()
if ($LASTEXITCODE -ne 0 -or -not $goRoot -or -not $pqRoot) { throw 'Unable to locate dependency license files' }
Copy-Item -LiteralPath (Join-Path $goRoot 'LICENSE') -Destination (Join-Path $licenses 'go-LICENSE') -Force
Copy-Item -LiteralPath (Join-Path $pqRoot 'LICENSE.md') -Destination (Join-Path $licenses 'lib-pq-LICENSE.md') -Force
$sbom = Join-Path $destination 'source-sbom.spdx.json'
& python scripts/generate_sbom.py | Set-Content -LiteralPath $sbom -Encoding utf8
if ($LASTEXITCODE -ne 0) { throw 'SBOM generation failed' }
$artifactNames = @($targets | ForEach-Object {
    $target = $_
    $suffix = if ($target.os -eq 'windows') { '.exe' } else { '' }
    @('gateway', 'uapctl') | ForEach-Object { "$_-$($target.os)-$($target.arch)$suffix" }
}) + @('source-sbom.spdx.json', 'LICENSE', 'licenses/go-LICENSE', 'licenses/lib-pq-LICENSE.md')
$checksums = foreach ($name in $artifactNames) {
    $digest = (Get-FileHash -LiteralPath (Join-Path $destination $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    "$digest  $name"
}
$checksums | Set-Content -LiteralPath (Join-Path $destination 'SHA256SUMS') -Encoding ascii
Write-Output "Release artifacts and checksums: $destination"
