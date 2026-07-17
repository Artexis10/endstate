[CmdletBinding()]
param(
    [Parameter()]
    [string]$EnginePath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Write-Phase {
    param([Parameter(Mandatory)][string]$Message)
    Write-Host "==> [wt-generation-smoke] $Message"
}

function Assert-True {
    param(
        [Parameter(Mandatory)][bool]$Condition,
        [Parameter(Mandatory)][string]$Message
    )
    if (-not $Condition) {
        throw $Message
    }
}

function Get-Sha256 {
    param([Parameter(Mandatory)][string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Test-ByteArrayEqual {
    param(
        [Parameter(Mandatory)][byte[]]$Left,
        [Parameter(Mandatory)][byte[]]$Right
    )
    if ($Left.Length -ne $Right.Length) {
        return $false
    }
    for ($index = 0; $index -lt $Left.Length; $index++) {
        if ($Left[$index] -ne $Right[$index]) {
            return $false
        }
    }
    return $true
}

function Test-ContainsBytes {
    param(
        [Parameter(Mandatory)][byte[]]$Haystack,
        [Parameter(Mandatory)][byte[]]$Needle
    )
    if ($Needle.Length -eq 0 -or $Haystack.Length -lt $Needle.Length) {
        return $false
    }
    for ($start = 0; $start -le ($Haystack.Length - $Needle.Length); $start++) {
        $matched = $true
        for ($offset = 0; $offset -lt $Needle.Length; $offset++) {
            if ($Haystack[$start + $offset] -ne $Needle[$offset]) {
                $matched = $false
                break
            }
        }
        if ($matched) {
            return $true
        }
    }
    return $false
}

function Assert-WindowsTerminalClosed {
    $running = @(Get-Process -Name 'WindowsTerminal' -ErrorAction SilentlyContinue)
    if ($running.Count -gt 0) {
        $ids = (($running | ForEach-Object { $_.Id }) -join ', ')
        throw "Windows Terminal must be closed for this smoke test. Running process id(s): $ids. The script will not stop them."
    }
}

function ConvertTo-NativeArgument {
    param([Parameter(Mandatory)][AllowEmptyString()][string]$Value)
    if ($Value.Contains('"')) {
        throw 'Native command arguments containing a double quote are not supported by this smoke script.'
    }
    if ($Value.Length -eq 0) {
        return '""'
    }
    if ($Value -match '\s') {
        return '"' + $Value + '"'
    }
    return $Value
}

function Invoke-Endstate {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string]$Command,
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$CommandArguments
    )

    $allArguments = @($Command) + $CommandArguments + @('--json', '--events', 'jsonl')
    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $Executable
    $startInfo.Arguments = (($allArguments | ForEach-Object { ConvertTo-NativeArgument $_ }) -join ' ')
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    if (-not $process.Start()) {
        throw "Failed to start endstate $Command."
    }
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    $process.WaitForExit()
    $stdout = $stdoutTask.GetAwaiter().GetResult()
    $stderr = $stderrTask.GetAwaiter().GetResult()
    $exitCode = $process.ExitCode
    $process.Dispose()

    $stdoutLines = @($stdout -split "`r?`n" | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($stdoutLines.Count -ne 1) {
        throw "endstate $Command wrote $($stdoutLines.Count) non-empty stdout lines; expected exactly one JSON envelope."
    }
    try {
        $envelope = $stdoutLines[0] | ConvertFrom-Json
    }
    catch {
        throw "endstate $Command stdout was not valid JSON: $($_.Exception.Message)"
    }
    if ($exitCode -ne 0 -or $envelope.success -ne $true -or $envelope.command -ne $Command -or $null -ne $envelope.error) {
        throw "endstate $Command failed (exit=$exitCode): $($stdoutLines[0])"
    }
    Assert-True (-not [string]::IsNullOrWhiteSpace([string]$envelope.runId)) "endstate $Command envelope omitted runId."
    Assert-True (-not [string]::IsNullOrWhiteSpace([string]$envelope.timestampUtc)) "endstate $Command envelope omitted timestampUtc."

    $eventLines = @($stderr -split "`r?`n" | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    Assert-True ($eventLines.Count -gt 0) "endstate $Command emitted no JSONL events."
    $events = @()
    foreach ($line in $eventLines) {
        try {
            $eventRecord = $line | ConvertFrom-Json
        }
        catch {
            throw "endstate $Command stderr was not pure JSONL: $line"
        }
        Assert-True ($eventRecord.version -eq 1) "endstate $Command emitted an event with an unexpected schema version."
        Assert-True (-not [string]::IsNullOrWhiteSpace([string]$eventRecord.runId)) "endstate $Command emitted an event without runId."
        Assert-True (-not [string]::IsNullOrWhiteSpace([string]$eventRecord.timestamp)) "endstate $Command emitted an event without timestamp."
        Assert-True (-not [string]::IsNullOrWhiteSpace([string]$eventRecord.event)) "endstate $Command emitted an event without event type."
        $events += $eventRecord
    }
    Assert-True ($events[0].event -eq 'phase') "endstate $Command did not emit phase first."
    Assert-True ($events[$events.Count - 1].event -eq 'summary') "endstate $Command did not emit summary last."

    return [pscustomobject]@{
        Envelope = $envelope
        Events = $events
    }
}

function Remove-VerifiedSmokeDirectory {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$ExpectedPrefix
    )
    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }
    $fullPath = [System.IO.Path]::GetFullPath($Path).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $parent = [System.IO.Directory]::GetParent($fullPath)
    Assert-True ($null -ne $parent) "Refusing to clean an unparented path: $fullPath"
    Assert-True ([string]::Equals($parent.FullName.TrimEnd([System.IO.Path]::DirectorySeparatorChar), $tempRoot, [System.StringComparison]::OrdinalIgnoreCase)) "Refusing to clean a directory outside the temporary root: $fullPath"
    Assert-True ([System.IO.Path]::GetFileName($fullPath).StartsWith($ExpectedPrefix, [System.StringComparison]::Ordinal)) "Refusing to clean an unexpected temporary directory: $fullPath"
    Remove-Item -LiteralPath $fullPath -Recurse -Force
}

function Write-BackupRecoveryStatus {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$ExpectedHash
    )
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        [Console]::Error.WriteLine("BACKUP UNAVAILABLE: $Path")
        return
    }
    try {
        $actualHash = Get-Sha256 $Path
        if (-not [string]::IsNullOrWhiteSpace($ExpectedHash) -and $actualHash -eq $ExpectedHash) {
            [Console]::Error.WriteLine("BACKUP RETAINED (SHA-256 verified): $Path")
        }
        else {
            [Console]::Error.WriteLine("BACKUP PRESENT BUT HASH UNVERIFIED: $Path")
        }
    }
    catch {
        [Console]::Error.WriteLine("BACKUP PRESENT BUT UNREADABLE: $Path")
    }
}

function Remove-VerifiedBackup {
    param(
        [Parameter(Mandatory)][string]$Root,
        [Parameter(Mandatory)][string]$Path
    )
    $fullRoot = [System.IO.Path]::GetFullPath($Root).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    $tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $parent = [System.IO.Directory]::GetParent($fullRoot)
    Assert-True ($null -ne $parent) "Refusing to clean an unparented backup root: $fullRoot"
    Assert-True ([string]::Equals($parent.FullName.TrimEnd([System.IO.Path]::DirectorySeparatorChar), $tempRoot, [System.StringComparison]::OrdinalIgnoreCase)) "Refusing to clean a backup outside the temporary root: $fullRoot"
    Assert-True ([System.IO.Path]::GetFileName($fullRoot).StartsWith('endstate-wt-backup-', [System.StringComparison]::Ordinal)) "Refusing to clean an unexpected backup root: $fullRoot"
    Assert-True ([string]::Equals([System.IO.Directory]::GetParent($fullPath).FullName, $fullRoot, [System.StringComparison]::OrdinalIgnoreCase)) "Refusing to clean a backup file outside its verified root: $fullPath"
    $children = @(Get-ChildItem -LiteralPath $fullRoot -Force)
    Assert-True ($children.Count -eq 1 -and [string]::Equals($children[0].FullName, $fullPath, [System.StringComparison]::OrdinalIgnoreCase)) 'Refusing to clean a backup root with unexpected contents.'
    Remove-Item -LiteralPath $fullPath -Force
    Assert-True (-not (Test-Path -LiteralPath $fullPath)) "Verified backup still exists after deletion: $fullPath"
    try {
        Remove-Item -LiteralPath $fullRoot -Force
    }
    catch {
        Write-Warning "Verified backup file was deleted, but its empty directory could not be removed: $fullRoot"
    }
}

if ($env:OS -ne 'Windows_NT') {
    throw 'This smoke test runs only on Windows.'
}
if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
    throw 'LOCALAPPDATA is required.'
}

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$moduleSource = Join-Path $repoRoot 'modules\apps\windows-terminal\module.jsonc'
$settingsPath = [System.IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json'))
if (-not (Test-Path -LiteralPath $moduleSource -PathType Leaf)) {
    throw "Windows Terminal module is missing: $moduleSource"
}
if (-not (Test-Path -LiteralPath $settingsPath -PathType Leaf)) {
    throw "Windows Terminal settings are not installed at the expected Microsoft package path: $settingsPath"
}
Assert-WindowsTerminalClosed

$runToken = [guid]::NewGuid().ToString('N')
$scratchRoot = Join-Path ([System.IO.Path]::GetTempPath()) "endstate-wt-smoke-$runToken"
$stateRoot = Join-Path $scratchRoot 'state'
$backupRoot = Join-Path ([System.IO.Path]::GetTempPath()) "endstate-wt-backup-$runToken"
$backupPath = Join-Path $backupRoot 'settings.json.original'
$bundlePath = Join-Path $scratchRoot 'windows-terminal-capture.zip'
$extractRoot = Join-Path $scratchRoot 'extracted'
$sentinelExpectedPath = Join-Path $scratchRoot 'settings.json.with-sentinel'
$sentinelText = "`r`n// endstate-config-generation-smoke:$runToken`r`n"
$sentinelBytes = [System.Text.UTF8Encoding]::new($false).GetBytes($sentinelText)
$previousEndstateRoot = [Environment]::GetEnvironmentVariable('ENDSTATE_ROOT', 'Process')
$workflowPassed = $false
$settingsMutationStarted = $false
$primaryError = $null
$cleanupError = $null
$originalHash = ''
$originalBytes = [byte[]]@()

Assert-True (-not ([System.IO.Path]::GetFullPath($backupRoot).StartsWith(
    [System.IO.Path]::GetFullPath($stateRoot).TrimEnd([System.IO.Path]::DirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar,
    [System.StringComparison]::OrdinalIgnoreCase
))) 'Independent backup must be outside ENDSTATE_ROOT.'

try {
    Write-Phase 'Creating independent, hash-verified settings backup'
    [System.IO.Directory]::CreateDirectory($scratchRoot) | Out-Null
    [System.IO.Directory]::CreateDirectory($backupRoot) | Out-Null
    $originalBytes = [System.IO.File]::ReadAllBytes($settingsPath)
    $originalHash = Get-Sha256 $settingsPath
    [System.IO.File]::WriteAllBytes($backupPath, $originalBytes)
    Assert-True ((Get-Sha256 $backupPath) -eq $originalHash) 'Independent backup hash does not match the live settings hash.'
    Write-Host "    backup: $backupPath"

    Write-Phase 'Creating one-module temporary catalog'
    $catalogModuleDir = Join-Path $stateRoot 'modules\apps\windows-terminal'
    [System.IO.Directory]::CreateDirectory($catalogModuleDir) | Out-Null
    $catalogModulePath = Join-Path $catalogModuleDir 'module.jsonc'
    Copy-Item -LiteralPath $moduleSource -Destination $catalogModulePath
    $catalogModules = @(Get-ChildItem -LiteralPath (Join-Path $stateRoot 'modules\apps') -Recurse -File -Filter 'module.jsonc')
    Assert-True ($catalogModules.Count -eq 1) "Temporary catalog contains $($catalogModules.Count) modules; expected exactly one."
    Assert-True ([string]::Equals($catalogModules[0].FullName, $catalogModulePath, [System.StringComparison]::OrdinalIgnoreCase)) 'Temporary catalog contains an unexpected module.'
    [Environment]::SetEnvironmentVariable('ENDSTATE_ROOT', $stateRoot, 'Process')

    if ([string]::IsNullOrWhiteSpace($EnginePath)) {
        Write-Phase 'Building temporary engine binary'
        $resolvedEngine = Join-Path $scratchRoot 'bin\endstate.exe'
        [System.IO.Directory]::CreateDirectory((Split-Path -Parent $resolvedEngine)) | Out-Null
        Push-Location (Join-Path $repoRoot 'go-engine')
        try {
            # This is an ephemeral proof binary; VCS metadata is irrelevant and
            # can resolve to an unrelated parent repository in nested worktrees.
            $buildOutput = & go build -buildvcs=false -o $resolvedEngine ./cmd/endstate 2>&1
            if ($LASTEXITCODE -ne 0) {
                throw "go build failed: $($buildOutput -join [Environment]::NewLine)"
            }
        }
        finally {
            Pop-Location
        }
    }
    else {
        $resolvedEngine = (Resolve-Path -LiteralPath $EnginePath).Path
    }
    Assert-True (Test-Path -LiteralPath $resolvedEngine -PathType Leaf) "Engine binary was not found: $resolvedEngine"

    Write-Phase 'Capturing actual Windows Terminal settings into a v2 bundle'
    Assert-WindowsTerminalClosed
    $capture = Invoke-Endstate -Executable $resolvedEngine -Command 'capture' -CommandArguments @('--driver', 'winget', '--name', 'windows-terminal-generation-smoke', '--out', $bundlePath)
    Assert-True (Test-Path -LiteralPath $bundlePath -PathType Leaf) 'Capture did not create the requested bundle.'
    Assert-True ($capture.Envelope.data.bundleSchemaVersion -eq '2.0') 'Capture did not produce bundle schema v2.'
    Assert-True ([int]$capture.Envelope.data.manifestVersion -eq 2) 'Capture did not produce manifest version 2.'
    $captureSets = @($capture.Envelope.data.configCapture.configSets)
    Assert-True ($captureSets.Count -eq 1) "Capture reported $($captureSets.Count) generation config sets; expected one."
    Assert-True ($captureSets[0].moduleId -eq 'apps.windows-terminal' -and $captureSets[0].status -eq 'captured' -and [int]$captureSets[0].filesCaptured -eq 1) 'Windows Terminal generation settings were not captured exactly once.'
    $bundleHash = Get-Sha256 $bundlePath

    Write-Phase 'Extracting bundle without modifying it'
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [System.IO.Directory]::CreateDirectory($extractRoot) | Out-Null
    [System.IO.Compression.ZipFile]::ExtractToDirectory($bundlePath, $extractRoot)
    Assert-True ((Get-Sha256 $bundlePath) -eq $bundleHash) 'Bundle hash changed during extraction.'
    $capturedManifest = Join-Path $extractRoot 'manifest.jsonc'
    Assert-True (Test-Path -LiteralPath $capturedManifest -PathType Leaf) 'Extracted bundle has no manifest.jsonc.'

    Write-Phase 'Appending behavior-neutral JSONC sentinel'
    Assert-WindowsTerminalClosed
	$currentBytes = [System.IO.File]::ReadAllBytes($settingsPath)
	Assert-True (Test-ByteArrayEqual $currentBytes $originalBytes) 'Windows Terminal settings changed after backup; refusing to overwrite newer bytes.'
	Assert-True ((Get-Sha256 $settingsPath) -eq $originalHash) 'Windows Terminal settings hash changed after backup; refusing to append the sentinel.'
    $mutatedBytes = [byte[]]::new($originalBytes.Length + $sentinelBytes.Length)
    [System.Array]::Copy($originalBytes, 0, $mutatedBytes, 0, $originalBytes.Length)
    [System.Array]::Copy($sentinelBytes, 0, $mutatedBytes, $originalBytes.Length, $sentinelBytes.Length)
	$settingsMutationStarted = $true
    [System.IO.File]::WriteAllBytes($settingsPath, $mutatedBytes)
    $sentinelStateBytes = [System.IO.File]::ReadAllBytes($settingsPath)
    [System.IO.File]::WriteAllBytes($sentinelExpectedPath, $sentinelStateBytes)
    Assert-True (Test-ByteArrayEqual $sentinelStateBytes $mutatedBytes) 'Sentinel settings bytes were not preserved exactly.'
    Assert-True (Test-ContainsBytes $sentinelStateBytes $sentinelBytes) 'Sentinel was not present after mutation.'

    Write-Phase 'Restoring captured original through the engine'
    Assert-WindowsTerminalClosed
    $restore = Invoke-Endstate -Executable $resolvedEngine -Command 'restore' -CommandArguments @('--manifest', $capturedManifest, '--enable-restore')
    $restoredBytes = [System.IO.File]::ReadAllBytes($settingsPath)
    Assert-True (Test-ByteArrayEqual $restoredBytes $originalBytes) 'Engine restore did not reproduce the exact original settings bytes.'
    Assert-True ((Get-Sha256 $settingsPath) -eq $originalHash) 'Engine-restored settings hash differs from the original.'
    Assert-True (-not (Test-ContainsBytes $restoredBytes $sentinelBytes)) 'Sentinel remained after engine restore.'
    $restoreItems = @($restore.Envelope.data.restoreItems)
    Assert-True ($restoreItems.Count -eq 1 -and $restoreItems[0].status -eq 'restored') 'Restore envelope did not report one terminal restored config item.'
    Assert-True ((Get-Sha256 $bundlePath) -eq $bundleHash) 'Bundle hash changed during restore.'

    Write-Phase 'Reverting to the exact sentinel bytes through the engine'
    Assert-WindowsTerminalClosed
    $revert = Invoke-Endstate -Executable $resolvedEngine -Command 'revert' -CommandArguments @()
    $revertedBytes = [System.IO.File]::ReadAllBytes($settingsPath)
    $preservedSentinelBytes = [System.IO.File]::ReadAllBytes($sentinelExpectedPath)
    Assert-True (Test-ByteArrayEqual $revertedBytes $preservedSentinelBytes) 'Engine revert did not restore the exact pre-restore sentinel bytes.'
    Assert-True (Test-ContainsBytes $revertedBytes $sentinelBytes) 'Sentinel was absent after engine revert.'
    $revertResults = @($revert.Envelope.data.results)
    Assert-True ($revertResults.Count -eq 1 -and $revertResults[0].action -eq 'reverted') 'Revert envelope did not report one reverted config action.'
    Assert-True ((Get-Sha256 $bundlePath) -eq $bundleHash) 'Bundle hash changed during revert.'

    $workflowPassed = $true
    Write-Host '    PASS: capture -> restore -> revert preserved exact bytes and bundle hash'
}
catch {
    $primaryError = $_
}
finally {
    Write-Phase 'Restoring independent original backup'
    try {
        if (-not (Test-Path -LiteralPath $backupPath -PathType Leaf)) {
            throw "Independent backup is missing: $backupPath"
        }
        Assert-True ((Get-Sha256 $backupPath) -eq $originalHash) 'Independent backup failed its final hash verification.'
		if ($settingsMutationStarted) {
			$runningDuringRecovery = @(Get-Process -Name 'WindowsTerminal' -ErrorAction SilentlyContinue)
			if ($runningDuringRecovery.Count -gt 0) {
				[Console]::Error.WriteLine('RECOVERY OVERRIDE: Windows Terminal opened during the smoke test; restoring the verified original backup anyway.')
			}
			$backupBytes = [System.IO.File]::ReadAllBytes($backupPath)
			[System.IO.File]::WriteAllBytes($settingsPath, $backupBytes)
			$finalBytes = [System.IO.File]::ReadAllBytes($settingsPath)
			Assert-True (Test-ByteArrayEqual $finalBytes $originalBytes) 'Final settings bytes do not equal the independent original backup.'
			Assert-True ((Get-Sha256 $settingsPath) -eq $originalHash) 'Final settings hash does not equal the original hash.'
			Assert-True (-not (Test-ContainsBytes $finalBytes $sentinelBytes)) 'Sentinel remained after final backup restoration.'
		}
		elseif ($workflowPassed) {
			throw 'Smoke workflow reported success without crossing the settings mutation boundary.'
		}

        Remove-VerifiedSmokeDirectory -Path $scratchRoot -ExpectedPrefix 'endstate-wt-smoke-'
        if ($workflowPassed -and $null -eq $primaryError) {
			Remove-VerifiedBackup -Root $backupRoot -Path $backupPath
        }
        else {
			Write-BackupRecoveryStatus -Path $backupPath -ExpectedHash $originalHash
        }
    }
    catch {
        $cleanupError = $_
		Write-BackupRecoveryStatus -Path $backupPath -ExpectedHash $originalHash
    }
    [Environment]::SetEnvironmentVariable('ENDSTATE_ROOT', $previousEndstateRoot, 'Process')
}

if ($null -ne $cleanupError) {
    if ($null -ne $primaryError) {
        throw "Smoke test failed: $($primaryError.Exception.Message) Final cleanup also failed: $($cleanupError.Exception.Message)"
    }
    throw $cleanupError
}
if ($null -ne $primaryError) {
    throw $primaryError
}

Write-Phase 'PASS: live Windows Terminal settings restored to their original hash'
