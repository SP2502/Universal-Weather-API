#Requires -RunAsAdministrator
[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$ServiceName='UniversalWeatherAPI'
$svc=Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($null -eq $svc) { Write-Host 'Service is not installed.'; exit 0 }
if ($svc.Status -ne 'Stopped') {Stop-Service -Name $ServiceName -ErrorAction Stop}
& sc.exe delete $ServiceName | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Service deletion failed' }
Write-Host 'Windows service removed. Binaries, logs and gateway.env preserved in ProgramData\UniversalWeatherAPI.'
