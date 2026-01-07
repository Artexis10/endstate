<#
.SYNOPSIS
    Blessed unit test runner for Endstate. Enforces Pester 5+ as a hard contract.

.DESCRIPTION
    This script is the ONLY allowed entrypoint for running unit tests.
    It guarantees Pester 5+ is loaded and will hard-fail if Pester < 5 is detected.
    
    DO NOT call Invoke-Pester directly. Always use this script.

.PARAMETER Path
    Optional path to specific test file or directory. Defaults to tests/unit.

.EXAMPLE
    .\scripts\test-unit.ps1
    Run all unit tests.

.EXAMPLE
    .\scripts\test-unit.ps1 -Path "tests/unit/Manifest.Tests.ps1"
    Run a specific test file.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$Path
)

$ErrorActionPreference = "Stop"
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:MinPesterVersion = [Version]"5.0.0"

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Endstate Unit Tests" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Step 1: Remove any already-loaded Pester module (critical for avoiding Pester 3.x)
$loadedPester = Get-Module -Name Pester
if ($loadedPester) {
    Write-Host "[CLEANUP] Removing already-loaded Pester $($loadedPester.Version)..." -ForegroundColor Yellow
    Remove-Module -Name Pester -Force -ErrorAction SilentlyContinue
}

# Step 2: Prepend vendored Pester to PSModulePath
$vendoredPesterPath = Join-Path $script:RepoRoot "tools\pester"
if (Test-Path $vendoredPesterPath) {
    $env:PSModulePath = "$vendoredPesterPath;$env:PSModulePath"
    Write-Host "[INFO] Vendored Pester path added: $vendoredPesterPath" -ForegroundColor DarkGray
}

# Step 3: Import Pester with minimum version requirement
try {
    Import-Module Pester -MinimumVersion $script:MinPesterVersion -ErrorAction Stop
} catch {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Red
    Write-Host " FATAL: Pester 5+ Required" -ForegroundColor Red
    Write-Host "========================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "This project requires Pester >= $script:MinPesterVersion" -ForegroundColor Red
    Write-Host "Detected error: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host ""
    Write-Host "Diagnostic checklist:" -ForegroundColor Yellow
    Write-Host "  1. Check vendored Pester: Test-Path '$vendoredPesterPath'" -ForegroundColor Yellow
    Write-Host "  2. Check available versions: Get-Module Pester -ListAvailable | Select Name,Version,Path" -ForegroundColor Yellow
    Write-Host "  3. Bootstrap vendored Pester: Save-Module Pester -Path tools/pester -RequiredVersion 5.7.1" -ForegroundColor Yellow
    Write-Host ""
    exit 1
}

# Step 4: Verify loaded Pester version (hard-fail if < 5)
$pester = Get-Module -Name Pester
if (-not $pester) {
    Write-Host "[FATAL] Pester module not loaded after import." -ForegroundColor Red
    exit 1
}

if ($pester.Version -lt $script:MinPesterVersion) {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Red
    Write-Host " FATAL: Pester Version Too Old" -ForegroundColor Red
    Write-Host "========================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "Loaded Pester version: $($pester.Version)" -ForegroundColor Red
    Write-Host "Required minimum: $script:MinPesterVersion" -ForegroundColor Red
    Write-Host "Loaded from: $($pester.ModuleBase)" -ForegroundColor Red
    Write-Host ""
    Write-Host "The system Pester 3.x was loaded instead of vendored Pester 5+." -ForegroundColor Red
    Write-Host "This is a project contract violation." -ForegroundColor Red
    Write-Host ""
    exit 1
}

Write-Host "[OK] Pester version: $($pester.Version)" -ForegroundColor Green
Write-Host "[OK] Loaded from: $($pester.ModuleBase)" -ForegroundColor DarkGray
Write-Host "[OK] PowerShell: $($PSVersionTable.PSVersion)" -ForegroundColor DarkGray
Write-Host ""

# Step 5: Determine test path
$testPath = if ($Path) {
    if ([System.IO.Path]::IsPathRooted($Path)) {
        $Path
    } else {
        Join-Path $script:RepoRoot $Path
    }
} else {
    Join-Path $script:RepoRoot "tests\unit"
}

if (-not (Test-Path $testPath)) {
    Write-Host "[ERROR] Test path not found: $testPath" -ForegroundColor Red
    exit 1
}

Write-Host "Test path: $testPath" -ForegroundColor DarkGray
Write-Host ""

# Step 6: Configure and run Pester
$config = New-PesterConfiguration

$config.Run.Path = $testPath
$config.Run.Exit = $true
$config.Run.PassThru = $true

$config.Output.Verbosity = "Detailed"
$config.Output.StackTraceVerbosity = "Filtered"
$config.Output.CIFormat = "Auto"

$config.TestResult.Enabled = $true
$config.TestResult.OutputPath = Join-Path $script:RepoRoot "test-results.xml"
$config.TestResult.OutputFormat = "NUnitXml"

$config.Should.ErrorAction = "Continue"

Write-Host "Running unit tests..." -ForegroundColor Cyan
Write-Host ""

$result = Invoke-Pester -Configuration $config

# Step 7: Summary
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Test Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Pester:  $($pester.Version)" -ForegroundColor DarkGray
Write-Host "  Total:   $($result.TotalCount)" -ForegroundColor White
Write-Host "  Passed:  $($result.PassedCount)" -ForegroundColor Green
Write-Host "  Failed:  $($result.FailedCount)" -ForegroundColor $(if ($result.FailedCount -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped: $($result.SkippedCount)" -ForegroundColor DarkGray
Write-Host ""

if ($result.FailedCount -eq 0) {
    Write-Host "All tests passed!" -ForegroundColor Green
    exit 0
} else {
    Write-Host "$($result.FailedCount) test(s) failed." -ForegroundColor Red
    exit 1
}
