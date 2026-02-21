<#
.SYNOPSIS
    Batch validation of all scaffolded modules via Windows Sandbox.

.DESCRIPTION
    Runs sandbox-validate.ps1 sequentially for each module.
    Each sandbox session: install -> seed -> capture -> wipe -> restore -> verify
    
    Estimated time: 3-10 minutes per module, 2-7 hours total for 40 modules.

.PARAMETER Skip
    Array of module IDs to skip (already validated or known issues).

.PARAMETER Only
    Array of module IDs to validate (ignore others).

.PARAMETER StartFrom
    Module ID to start from (skip all before it in the list).

.PARAMETER DryRun
    Show what would be validated without running.

.EXAMPLE
    .\scripts\batch-validate.ps1
    # Validate all modules

.EXAMPLE
    .\scripts\batch-validate.ps1 -Skip @('git', 'vscodium')
    # Skip already-validated modules

.EXAMPLE
    .\scripts\batch-validate.ps1 -Only @('powertoys', 'vscode', 'obsidian')
    # Validate specific modules only

.EXAMPLE
    .\scripts\batch-validate.ps1 -StartFrom 'intellij-idea'
    # Resume from a specific module
#>
[CmdletBinding()]
param(
    [string[]]$Skip = @(),
    [string[]]$Only = @(),
    [string]$StartFrom,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $script:RepoRoot "engine\manifest.ps1")

# All scaffolded modules (excluding already-validated git, vscodium)
$allModules = @(
    # Tier 1 - Editors/IDEs
    'vscode', 'cursor', 'windsurf', 'intellij-idea', 'pycharm', 'sublime-text',
    'notepad-plus-plus', 'windows-terminal',
    
    # Tier 1 - Productivity
    'obsidian', 'totalcommander', 'powertoys', 'sharex', 'autohotkey',
    
    # Tier 1 - Creative (Photo/Video)
    'lightroom-classic', 'capture-one', 'dxo-photolab', 'davinci-resolve',
    'obs-studio', 'reaper',
    
    # Tier 2 - Creative
    'affinity-photo', 'fl-studio', 'ableton-live', 'audacity', 'foobar2000',
    
    # Tier 2 - Productivity
    'logseq', 'directory-opus', 'ditto', 'keepassxc',
    
    # Tier 2 - Editors
    'webstorm', 'fastrawviewer',
    
    # Tier 2 - Adobe (manual install)
    'premiere-pro', 'after-effects',
    
    # Tier 2 - System/Hardware
    'hwinfo', 'evga-precision-x1', 'openrgb',
    
    # Tier 2 - Media
    'plex', 'kodi', 'mpv',
    
    # Tier 2 - Network (sensitive)
    'wireguard'
)

# Filter modules
$modules = $allModules

if ($Only.Count -gt 0) {
    $modules = $modules | Where-Object { $_ -in $Only }
}

if ($Skip.Count -gt 0) {
    $modules = $modules | Where-Object { $_ -notin $Skip }
}

if ($StartFrom) {
    $startIndex = [array]::IndexOf($modules, $StartFrom)
    if ($startIndex -lt 0) {
        Write-Host "[ERROR] Module '$StartFrom' not found in list" -ForegroundColor Red
        exit 1
    }
    $modules = $modules[$startIndex..($modules.Count - 1)]
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " Batch Module Validation" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""
Write-Host "Modules to validate: $($modules.Count)" -ForegroundColor White
Write-Host "Estimated time: $($modules.Count * 5) - $($modules.Count * 10) minutes" -ForegroundColor Yellow
Write-Host ""

if ($DryRun) {
    Write-Host "DRY RUN - Would validate:" -ForegroundColor Yellow
    $modules | ForEach-Object { Write-Host "  - $_" }
    exit 0
}

# Results tracking
$results = [ordered]@{}
$startTime = Get-Date
$logFile = Join-Path $script:RepoRoot "sandbox-tests\validation\batch-$(Get-Date -Format 'yyyyMMdd-HHmmss').log"

# Ensure log directory exists
$logDir = Split-Path $logFile -Parent
if (-not (Test-Path $logDir)) {
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
}

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $line = "[$timestamp] $Message"
    Add-Content -Path $logFile -Value $line
    Write-Host $line
}

Write-Log "Batch validation started"
Write-Log "Modules: $($modules -join ', ')"

$validated = 0
$passed = 0
$failed = 0
$skippedNoWinget = 0

foreach ($mod in $modules) {
    $validated++
    Write-Host ""
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host " [$validated/$($modules.Count)] Validating: $mod" -ForegroundColor Cyan
    Write-Host "=" * 60 -ForegroundColor Cyan
    
    $modStart = Get-Date
    
    # Check if module has winget ID
    $modulePath = Join-Path $script:RepoRoot "modules\apps\$mod\module.jsonc"
    $module = Read-JsoncFile -Path $modulePath
    
    if (-not $module.matches.winget -or $module.matches.winget.Count -eq 0) {
        Write-Host "[SKIP] $mod has no winget ID (manual install required)" -ForegroundColor Yellow
        $results[$mod] = @{ status = 'SKIP'; reason = 'no-winget'; duration = 0 }
        $skippedNoWinget++
        Write-Log "$mod : SKIP (no winget ID)"
        continue
    }
    
    # Run validation
    try {
        & "$script:RepoRoot\scripts\sandbox-validate.ps1" -AppId $mod
        $exitCode = $LASTEXITCODE
        
        $duration = (Get-Date) - $modStart
        
        if ($exitCode -eq 0) {
            $results[$mod] = @{ status = 'PASS'; duration = $duration.TotalSeconds }
            $passed++
            Write-Log "$mod : PASS ($('{0:N0}' -f $duration.TotalSeconds)s)"
        } else {
            $results[$mod] = @{ status = 'FAIL'; exitCode = $exitCode; duration = $duration.TotalSeconds }
            $failed++
            Write-Log "$mod : FAIL (exit $exitCode, $('{0:N0}' -f $duration.TotalSeconds)s)"
        }
    } catch {
        $duration = (Get-Date) - $modStart
        $results[$mod] = @{ status = 'ERROR'; error = $_.Exception.Message; duration = $duration.TotalSeconds }
        $failed++
        Write-Log "$mod : ERROR - $($_.Exception.Message)"
    }
    
    # Progress summary
    $elapsed = (Get-Date) - $startTime
    $remaining = $modules.Count - $validated
    $avgTime = $elapsed.TotalSeconds / $validated
    $eta = [TimeSpan]::FromSeconds($avgTime * $remaining)
    
    Write-Host ""
    Write-Host "Progress: $validated/$($modules.Count) | Pass: $passed | Fail: $failed | Skip: $skippedNoWinget" -ForegroundColor White
    Write-Host "Elapsed: $('{0:hh\:mm\:ss}' -f $elapsed) | ETA: $('{0:hh\:mm\:ss}' -f $eta)" -ForegroundColor Gray
}

# Final summary
$totalTime = (Get-Date) - $startTime

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " BATCH VALIDATION COMPLETE" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""
Write-Host "Total time: $('{0:hh\:mm\:ss}' -f $totalTime)" -ForegroundColor White
Write-Host ""
Write-Host "Results:" -ForegroundColor White
Write-Host "  PASS: $passed" -ForegroundColor Green
Write-Host "  FAIL: $failed" -ForegroundColor $(if ($failed -gt 0) { 'Red' } else { 'Green' })
Write-Host "  SKIP: $skippedNoWinget (no winget)" -ForegroundColor Yellow
Write-Host ""

Write-Host "Details:" -ForegroundColor White
$results.GetEnumerator() | ForEach-Object {
    $color = switch ($_.Value.status) {
        'PASS' { 'Green' }
        'FAIL' { 'Red' }
        'ERROR' { 'Red' }
        'SKIP' { 'Yellow' }
        default { 'White' }
    }
    $duration = if ($_.Value.duration -gt 0) { " ($('{0:N0}' -f $_.Value.duration)s)" } else { "" }
    Write-Host "  $($_.Key): $($_.Value.status)$duration" -ForegroundColor $color
}

Write-Host ""
Write-Host "Log file: $logFile" -ForegroundColor Gray

Write-Log "Batch validation completed: $passed passed, $failed failed, $skippedNoWinget skipped"

# Exit with failure if any failed
if ($failed -gt 0) {
    exit 1
}
exit 0
