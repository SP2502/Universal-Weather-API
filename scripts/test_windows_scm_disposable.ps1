#Requires -RunAsAdministrator
[CmdletBinding()]
param(
  [Parameter(Mandatory=$true)][string]$GatewayExe,
  [Parameter(Mandatory=$true)][string]$UpdateExe,
  [Parameter(Mandatory=$true)][string]$EnvironmentFile,
  [Parameter(Mandatory=$true)][string]$ResultFile,
  [switch]$ArchiveMatchingPreviousTestInstall
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$name = 'UniversalWeatherAPI'
$serviceRoot = Join-Path $env:ProgramData $name
$target = Join-Path $serviceRoot 'gateway.exe'
$bad = Join-Path (Resolve-Path -LiteralPath (Split-Path -Parent $ResultFile)).Path 'gateway-scm-invalid.exe'
$install = Join-Path $root 'deployments\windows\install-service.ps1'
$update = Join-Path $root 'deployments\windows\update-service.ps1'
$uninstall = Join-Path $root 'deployments\windows\uninstall-service.ps1'
$result = [ordered]@{status='FAIL'; installed=$false; updated=$false; rollback=$false; uninstalled=$false; archived_path=''; error=''}

function Wait-Liveness {
  $lastFailure = 'no response'
  for ($i = 0; $i -lt 40; $i++) {
    try {
      $response = Invoke-WebRequest -Uri 'http://127.0.0.1:18498/liveness' -UseBasicParsing -TimeoutSec 2
      if ($response.StatusCode -eq 200) { return }
      $lastFailure = "HTTP $($response.StatusCode)"
    } catch { $lastFailure = $_.Exception.Message }
    Start-Sleep -Milliseconds 250
  }
  $service = Get-Service -Name $name -ErrorAction SilentlyContinue
  $state = if ($service) { $service.Status.ToString() } else { 'not installed' }
  throw "Disposable Windows service did not return liveness 200 (service=$state; last probe=$lastFailure)"
}

if (Get-Service -Name $name -ErrorAction SilentlyContinue) { throw 'Service already exists; refusing to touch it' }
if (Get-NetTCPConnection -LocalPort 18498 -State Listen -ErrorAction SilentlyContinue) { throw 'Test port is already in use' }
$original = (Get-FileHash -LiteralPath (Resolve-Path -LiteralPath $GatewayExe).Path -Algorithm SHA256).Hash
$candidateHash = (Get-FileHash -LiteralPath (Resolve-Path -LiteralPath $UpdateExe).Path -Algorithm SHA256).Hash
if ($candidateHash -eq $original) { throw 'Update candidate must differ from the installed test binary' }
if (Test-Path -LiteralPath $serviceRoot) {
  if (-not $ArchiveMatchingPreviousTestInstall) { throw 'ProgramData target already exists; refusing to touch it' }
  $existingEnv = Join-Path $serviceRoot 'gateway.env'
  $existingExe = Join-Path $serviceRoot 'gateway.exe'
  if (-not (Test-Path -LiteralPath $existingEnv) -or -not (Test-Path -LiteralPath $existingExe)) {
    throw 'Existing ProgramData target is not a complete prior test install'
  }
  if ((Get-FileHash -LiteralPath $existingEnv -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $EnvironmentFile -Algorithm SHA256).Hash) {
    throw 'Existing ProgramData configuration differs from this test; refusing to move it'
  }
  if ((Get-FileHash -LiteralPath $existingExe -Algorithm SHA256).Hash -notin @($original, $candidateHash)) {
    throw 'Existing ProgramData executable differs from this test; refusing to move it'
  }
  $archive = Join-Path $env:ProgramData ($name + '.acceptance-previous-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
  $programDataPath = [IO.Path]::GetFullPath($env:ProgramData).TrimEnd('\') + '\'
  foreach ($path in @($serviceRoot, $archive)) {
    if (-not [IO.Path]::GetFullPath($path).StartsWith($programDataPath, [StringComparison]::OrdinalIgnoreCase)) {
      throw 'Unexpected ProgramData path; refusing to move it'
    }
  }
  if (Test-Path -LiteralPath $archive) { throw 'Archive destination already exists' }
  Move-Item -LiteralPath $serviceRoot -Destination $archive
  $result.archived_path = $archive
}

try {
  & $install -GatewayExe $GatewayExe -EnvironmentFile $EnvironmentFile -Start
  $result.installed = $true
  $identity = (Get-CimInstance Win32_Service -Filter "Name='$name'").StartName
  if ($identity -ne 'NT AUTHORITY\LocalService') { throw "Wrong service identity: $identity" }
  Wait-Liveness
  & $update -GatewayExe $UpdateExe
  Wait-Liveness
  if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -ne $candidateHash) { throw 'Updated binary hash mismatch' }
  $result.updated = $true
  [IO.File]::WriteAllBytes($bad, [Text.Encoding]::ASCII.GetBytes('not a Windows executable'))
  $rejected = $false
  try { & $update -GatewayExe $bad } catch { $rejected = $true }
  if (-not $rejected) { throw 'Invalid update unexpectedly succeeded' }
  Wait-Liveness
  if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -ne $candidateHash) { throw 'Rollback did not restore the updated test binary' }
  $result.rollback = $true
  & $uninstall
  $result.uninstalled = $true
  $result.status = 'PASS'
} catch {
  $result.error = $_.Exception.Message
  throw
} finally {
  if (-not $result.uninstalled -and (Get-Service -Name $name -ErrorAction SilentlyContinue)) {
    try { & $uninstall; $result.uninstalled = $true } catch { $result.error += '; cleanup: ' + $_.Exception.Message }
  }
  if (Test-Path -LiteralPath $bad) { Remove-Item -LiteralPath $bad -Force }
  $result | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $ResultFile
}
