<#
.SYNOPSIS
    Batch runner for Sandbox-based module validation.

.DESCRIPTION
    Reads a queue file and runs validation sequentially for each app.
    Produces a summary file with PASS/FAIL results for all apps.

.PARAMETER QueueFile
    Path to the queue file (JSONC). Default: sandbox-tests/golden-queue.jsonc

.PARAMETER OutDir
    Base output directory for all validation runs.
    Default: sandbox-tests/validation/batch/<timestamp>/

.PARAMETER StopOnFail
    Stop batch execution on first failure. Default: false (continue all).

.EXAMPLE
    .\scripts\sandbox-validate-batch.ps1

.EXAMPLE
    .\scripts\sandbox-validate-batch.ps1 -QueueFile "my-queue.jsonc" -StopOnFail
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$QueueFile,
    
    [Parameter(Mandatory = $false)]
    [string]$OutDir,
    
    [Parameter(Mandatory = $false)]
    [switch]$StopOnFail
)

$ErrorActionPreference = 'Stop'

# Resolve paths
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:ValidateScript = Join-Path $PSScriptRoot "sandbox-validate.ps1"

function Write-Header {
    param([string]$Message)
    Write-Host ""
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host " $Message" -ForegroundColor Cyan
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host ""
}

function Write-Step {
    param([string]$Message)
    Write-Host "[STEP] $Message" -ForegroundColor Yellow
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

# Load manifest.ps1 for JSONC parsing
$manifestModule = Join-Path $script:RepoRoot "engine\manifest.ps1"
if (Test-Path $manifestModule) {
    . $manifestModule
}

function Read-QueueJsonc {
    param([string]$Path)
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    if (Get-Command Read-JsoncFile -ErrorAction SilentlyContinue) {
        return Read-JsoncFile -Path $Path
    }
    
    # Fallback: strip comments and parse
    $content = Get-Content -Path $Path -Raw
    $content = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
    return $content | ConvertFrom-Json -AsHashtable
}

# ============================================================================
# VALIDATION
# ============================================================================

# Default queue file
if (-not $QueueFile) {
    $QueueFile = Join-Path $script:RepoRoot "sandbox-tests\golden-queue.jsonc"
}

if (-not (Test-Path $QueueFile)) {
    Write-Host "[ERROR] Queue file not found: $QueueFile" -ForegroundColor Red
    exit 1
}

# Verify validate script exists
if (-not (Test-Path $script:ValidateScript)) {
    Write-Host "[ERROR] Validate script not found: $script:ValidateScript" -ForegroundColor Red
    exit 1
}

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Sandbox Validation Batch Runner"

# Load queue
Write-Step "Loading queue file..."
$queue = Read-QueueJsonc -Path $QueueFile

if (-not $queue -or -not $queue.apps -or $queue.apps.Count -eq 0) {
    Write-Host "[ERROR] Queue file is empty or invalid" -ForegroundColor Red
    exit 1
}

Write-Info "Queue file: $QueueFile"
Write-Info "Apps to validate: $($queue.apps.Count)"

# Create output directory
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
if (-not $OutDir) {
    $OutDir = Join-Path $script:RepoRoot "sandbox-tests\validation\batch\$timestamp"
}

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}

Write-Info "Output directory: $OutDir"
Write-Host ""

# Track results
$results = @()
$passCount = 0
$failCount = 0
$skipCount = 0
$startTime = Get-Date

# Run validation for each app
foreach ($app in $queue.apps) {
    $appId = $app.appId
    $wingetId = $app.wingetId
    
    Write-Host ""
    Write-Host "-" * 60 -ForegroundColor DarkGray
    Write-Step "Validating: $appId ($wingetId)"
    Write-Host "-" * 60 -ForegroundColor DarkGray
    
    $appOutDir = Join-Path $OutDir $appId
    $appStartTime = Get-Date
    
    if ($app.sandboxSkip -eq $true) {
        Write-Host "[SKIP] ${appId}: marked sandboxSkip in queue" -ForegroundColor DarkYellow
        $results += @{
            appId = $appId
            wingetId = $wingetId
            status = "SKIPPED"
            reason = "sandboxSkip"
            duration = 0
            outputDir = $appOutDir
        }
        $skipCount++
        continue
    }
    
    if ($app.sandboxSupported -eq $false) {
        Write-Host "[SKIP] ${appId}: sandbox not supported" -ForegroundColor DarkYellow
        $results += @{
            appId = $appId
            wingetId = $wingetId
            status = "SKIPPED"
            skipReason = "sandbox_unsupported"
            duration = 0
            outputDir = $appOutDir
        }
        $skipCount++
        continue
    }
    
    # Run validation
    try {
        # Build argument string (not array) to preserve quoting for paths with spaces
        $argString = "-File `"$($script:ValidateScript)`" -AppId `"$appId`" -WingetId `"$wingetId`" -OutDir `"$appOutDir`""
        
        # Add offline installer fallback parameters if present in queue entry
        if ($app.installer) {
            if ($app.installer.path) {
                $argString += " -InstallerPath `"$($app.installer.path)`""
            }
            if ($app.installer.silentArgs) {
                $argString += " -InstallerArgs `"$($app.installer.silentArgs)`""
            }
            if ($app.installer.exePath) {
                $argString += " -InstallerExePath `"$($app.installer.exePath)`""
            }
        }
        
        # Launch with timeout (10 minutes per app)
        $timeoutMs = 600000
        $process = Start-Process -FilePath "powershell.exe" -ArgumentList $argString -PassThru -NoNewWindow
        $completed = $process.WaitForExit($timeoutMs)
        
        if (-not $completed) {
            Write-Host "[WARN] Timeout after 600s, killing process..." -ForegroundColor Yellow
            $process.Kill()
            $process.WaitForExit(5000)
            $exitCode = 124  # timeout exit code
            
            # Clean up lingering sandbox session
            try {
                Stop-Process -Name "WindowsSandboxClient" -Force -ErrorAction SilentlyContinue
                Stop-Process -Name "WindowsSandbox" -Force -ErrorAction SilentlyContinue
                Start-Sleep -Seconds 3
            } catch { }
        } else {
            $exitCode = $process.ExitCode
        }
        
        $appEndTime = Get-Date
        $duration = ($appEndTime - $appStartTime).TotalSeconds
        
        # Read result file if exists
        $resultFile = Join-Path $appOutDir "result.json"
        $appResult = @{
            appId = $appId
            wingetId = $wingetId
            status = if ($exitCode -eq 0) { "PASS" } elseif ($exitCode -eq 124) { "TIMEOUT" } else { "FAIL" }
            exitCode = $exitCode
            duration = [math]::Round($duration, 2)
            outputDir = $appOutDir
        }
        
        if (Test-Path $resultFile) {
            $detailedResult = Get-Content -Path $resultFile -Raw | ConvertFrom-Json
            $appResult.seedRan = $detailedResult.seedRan
            $appResult.capturedFiles = $detailedResult.capturedFiles
            $appResult.wipedFiles = $detailedResult.wipedFiles
            $appResult.restoredFiles = $detailedResult.restoredFiles
            $appResult.verifyPass = $detailedResult.verifyPass
            $appResult.verifyTotal = $detailedResult.verifyTotal
            if ($detailedResult.failedStage) {
                $appResult.failedStage = $detailedResult.failedStage
            }
            if ($detailedResult.failReason) {
                $appResult.failReason = $detailedResult.failReason
            }
        }
        
        $results += $appResult
        
        if ($exitCode -eq 0) {
            $passCount++
            Write-Pass "${appId}: PASSED (${duration}s)"
        } elseif ($exitCode -eq 124) {
            $failCount++
            Write-Host "[TIME] ${appId}: TIMEOUT after ${duration}s" -ForegroundColor Magenta
        } else {
            $failCount++
            Write-Fail "${appId}: FAILED (${duration}s)"
            
            if ($StopOnFail) {
                Write-Host ""
                Write-Host "[STOP] StopOnFail enabled, stopping batch execution" -ForegroundColor Yellow
                break
            }
        }
    } catch {
        $appEndTime = Get-Date
        $duration = ($appEndTime - $appStartTime).TotalSeconds
        
        $appResult = @{
            appId = $appId
            wingetId = $wingetId
            status = "ERROR"
            error = $_.Exception.Message
            duration = [math]::Round($duration, 2)
            outputDir = $appOutDir
        }
        $results += $appResult
        $failCount++
        
        Write-Fail "${appId}: ERROR - $($_.Exception.Message)"
        
        if ($StopOnFail) {
            Write-Host ""
            Write-Host "[STOP] StopOnFail enabled, stopping batch execution" -ForegroundColor Yellow
            break
        }
    }
}

$endTime = Get-Date
$totalDuration = ($endTime - $startTime).TotalSeconds

# ============================================================================
# Generate Summary
# ============================================================================
Write-Host ""
Write-Header "Batch Validation Summary"

$summary = @{
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    queueFile = $QueueFile
    outputDir = $OutDir
    totalApps = $queue.apps.Count
    passed = $passCount
    failed = $failCount
    skipped = $skipCount
    duration = [math]::Round($totalDuration, 2)
    results = $results
}

# Write JSON summary
$summaryJsonPath = Join-Path $OutDir "summary.json"
$summary | ConvertTo-Json -Depth 10 | Out-File -FilePath $summaryJsonPath -Encoding UTF8

# Write Markdown summary
$summaryMdPath = Join-Path $OutDir "summary.md"
$mdContent = @"
# Sandbox Validation Summary

**Date:** $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")
**Queue:** $QueueFile
**Duration:** $([math]::Round($totalDuration, 2))s

## Results

| App | Winget ID | Status | Duration | Details |
|-----|-----------|--------|----------|---------|
"@

foreach ($r in $results) {
    $statusEmoji = if ($r.status -eq "PASS") { "✅" } elseif ($r.status -eq "SKIPPED") { "⏭️" } elseif ($r.status -eq "TIMEOUT") { "⏱️" } elseif ($r.status -eq "FAIL") { "❌" } else { "⚠️" }
    $details = if ($r.status -eq "PASS") {
        "Verify: $($r.verifyPass)/$($r.verifyTotal)"
    } elseif ($r.failReason) {
        $r.failReason
    } elseif ($r.error) {
        $r.error
    } else {
        "-"
    }
    $mdContent += "`n| $($r.appId) | $($r.wingetId) | $statusEmoji $($r.status) | $($r.duration)s | $details |"
}

$mdContent += @"

## Summary

- **Total Apps:** $($queue.apps.Count)
- **Passed:** $passCount
- **Failed:** $failCount
- **Skipped:** $skipCount
- **Pass Rate:** $([math]::Round(($passCount / $queue.apps.Count) * 100, 1))%

## Artifacts

- JSON Summary: ``summary.json``
- Per-app results in subdirectories
"@

$mdContent | Out-File -FilePath $summaryMdPath -Encoding UTF8

# Console summary
Write-Host "  Total Apps:  $($queue.apps.Count)" -ForegroundColor White
Write-Host "  Passed:      $passCount" -ForegroundColor Green
Write-Host "  Failed:      $failCount" -ForegroundColor $(if ($failCount -gt 0) { "Red" } else { "White" })
Write-Host "  Skipped:     $skipCount" -ForegroundColor $(if ($skipCount -gt 0) { "DarkYellow" } else { "White" })
Write-Host "  Duration:    $([math]::Round($totalDuration, 2))s" -ForegroundColor White
Write-Host ""

# Per-app results
Write-Host "  Results:" -ForegroundColor White
foreach ($r in $results) {
    $statusColor = if ($r.status -eq "PASS") { "Green" } elseif ($r.status -eq "FAIL") { "Red" } else { "Yellow" }
    Write-Host "    - $($r.appId): $($r.status)" -ForegroundColor $statusColor
}

Write-Host ""
Write-Host "  Summary JSON: $summaryJsonPath" -ForegroundColor Green
Write-Host "  Summary MD:   $summaryMdPath" -ForegroundColor Green
Write-Host ""

# Exit with appropriate code
if ($failCount -gt 0) {
    exit 1
} else {
    exit 0
}
