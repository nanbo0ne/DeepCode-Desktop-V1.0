$ErrorActionPreference = 'Stop'
$profileRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../../.tmp/audit-2026-09-07/evidence-test-profile'))
$names = @('APPDATA', 'LOCALAPPDATA', 'HOME', 'USERPROFILE')
$previous = @{}
foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
$code = 1
Push-Location -LiteralPath $PSScriptRoot
try {
    $env:APPDATA = Join-Path $profileRoot 'roaming'
    $env:LOCALAPPDATA = Join-Path $profileRoot 'local'
    $env:HOME = Join-Path $profileRoot 'home'
    $env:USERPROFILE = $env:HOME
    foreach ($path in @($env:APPDATA, $env:LOCALAPPDATA, $env:HOME)) { [void][IO.Directory]::CreateDirectory($path) }
    go test . -count=1 -timeout=90s -v
    $code = $LASTEXITCODE
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
    Pop-Location
}
exit $code
