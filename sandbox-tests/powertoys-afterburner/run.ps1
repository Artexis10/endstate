<#
.SYNOPSIS
    Windows Sandbox contract-test harness for Endstate engine.

.DESCRIPTION
    Tests the engine contract: apply → assert state → apply again (idempotent).
    Uses powertoys and msi-afterburner modules.
    
    This is NOT app-behavior testing. It is engine contract testing.

.NOTES
    Run inside Windows Sandbox with the repo mapped.
    Exit code 0 = all assertions passed.
    Exit code 1 = one or more assertions failed.
#>

$ErrorActionPreference = 'Stop'

# Resolve paths relative to this script
$script:HarnessRoot = $PSScriptRoot
$script:RepoRoot = (Resolve-Path (Join-Path $script:HarnessRoot "..\..")).Path
$script:ManifestPath = Join-Path $script:HarnessRoot "manifest.jsonc"
$script:ExportDir = Join-Path $script:HarnessRoot "export"
$script:EndstateCmd = Join-Path $script:RepoRoot "bin\endstate.cmd"

# Sentinel paths for verification
$script:PowerToysSentinel = [System.Environment]::ExpandEnvironmentVariables("%LOCALAPPDATA%\Microsoft\PowerToys\settings.json")
$script:AfterburnerSentinel = "C:\Program Files (x86)\MSI Afterburner\MSIAfterburner.cfg"
$script:AfterburnerProfilesDir = "C:\Program Files (x86)\MSI Afterburner\Profiles"

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

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Skip {
    param([string]$Message)
    Write-Host "[SKIP] $Message" -ForegroundColor DarkYellow
}

# Track failures
$script:FailCount = 0

function Assert-FileExists {
    param(
        [string]$Path,
        [string]$Description
    )
    
    if (Test-Path $Path) {
        Write-Pass "$Description exists: $Path"
        return $true
    } else {
        Write-Fail "$Description missing: $Path"
        $script:FailCount++
        return $false
    }
}

function Assert-ExitCodeZero {
    param(
        [int]$ExitCode,
        [string]$Description
    )
    
    if ($ExitCode -eq 0) {
        Write-Pass "$Description (exit code: 0)"
        return $true
    } else {
        Write-Fail "$Description (exit code: $ExitCode)"
        $script:FailCount++
        return $false
    }
}

# ============================================================================
# MAIN HARNESS
# ============================================================================

Write-Header "Endstate Sandbox Contract Test"

Write-Info "Repo root: $script:RepoRoot"
Write-Info "Manifest: $script:ManifestPath"
Write-Info "Export dir: $script:ExportDir"
Write-Info "Endstate CLI: $script:EndstateCmd"

# Validate prerequisites
if (-not (Test-Path $script:EndstateCmd)) {
    Write-Fail "Endstate CLI not found at: $script:EndstateCmd"
    exit 1
}

if (-not (Test-Path $script:ManifestPath)) {
    Write-Fail "Manifest not found at: $script:ManifestPath"
    exit 1
}

# Create export directory
if (-not (Test-Path $script:ExportDir)) {
    New-Item -ItemType Directory -Path $script:ExportDir -Force | Out-Null
    Write-Info "Created export directory: $script:ExportDir"
}

# ============================================================================
# STEP 1: First Apply (with restore)
# ============================================================================

Write-Header "Step 1: First Apply (with -EnableRestore)"

Write-Step "Running: endstate apply -Manifest $script:ManifestPath -EnableRestore"

$applyArgs = @(
    "apply",
    "-Manifest", $script:ManifestPath,
    "-EnableRestore"
)

$applyResult = & $script:EndstateCmd @applyArgs
$applyExitCode = $LASTEXITCODE

Write-Info "Apply output:"
$applyResult | ForEach-Object { Write-Host "  $_" }

Assert-ExitCodeZero -ExitCode $applyExitCode -Description "First apply completed"

# ============================================================================
# STEP 2: Assert Sentinel Files Exist
# ============================================================================

Write-Header "Step 2: Assert Sentinel Files"

Write-Step "Checking PowerToys sentinel..."
$ptExists = Assert-FileExists -Path $script:PowerToysSentinel -Description "PowerToys settings.json"

Write-Step "Checking MSI Afterburner sentinel..."
$abCfgExists = Assert-FileExists -Path $script:AfterburnerSentinel -Description "MSI Afterburner config"

Write-Step "Checking MSI Afterburner Profiles directory..."
Assert-FileExists -Path $script:AfterburnerProfilesDir -Description "MSI Afterburner Profiles" | Out-Null

# Note: In a fresh Sandbox, these files may not exist if the apps aren't installed
# and the restore sources don't exist. This is expected behavior for contract testing.
if (-not $ptExists -and -not $abCfgExists) {
    Write-Skip "Sentinel files not present - this is expected in a fresh Sandbox without source configs"
    Write-Info "The contract test validates the engine runs without error, not that files are restored"
    Write-Info "(Restore requires source config files to exist in the manifest's relative paths)"
}

# ============================================================================
# STEP 3: Second Apply (Idempotency Check)
# ============================================================================

Write-Header "Step 3: Second Apply (Idempotency Check)"

Write-Step "Running: endstate apply -Manifest $script:ManifestPath -EnableRestore"

$apply2Result = & $script:EndstateCmd @applyArgs
$apply2ExitCode = $LASTEXITCODE

Write-Info "Second apply output:"
$apply2Result | ForEach-Object { Write-Host "  $_" }

Assert-ExitCodeZero -ExitCode $apply2ExitCode -Description "Second apply completed (idempotent)"

# Check for idempotency indicators in output
$idempotentIndicators = @("already", "skipped", "up-to-date", "no changes", "nothing to do")
$foundIdempotent = $false
foreach ($line in $apply2Result) {
    foreach ($indicator in $idempotentIndicators) {
        if ($line -match $indicator) {
            $foundIdempotent = $true
            Write-Pass "Idempotency indicator found: '$indicator' in output"
            break
        }
    }
    if ($foundIdempotent) { break }
}

if (-not $foundIdempotent) {
    Write-Info "No explicit idempotency indicator found in output (exit code 0 is sufficient)"
}

# ============================================================================
# STEP 4: Revert (Not Implemented)
# ============================================================================

Write-Header "Step 4: Revert (Not Implemented)"

Write-Skip "Revert command is not yet implemented in Endstate CLI"
Write-Info "When 'endstate revert' is available, add revert + cleanup assertions here"

# ============================================================================
# SUMMARY
# ============================================================================

Write-Header "Test Summary"

if ($script:FailCount -eq 0) {
    Write-Pass "All contract tests passed!"
    Write-Host ""
    exit 0
} else {
    Write-Fail "$script:FailCount assertion(s) failed"
    Write-Host ""
    exit 1
}
