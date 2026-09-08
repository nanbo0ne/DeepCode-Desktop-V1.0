#requires -Version 7.0
[CmdletBinding()]
param([string]$ExpectedVersion = '3.0.3')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Never use this acceptance test on a workstation or a self-hosted runner.
if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:RUNNER_OS -cne 'Windows' -or $env:RUNNER_ARCH -cne 'X64' -or
    $env:GITHUB_RUN_ID -notmatch '^\d+$' -or -not $IsWindows -or -not [Environment]::Is64BitProcess) {
    throw 'Installer acceptance requires a GitHub-hosted Windows X64 runner.'
}

function Get-PlainPath([string]$Path) {
    if ($Path -notmatch '^[A-Za-z]:[\\/]' -or $Path.Substring(2) -match '[:"*?<>|\x00-\x1f]') {
        throw "Not an absolute local filesystem path: $Path"
    }
    $full = [IO.Path]::GetFullPath($Path)
    if ($full -eq [IO.Path]::GetPathRoot($full)) { throw "Drive roots are forbidden: $Path" }
    $full = $full.TrimEnd('\')
    foreach ($part in $full.Substring(3).Split('\')) {
        if ($part -match '[. ]$') { throw "Ambiguous path component: $Path" }
    }
    $cursor = $full
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            if ((Get-Item -Force -LiteralPath $cursor).Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "Reparse points are forbidden: $cursor"
            }
        }
        $cursor = [IO.Path]::GetDirectoryName($cursor)
    }
    return $full
}

function Assert-ChildPath([string]$Path, [string]$Parent) {
    $full = Get-PlainPath $Path
    $root = Get-PlainPath $Parent
    if (-not $full.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Path is outside the required parent $root : $full"
    }
    return $full
}

$runnerTemp = Get-PlainPath $env:RUNNER_TEMP
if ($runnerTemp.Length -le 3 -or -not (Test-Path -LiteralPath $runnerTemp -PathType Container)) {
    throw 'RUNNER_TEMP must be an existing non-root directory.'
}
$workspace = Get-PlainPath $env:GITHUB_WORKSPACE
$repo = Get-PlainPath (Join-Path $PSScriptRoot '..')
if ($repo -ine $workspace -or $runnerTemp -ieq $workspace) {
    throw 'The script must belong to this job checkout, separate from RUNNER_TEMP.'
}
if ($ExpectedVersion -notmatch '^v?(\d+\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?$') {
    throw 'ExpectedVersion must be a desktop semantic version.'
}
$productVersion = $Matches[1]
if ([version]$productVersion -le [version]'3.0.2') { throw 'The candidate must be newer than 3.0.2.' }
$ownedRoot = Assert-ChildPath (Join-Path $runnerTemp ('orca installer acceptance ' + [guid]::NewGuid().ToString('N'))) $runnerTemp
if (Test-Path -LiteralPath $ownedRoot) { throw 'The acceptance directory must be new.' }

function Owned-Path([string]$Relative) {
    return Assert-ChildPath (Join-Path $ownedRoot $Relative) $ownedRoot
}

function New-OwnedDirectory([string]$Path) {
    $safe = Assert-ChildPath $Path $ownedRoot
    [void][IO.Directory]::CreateDirectory($safe)
}

function Get-SHA256([string]$Path) {
    $safe = Get-PlainPath $Path
    return (Get-FileHash -LiteralPath $safe -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-Version([string]$Actual, [string]$Expected, [string]$Label) {
    if ($Actual -notmatch ('^' + [regex]::Escape($Expected) + '(?:\.0)?$')) {
        throw "$Label version is '$Actual', expected $Expected"
    }
}

$uninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\O.R.C.A for Windows'
$legacyKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\DeepSeek-Orca'
foreach ($hive in @('CurrentUser', 'LocalMachine')) {
    foreach ($view in @('Registry32', 'Registry64')) {
        $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey($hive, $view)
        try {
            foreach ($name in @($uninstallKey, $legacyKey)) {
                $key = $base.OpenSubKey($name)
                if ($null -ne $key) {
                    $key.Dispose()
                    throw "Runner is not clean: existing $hive/$view/$name"
                }
            }
        } finally { $base.Dispose() }
    }
}
if (Get-Process -Name Orca, deepseek-orca-desktop -ErrorAction SilentlyContinue) {
    throw 'Runner is not clean: an ORCA application is already running.'
}

# Do not let the embedded bootstrapper install another product during this test.
$webviewID = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$webviewMachine = Get-ItemProperty -LiteralPath "HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\$webviewID" -ErrorAction SilentlyContinue
$webviewUser = Get-ItemProperty -LiteralPath "HKCU:\Software\Microsoft\EdgeUpdate\Clients\$webviewID" -ErrorAction SilentlyContinue
if (-not (($webviewMachine -and $webviewMachine.PSObject.Properties['pv'] -and $webviewMachine.pv) -or
          ($webviewUser -and $webviewUser.PSObject.Properties['pv'] -and $webviewUser.pv))) {
    throw 'Preinstalled WebView2 is required; this test will not run its bootstrapper.'
}

$assetName = 'O.R.C.A-for-Windows-windows-amd64-installer.exe'
$candidate = Assert-ChildPath (Join-Path $repo "dist\$assetName") $repo
$app = Assert-ChildPath (Join-Path $repo 'desktop\build\bin\Orca.exe') $repo
$payload = Assert-ChildPath (Join-Path $repo 'desktop\build\windows\installer-go\payload') $repo
Assert-Version ([Diagnostics.FileVersionInfo]::GetVersionInfo($app).ProductVersion) $productVersion 'Build app ProductVersion'
$expectedHashes = @{'Orca.exe' = Get-SHA256 $app}
foreach ($relative in @('node.exe', 'LICENSE.node.txt', 'codegraph\node.exe', 'codegraph\bin\codegraph.cmd')) {
    $source = Assert-ChildPath (Join-Path $payload $relative) $payload
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "Missing payload: $relative" }
}
foreach ($relative in @('node.exe', 'LICENSE.node.txt')) {
    $expectedHashes[$relative] = Get-SHA256 (Join-Path $payload $relative)
}
$codegraph = Join-Path $payload 'codegraph'
foreach ($entry in Get-ChildItem -Force -Recurse -LiteralPath $codegraph) {
    $source = Assert-ChildPath $entry.FullName $codegraph
    if (-not $entry.PSIsContainer) {
        $expectedHashes['codegraph\' + [IO.Path]::GetRelativePath($codegraph, $source)] = Get-SHA256 $source
    }
}
$candidateHash = Get-SHA256 $candidate

# NSIS uses SHGetFolderPath, not just the APPDATA environment variables. Snapshot
# both current-user shell mappings before changing them, then verify in fresh
# 32/64-bit processes. Never move the runner's existing folders or their data.
$folderTargets = [ordered]@{
    'AppData' = Owned-Path 'profile\AppData\Roaming'
    'Local AppData' = Owned-Path 'profile\AppData\Local'
    'Desktop' = Owned-Path 'profile\Desktop'
    'Programs' = Owned-Path 'profile\Start Menu\Programs'
}
$shellSnapshots = [Collections.Generic.List[object]]::new()
foreach ($leaf in @('User Shell Folders', 'Shell Folders')) {
    $keyName = "Software\Microsoft\Windows\CurrentVersion\Explorer\$leaf"
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($keyName)
    if ($null -eq $key) { throw "Missing shell mapping: $keyName" }
    try {
        foreach ($name in $folderTargets.Keys) {
            if ($key.GetValueNames() -notcontains $name) { throw "Cannot snapshot shell folder $name" }
            $shellSnapshots.Add([pscustomobject]@{
                Key = $keyName; Name = $name; Kind = $key.GetValueKind($name)
                Value = $key.GetValue($name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            })
        }
    } finally { $key.Dispose() }
}

# All writes below belong to this unique directory or the saved shell mappings.
[void][IO.Directory]::CreateDirectory($ownedRoot)
$evidenceDir = Owned-Path 'evidence'
foreach ($dir in @($evidenceDir, (Owned-Path 'downloads'), (Owned-Path 'process-temp')) + @($folderTargets.Values)) {
    New-OwnedDirectory $dir
}
$evidence = [ordered]@{
    status = 'running'; run = $env:GITHUB_RUN_ID; commit = $env:GITHUB_SHA
    expectedVersion = $ExpectedVersion; candidateSHA256 = $candidateHash
    processes = [Collections.Generic.List[object]]::new()
    checks = [Collections.Generic.List[object]]::new()
    shellFoldersRestored = $false
}
$allowedExecutables = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
$childEnvironment = @{
    APPDATA = $folderTargets['AppData']; LOCALAPPDATA = $folderTargets['Local AppData']
    TEMP = Owned-Path 'process-temp'; TMP = Owned-Path 'process-temp'
}

function Invoke-BoundedProcess([string]$Executable, [string]$Arguments, [ValidatePattern('^[a-z0-9-]+$')][string]$Label) {
    $exe = Get-PlainPath $Executable
    if (-not $allowedExecutables.Contains($exe)) { throw "Executable is not allowlisted: $exe" }
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $exe
    # NSIS /D and _?= must be LAST and UNQUOTED even when the path has spaces.
    # ArgumentList would add quotes, so deliberately use the raw argument string.
    $info.Arguments = $Arguments
    $info.WorkingDirectory = $ownedRoot
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($name in $childEnvironment.Keys) { $info.Environment[$name] = $childEnvironment[$name] }
    [void]$info.Environment.Remove('GH_TOKEN')
    [void]$info.Environment.Remove('GITHUB_TOKEN')
    $record = [ordered]@{label = $Label; executable = $exe; arguments = $Arguments; exitCode = $null; timedOut = $false}
    $evidence.processes.Add($record)
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $process = [Diagnostics.Process]::Start($info)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(120000)) {
            $record.timedOut = $true
            $process.Kill($true) # Only this test's process and its descendants.
            [void]$process.WaitForExit(5000)
            throw "$Label exceeded the 120-second process timeout."
        }
        $record.exitCode = $process.ExitCode
        $remaining = [int][Math]::Max(0, 120000 - $timer.ElapsedMilliseconds)
        $streams = [Threading.Tasks.Task]::WhenAll([Threading.Tasks.Task[]]@($stdout, $stderr))
        if (-not $streams.Wait($remaining)) {
            $record.timedOut = $true
            throw "$Label did not close its output streams within 120 seconds."
        }
        $output = $stdout.GetAwaiter().GetResult()
        $errors = $stderr.GetAwaiter().GetResult()
        [IO.File]::WriteAllText((Owned-Path "evidence\$Label.stdout.txt"), $output)
        [IO.File]::WriteAllText((Owned-Path "evidence\$Label.stderr.txt"), $errors)
        if ($process.ExitCode -ne 0) { throw "$Label failed with exit code $($process.ExitCode)." }
        return $output
    } finally { $process.Dispose() }
}

function Save-OfficialAsset($Asset, [string]$Destination) {
    $destination = Assert-ChildPath $Destination $ownedRoot
    if ([string]$Asset.id -notmatch '^\d+$') { throw 'Invalid release asset ID.' }
    $api = "https://api.github.com/repos/nanbo0ne/O.R.C.A-for-Windows/releases/assets/$($Asset.id)"
    try {
        Invoke-WebRequest -Uri $api -Headers $downloadHeaders -OutFile $destination -TimeoutSec 120
    } catch {
        # Public fallback; never forward the API bearer token to another host.
        $direct = "https://github.com/nanbo0ne/O.R.C.A-for-Windows/releases/download/desktop-v3.0.2/$($Asset.name)"
        Invoke-WebRequest -Uri $direct -OutFile $destination -TimeoutSec 120
    }
}

function Assert-Installation([string]$Directory, [string]$Version, [string]$Label) {
    $target = Assert-ChildPath $Directory $ownedRoot
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($uninstallKey)
    if ($null -eq $key) { throw "$Label did not register an uninstall key." }
    try {
        $location = Get-PlainPath ([string]$key.GetValue('InstallLocation'))
        $displayVersion = [string]$key.GetValue('DisplayVersion')
        if ($location -ine $target) { throw "$Label installed in the wrong directory: $location" }
        Assert-Version $displayVersion $Version "$Label registry DisplayVersion"
        if ([string]$key.GetValue('UninstallString') -cne ('"' + $target + '\uninstall.exe"')) {
            throw "$Label has an incorrect UninstallString."
        }
    } finally { $key.Dispose() }
    $installedApp = Assert-ChildPath (Join-Path $target 'Orca.exe') $target
    $peVersion = [Diagnostics.FileVersionInfo]::GetVersionInfo($installedApp).ProductVersion
    Assert-Version $peVersion $Version "$Label app ProductVersion"
    if (-not (Test-Path -LiteralPath (Join-Path $target 'uninstall.exe') -PathType Leaf)) {
        throw "$Label is missing uninstall.exe."
    }
    $evidence.checks.Add(@{phase = $Label; InstallLocation = $location; DisplayVersion = $displayVersion; ProductVersion = $peVersion})
}

function Assert-CurrentPayload([string]$Directory, [string]$Label) {
    foreach ($relative in $expectedHashes.Keys) {
        $installed = Assert-ChildPath (Join-Path $Directory $relative) $ownedRoot
        if ((Get-SHA256 $installed) -cne $expectedHashes[$relative]) { throw "$Label payload mismatch: $relative" }
    }
    $actualFiles = @(foreach ($entry in Get-ChildItem -Force -Recurse -LiteralPath (Join-Path $Directory 'codegraph')) {
        $null = Assert-ChildPath $entry.FullName $ownedRoot
        if (-not $entry.PSIsContainer) { $entry }
    })
    $expectedCount = @($expectedHashes.Keys | Where-Object { $_.StartsWith('codegraph\') }).Count
    if ($actualFiles.Count -ne $expectedCount) { throw "$Label contains unexpected CodeGraph files." }
    $evidence.checks.Add(@{phase = $Label; matchedPayloadFiles = $expectedHashes.Count; hashes = $expectedHashes})
}

$markerHashes = @{}
function Add-Marker([string]$Path, [string]$Content) {
    $safe = Assert-ChildPath $Path $ownedRoot
    New-OwnedDirectory ([IO.Path]::GetDirectoryName($safe))
    [IO.File]::WriteAllText($safe, $Content, [Text.UTF8Encoding]::new($false))
    $markerHashes[$safe] = Get-SHA256 $safe
}

function Assert-Markers([string]$Label) {
    foreach ($path in $markerHashes.Keys) {
        if ((Get-SHA256 $path) -cne $markerHashes[$path]) { throw "$Label changed synthetic user data: $path" }
    }
    $evidence.checks.Add(@{phase = $Label; preservedMarkers = $markerHashes.Count})
}

function Assert-NoApplication {
    if (Get-Process -Name Orca, deepseek-orca-desktop -ErrorAction SilentlyContinue) {
        throw 'Silent installation unexpectedly launched the application.'
    }
}

function Invoke-DefaultUninstall([string]$Directory, [string]$Label) {
    $target = Assert-ChildPath $Directory $ownedRoot
    $uninstaller = Owned-Path "$Label.exe"
    Copy-Item -LiteralPath (Join-Path $target 'uninstall.exe') -Destination $uninstaller
    [void]$allowedExecutables.Add($uninstaller)
    # Run the copy outside INSTDIR. _?= prevents NSIS detaching another uninstaller,
    # so the same 120s timeout covers the actual uninstall, not just its launcher.
    $null = Invoke-BoundedProcess $uninstaller "/S _?=$target" $Label
    foreach ($relative in @('Orca.exe', 'node.exe', 'LICENSE.node.txt', 'codegraph', 'uninstall.exe')) {
        if (Test-Path -LiteralPath (Join-Path $target $relative)) { throw "$Label left installed payload: $relative" }
    }
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($uninstallKey)
    if ($null -ne $key) {
        $key.Dispose()
        throw "$Label left the uninstall registry key."
    }
    Assert-NoApplication
    Assert-Markers $Label
}

$mappingsChanged = $false
try {
    $headers = @{'Accept' = 'application/vnd.github+json'; 'User-Agent' = 'ORCA-installer-acceptance'; 'X-GitHub-Api-Version' = '2022-11-28'}
    if ($env:GH_TOKEN) { $headers.Authorization = "Bearer $env:GH_TOKEN" }
    $downloadHeaders = $headers.Clone()
    $downloadHeaders.Accept = 'application/octet-stream'
    $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/nanbo0ne/O.R.C.A-for-Windows/releases/tags/desktop-v3.0.2' -Headers $headers -TimeoutSec 120
    if ($release.tag_name -cne 'desktop-v3.0.2' -or $release.draft -or $release.prerelease) { throw 'Invalid official baseline release.' }
    foreach ($name in @($assetName, 'SHA256SUMS.txt')) {
        $assets = @($release.assets | Where-Object { $_.name -ceq $name })
        if ($assets.Count -ne 1) { throw "Missing or ambiguous release asset: $name" }
        Save-OfficialAsset $assets[0] (Owned-Path "downloads\$name")
    }
    $oldInstaller = Owned-Path "downloads\$assetName"
    $checksumText = [IO.File]::ReadAllText((Owned-Path 'downloads\SHA256SUMS.txt'))
    $checksumRows = [regex]::Matches($checksumText, ('(?im)^([a-f0-9]{64}) [ *]' + [regex]::Escape($assetName) + '\r?$'))
    if ($checksumRows.Count -ne 1) { throw 'The baseline must have exactly one SHA256SUMS entry.' }
    $oldHash = Get-SHA256 $oldInstaller
    # Pin the public 3.0.2 baseline in addition to checking its downloaded sums.
    $pinnedOldHash = '5bf27fd4d958fc389a2ef320e401d05875d379b4d96c4ac64a8e8288ea48894d'
    if ($oldHash -ine $checksumRows[0].Groups[1].Value -or $oldHash -cne $pinnedOldHash) {
        throw 'Official 3.0.2 installer SHA256 mismatch.'
    }
    $evidence.checks.Add(@{phase = 'official-baseline-sha256'; tag = $release.tag_name; sha256 = $oldHash})
    $newInstaller = Owned-Path 'candidate-installer.exe'
    Copy-Item -LiteralPath $candidate -Destination $newInstaller
    if ((Get-SHA256 $newInstaller) -cne $candidateHash) { throw 'Candidate changed while staging.' }
    [void]$allowedExecutables.Add($oldInstaller)
    [void]$allowedExecutables.Add($newInstaller)

    $mappingsChanged = $true
    foreach ($saved in $shellSnapshots) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($saved.Key, $true)
        try { $key.SetValue($saved.Name, $folderTargets[$saved.Name], $saved.Kind) } finally { $key.Dispose() }
    }
    $probe = Owned-Path 'shell-folders.ps1'
    [IO.File]::WriteAllText($probe, @'
$ErrorActionPreference = 'Stop'
[ordered]@{
    'AppData' = [Environment]::GetFolderPath('ApplicationData')
    'Local AppData' = [Environment]::GetFolderPath('LocalApplicationData')
    'Desktop' = [Environment]::GetFolderPath('DesktopDirectory')
    'Programs' = [Environment]::GetFolderPath('Programs')
} | ConvertTo-Json -Compress
'@)
    foreach ($arch in @('System32', 'SysWOW64')) {
        $shell = Get-PlainPath (Join-Path $env:SystemRoot "$arch\WindowsPowerShell\v1.0\powershell.exe")
        [void]$allowedExecutables.Add($shell)
        $json = Invoke-BoundedProcess $shell "-NoLogo -NoProfile -NonInteractive -File `"$probe`"" "shell-$($arch.ToLowerInvariant())"
        $resolved = ConvertFrom-Json -AsHashtable $json
        foreach ($name in $folderTargets.Keys) {
            if ((Get-PlainPath $resolved[$name]) -ine $folderTargets[$name]) { throw "NSIS shell folder isolation failed: $arch/$name" }
        }
    }

    foreach ($name in @('Orca.exe', 'deepseek-orca', 'orca', 'O.R.C.A')) {
        $dataRoot = Join-Path $folderTargets['AppData'] $name
        Add-Marker (Join-Path $dataRoot 'config.json') '{"synthetic":true,"selectedModel":"acceptance-only"}'
        Add-Marker (Join-Path $dataRoot 'sessions\synthetic-session.json') '{"id":"acceptance-only","messages":["preserve me"]}'
    }
    foreach ($name in @('deepseek-orca', 'O.R.C.A')) {
        Add-Marker (Join-Path $folderTargets['Local AppData'] "$name\models\synthetic-tiny.gguf") 'synthetic model marker; not an executable or a usable model'
    }
    $upgradeDir = Owned-Path 'upgrade target with spaces'
    $freshDir = Owned-Path 'fresh target with spaces'
    $null = Invoke-BoundedProcess $oldInstaller "/S /D=$upgradeDir" 'install-302'
    Assert-NoApplication
    Assert-Installation $upgradeDir '3.0.2' 'installed-302'
    Assert-Markers 'installed-302'
    Add-Marker (Join-Path $upgradeDir 'data\synthetic-session.json') '{"synthetic":true}'
    Add-Marker (Join-Path $upgradeDir '.deepseek-orca\config.json') '{"synthetic":true}'

    # Omit /D for the upgrade to test the persisted InstallLocation fallback.
    $null = Invoke-BoundedProcess $newInstaller '/S' 'upgrade-current'
    Assert-NoApplication
    Assert-Installation $upgradeDir $productVersion 'upgraded-current'
    Assert-CurrentPayload $upgradeDir 'upgraded-current'
    Assert-Markers 'upgraded-current'

    # An explicit /D must win even while another installation is registered.
    $null = Invoke-BoundedProcess $newInstaller "/S /D=$freshDir" 'install-fresh'
    Assert-NoApplication
    Assert-Installation $freshDir $productVersion 'fresh-directory'
    Assert-CurrentPayload $freshDir 'fresh-directory'
    Assert-CurrentPayload $upgradeDir 'original-directory-unchanged'
    Add-Marker (Join-Path $freshDir 'data\synthetic-session.json') '{"synthetic":true}'
    Invoke-DefaultUninstall $freshDir 'uninstall-fresh'
    Invoke-DefaultUninstall $upgradeDir 'uninstall-upgraded'
    $evidence.status = 'passed'
} catch {
    $evidence.status = 'failed'
    $evidence['error'] = $_.Exception.Message
    throw
} finally {
    $restoreErrors = [Collections.Generic.List[string]]::new()
    if ($mappingsChanged) {
        foreach ($saved in $shellSnapshots) {
            try {
                $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($saved.Key, $true)
                try { $key.SetValue($saved.Name, $saved.Value, $saved.Kind) } finally { $key.Dispose() }
            } catch { $restoreErrors.Add("$($saved.Key)/$($saved.Name)") }
        }
    }
    $evidence.shellFoldersRestored = $restoreErrors.Count -eq 0
    if ($restoreErrors.Count) { $evidence.status = 'failed'; $evidence['restoreErrors'] = @($restoreErrors.ToArray()) }
    $evidencePath = Owned-Path 'evidence\result.json'
    [IO.File]::WriteAllText($evidencePath, ($evidence | ConvertTo-Json -Depth 10))
    Write-Host "Installer acceptance: $($evidence.status); evidence: $evidencePath"
    # Leave only the owned tree for the hosted runner's automatic cleanup.
    if ($restoreErrors.Count) { throw 'Failed to restore runner shell folder mappings.' }
}
