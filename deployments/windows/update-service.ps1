#Requires -RunAsAdministrator
[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$GatewayExe)
$ErrorActionPreference='Stop'
$name='UniversalWeatherAPI'
$target=Join-Path $env:ProgramData 'UniversalWeatherAPI\gateway.exe'
$backup=Join-Path $env:ProgramData 'UniversalWeatherAPI\gateway.previous.exe'
$new=(Resolve-Path -LiteralPath $GatewayExe).Path
$service=Get-Service -Name $name -ErrorAction Stop
if (-not (Test-Path -LiteralPath $target)) {throw 'Existing service executable missing'}
$candidate=Join-Path $env:ProgramData 'UniversalWeatherAPI\gateway.candidate.exe'
$backupReady=$false
$targetTouched=$false
$wasRunning=$service.Status -eq 'Running'
try {
  # Stage the candidate and refresh the rollback image before service downtime.
  Copy-Item -LiteralPath $new -Destination $candidate -Force
  Copy-Item -LiteralPath $target -Destination $backup -Force
  $backupReady=$true
  if ($wasRunning) {Stop-Service -Name $name -ErrorAction Stop}
  $targetTouched=$true
  Copy-Item -LiteralPath $candidate -Destination $target -Force
  if ($wasRunning) {
    Start-Service -Name $name -ErrorAction Stop
    Start-Sleep -Seconds 3
    $service.Refresh()
    if ($service.Status -ne 'Running') {throw 'Replacement service did not remain running'}
  }
  Write-Host "Updated $name. Previous binary preserved as $backup"
} catch {
  Write-Warning 'Update failed. Attempting previous binary rollback.'
  if ($targetTouched) {Stop-Service -Name $name -ErrorAction SilentlyContinue}
  if ($backupReady -and $targetTouched) {
    Copy-Item -LiteralPath $backup -Destination $target -Force
  }
  if ($wasRunning) {
    $service.Refresh()
    if ($service.Status -ne 'Running') {Start-Service -Name $name -ErrorAction Stop}
  }
  throw
} finally {
  Remove-Item -LiteralPath $candidate -Force -ErrorAction SilentlyContinue
}
