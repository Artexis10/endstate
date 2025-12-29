<#
.SYNOPSIS
    Canonical entrypoint for running engine tests with vendored Pester 5.

.DESCRIPTION
    This script is the ONLY supported way to run engine tests.
    It enforces deterministic usage of vendored Pester 5.x and fails fast
    if Pester < 5 is detected.

    Usage:
        pwsh scripts/test-engine.ps1

.PARAMETER Path
    Optional path to specific test file or directory. Defaults to tests/unit.

.PARAMETER Tag
    Optional tag filter for running specific test categories.

.PARAMETER IncludeIntegration
    Include integration tests (cli.tests.ps1, capture.tests.ps1, Endstate.Tests.ps1).

.EXAMPLE
    .\scripts\test-engine.ps1
    Run all engine unit tests.

.EXAMPLE
    .\scripts\test-engine.ps1 -Path "tests/unit/Manifest.Tests.ps1"
    Run a specific test file.

.EXAMPLE
    .\scripts\test-engine.ps1 -IncludeIntegration
    Run unit tests plus integration tests.

.NOTES
    This repo enforces Pester 5.x for all tests.
    Pester is vendored in tools/pester/ for deterministic, offline-capable execution.
    Tests will FAIL IMMEDIATELY if Pester < 5 is detected.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$Path,
    
    [Parameter(Mandatory = $false)]
    [string[]]$Tag,
    
    [Parameter(Mandatory = $false)]
    [switch]$IncludeIntegration
)

$ErrorActionPreference = "Stop"
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:MinPesterVersion = [Version]"5.0.0"
$script:VendorPath = Join-Path $script:RepoRoot "tools\pester"

# ============================================================================
# PESTER 5 ENFORCEMENT - FAIL FAST
# ============================================================================

function Assert-Pester5 {
    <#
    .SYNOPSIS
        Hard-enforce Pester 5.x. Fails immediately if not available.
    #>
    
    # Step 1: Remove any pre-loaded Pester module to avoid version conflicts
    $loadedPester = Get-Module -Name Pester
    if ($loadedPester) {
        Remove-Module -Name Pester -Force -ErrorAction SilentlyContinue
    }
    
    # Step 2: Locate vendored Pester manifest
    $pesterManifest = Get-ChildItem -Path $script:VendorPath -Filter "Pester.psd1" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
    
    if (-not $pesterManifest) {
        Write-Host ""
        Write-Host "========================================" -ForegroundColor Red
        Write-Host " FATAL: Vendored Pester 5 not found" -ForegroundColor Red
        Write-Host "========================================" -ForegroundColor Red
        Write-Host ""
        Write-Host "Expected location: $script:VendorPath" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "To bootstrap Pester 5.x locally, run:" -ForegroundColor Cyan
        Write-Host "  Save-Module -Name Pester -Path tools/pester -RequiredVersion 5.7.1 -Repository PSGallery" -ForegroundColor White
        Write-Host ""
        exit 1
    }
    
    # Step 3: Validate version from manifest BEFORE importing
    try {
        $manifestData = Import-PowerShellDataFile -Path $pesterManifest.FullName
        $vendoredVersion = [Version]$manifestData.ModuleVersion
    } catch {
        Write-Host ""
        Write-Host "FATAL: Could not read Pester manifest: $($pesterManifest.FullName)" -ForegroundColor Red
        Write-Host "Error: $_" -ForegroundColor Red
        exit 1
    }
    
    if ($vendoredVersion -lt $script:MinPesterVersion) {
        Write-Host ""
        Write-Host "========================================" -ForegroundColor Red
        Write-Host " FATAL: Pester version < 5 detected" -ForegroundColor Red
        Write-Host "========================================" -ForegroundColor Red
        Write-Host ""
        Write-Host "Found version: $vendoredVersion" -ForegroundColor Yellow
        Write-Host "Required: >= $script:MinPesterVersion" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "This repository requires Pester 5.x for deterministic test execution." -ForegroundColor Cyan
        Write-Host "Pester 3.x/4.x have different semantics and are NOT supported." -ForegroundColor Cyan
        Write-Host ""
        exit 1
    }
    
    # Step 4: Prepend vendor path to PSModulePath (ensures vendored takes precedence)
    if ($env:PSModulePath -notlike "*$script:VendorPath*") {
        $env:PSModulePath = "$script:VendorPath$([IO.Path]::PathSeparator)$env:PSModulePath"
    }
    
    # Step 5: Import vendored Pester explicitly
    try {
        Import-Module $pesterManifest.FullName -Force -ErrorAction Stop
    } catch {
        Write-Host ""
        Write-Host "FATAL: Failed to import vendored Pester module" -ForegroundColor Red
        Write-Host "Path: $($pesterManifest.FullName)" -ForegroundColor Yellow
        Write-Host "Error: $_" -ForegroundColor Red
        exit 1
    }
    
    # Step 6: Final verification - confirm loaded module is Pester 5+
    $loadedPester = Get-Module -Name Pester
    if (-not $loadedPester) {
        Write-Host ""
        Write-Host "FATAL: Pester module not loaded after import" -ForegroundColor Red
        exit 1
    }
    
    if ($loadedPester.Version -lt $script:MinPesterVersion) {
        Write-Host ""
        Write-Host "========================================" -ForegroundColor Red
        Write-Host " FATAL: Wrong Pester version loaded" -ForegroundColor Red
        Write-Host "========================================" -ForegroundColor Red
        Write-Host ""
        Write-Host "Loaded: $($loadedPester.Version)" -ForegroundColor Yellow
        Write-Host "Required: >= $script:MinPesterVersion" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "A system-installed Pester may have taken precedence." -ForegroundColor Cyan
        Write-Host "Ensure vendored Pester is correctly installed in: $script:VendorPath" -ForegroundColor Cyan
        Write-Host ""
        exit 1
    }
    
    return $loadedPester
}

# ============================================================================
# MAIN
# ============================================================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Endstate Engine Tests" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Enforce Pester 5 - fails fast if not available
$pester = Assert-Pester5

Write-Host "Pester version: $($pester.Version) (vendored)" -ForegroundColor Green
Write-Host "PowerShell: $($PSVersionTable.PSVersion)" -ForegroundColor DarkGray
Write-Host ""

# Determine test paths
$unitTestDir = Join-Path $script:RepoRoot "tests\unit"
$testPaths = @()

if ($Path) {
    # Specific path provided
    if ([System.IO.Path]::IsPathRooted($Path)) {
        $testPaths = @($Path)
    } else {
        $testPaths = @(Join-Path $script:RepoRoot $Path)
    }
} else {
    # Default: unit tests
    if (Test-Path $unitTestDir) {
        $testPaths = @($unitTestDir)
    }
    
    # Include integration tests if requested
    if ($IncludeIntegration) {
        $integrationTests = @(
            (Join-Path $script:RepoRoot "tests\Endstate.Tests.ps1"),
            (Join-Path $script:RepoRoot "tests\cli.tests.ps1"),
            (Join-Path $script:RepoRoot "tests\capture.tests.ps1")
        ) | Where-Object { Test-Path $_ }
        
        $testPaths += $integrationTests
    }
}

if ($testPaths.Count -eq 0) {
    Write-Host "[ERROR] No test paths found." -ForegroundColor Red
    exit 1
}

$modeLabel = if ($IncludeIntegration) { "Unit + Integration" } else { "Unit only" }
Write-Host "Mode: $modeLabel" -ForegroundColor DarkGray

foreach ($tp in $testPaths) {
    Write-Host "Test path: $tp" -ForegroundColor DarkGray
}
Write-Host ""

# Configure Pester 5
$config = New-PesterConfiguration

$config.Run.Path = $testPaths
$config.Run.Exit = $true
$config.Run.PassThru = $true

$config.Output.Verbosity = "Detailed"
$config.Output.StackTraceVerbosity = "Filtered"
$config.Output.CIFormat = "Auto"

$config.TestResult.Enabled = $true
$config.TestResult.OutputPath = Join-Path $script:RepoRoot "test-results.xml"
$config.TestResult.OutputFormat = "NUnitXml"

$config.Should.ErrorAction = "Continue"

# Apply tag filter if specified
if ($Tag) {
    $config.Filter.Tag = $Tag
}

# Run tests
Write-Host "Running tests..." -ForegroundColor Cyan
Write-Host ""

$result = Invoke-Pester -Configuration $config

# Summary
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Test Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
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
