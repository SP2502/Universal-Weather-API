#Requires -RunAsAdministrator
[CmdletBinding()]
param(
  [Parameter(Mandatory=$true)][string]$GatewayExe,
  [string]$EnvironmentFile,
  [switch]$Start
)
$ErrorActionPreference = 'Stop'
$ServiceName = 'UniversalWeatherAPI'
$Root = Join-Path $env:ProgramData 'UniversalWeatherAPI'
$LogDir = Join-Path $Root 'logs'
$TargetExe = Join-Path $Root 'gateway.exe'
$TargetEnv = Join-Path $Root 'gateway.env'
$ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$SourceExe = (Resolve-Path -LiteralPath $GatewayExe).Path
if ($SourceExe.Contains('"')) { throw 'Executable path cannot contain quotation marks.' }
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) { throw 'Service already exists. Stop/uninstall it before replacing binaries.' }
New-Item -ItemType Directory -Path $Root, $LogDir -Force | Out-Null
Copy-Item -LiteralPath $SourceExe -Destination $TargetExe -Force
if (-not (Test-Path -LiteralPath $TargetEnv)) {
  if ($EnvironmentFile) {
    Copy-Item -LiteralPath (Resolve-Path -LiteralPath $EnvironmentFile).Path -Destination $TargetEnv
  } else {
    Copy-Item -LiteralPath (Join-Path $ScriptRoot 'gateway.env.example') -Destination $TargetEnv
  }
}
# SIDs avoid localized built-in group name differences. Administrators:
# S-1-5-32-544, SYSTEM: S-1-5-18, LocalService: S-1-5-19.
& icacls.exe $Root /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-19:(OI)(CI)RX' | Out-Null
if ($LASTEXITCODE -ne 0) {throw 'Root directory ACL failure'}
& icacls.exe $LogDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-19:(OI)(CI)M' | Out-Null
if ($LASTEXITCODE -ne 0) {throw 'Log ACL failure'}
& icacls.exe $TargetEnv /inheritance:r /grant:r '*S-1-5-18:F' '*S-1-5-32-544:F' '*S-1-5-19:R' | Out-Null
if ($LASTEXITCODE -ne 0) {throw 'Configuration ACL failure'}
$ImagePath = '"' + $TargetExe + '" --env-file "' + $TargetEnv + '" --service'
# New-Service supports native Windows SCM without third-party wrapper programs.
New-Service -Name $ServiceName -BinaryPathName $ImagePath -DisplayName 'Universal Weather API' -Description 'Self-hosted multi-provider weather API gateway' -StartupType Automatic | Out-Null
try {
  $accountResult = & sc.exe config $ServiceName 'obj=' 'NT AUTHORITY\LocalService' 'start=' 'delayed-auto' 2>&1
  if ($LASTEXITCODE -ne 0) { throw "Unable to set least-privileged LocalService account: $accountResult" }
  $failureResult = & sc.exe failure $ServiceName 'reset=' '86400' 'actions=' 'restart/30000/restart/60000/none/0' 2>&1
  if ($LASTEXITCODE -ne 0) {throw "SCM recovery policy setup failed: $failureResult"}
  $flagResult = & sc.exe failureflag $ServiceName '1' 2>&1
  if ($LASTEXITCODE -ne 0) {throw "SCM non-crash failure recovery setting failed: $flagResult"}
} catch {
  # Never leave a partially configured service running as LocalSystem.
  & sc.exe delete $ServiceName | Out-Null
  throw
}
Write-Host "Installed $ServiceName as LocalService."
Write-Host "Configuration: $TargetEnv"
if ($Start) {
  & $TargetExe --env-file $TargetEnv --check-config
  if ($LASTEXITCODE -ne 0) {throw 'Configuration check failed; service left installed but stopped'}
  Start-Service -Name $ServiceName
  Write-Host "Service started; inspect with Get-Service $ServiceName and Invoke-WebRequest http://127.0.0.1:8080/readiness"
} else {
  Write-Host 'Not started. Edit the restricted config file, then: Start-Service UniversalWeatherAPI'
}
