<#
.SYNOPSIS
    Run Pester tests for Provisioning.

.DESCRIPTION
    DEPRECATED: Use scripts/test-engine.ps1 instead.
    
    This script now delegates to the canonical test entrypoint which
    enforces vendored Pester 5.x for deterministic test execution.

.PARAMETER IncludeIntegration
    Also run integration tests (cli.tests.ps1, capture.tests.ps1).
    These may be slow or require external tools like winget.

.EXAMPLE
    .\run-tests.ps1
    Run unit tests only (fast, no external dependencies).

.EXAMPLE
    .\run-tests.ps1 -IncludeIntegration
    Run all tests including integration tests.

.NOTES
    DEPRECATED: Prefer using scripts/test-engine.ps1 directly:
        pwsh scripts/test-engine.ps1
        pwsh scripts/test-engine.ps1 -IncludeIntegration
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [switch]$IncludeIntegration
)

$ErrorActionPreference = "Stop"
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:MinPesterVersion = [Version]"5.0.0"
$script:VendorPath = Join-Path $script:RepoRoot "tools\pester"

Write-Host ""
Write-Host "[DEPRECATED] This script is deprecated. Use: pwsh scripts/test-engine.ps1" -ForegroundColor Yellow
Write-Host ""

# ============================================================================
# PESTER 5 ENFORCEMENT - FAIL FAST
# ============================================================================

# Remove any pre-loaded Pester to avoid version conflicts
$loadedPester = Get-Module -Name Pester
if ($loadedPester) {
    Remove-Module -Name Pester -Force -ErrorAction SilentlyContinue
}

# Locate vendored Pester manifest
$pesterManifest = Get-ChildItem -Path $script:VendorPath -Filter "Pester.psd1" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1

if (-not $pesterManifest) {
    Write-Host "[ERROR] Vendored Pester 5 not found at: $script:VendorPath" -ForegroundColor Red
    Write-Host "Run: Save-Module -Name Pester -Path tools/pester -RequiredVersion 5.7.1 -Repository PSGallery" -ForegroundColor Yellow
    exit 1
}

# Validate version from manifest
try {
    $manifestData = Import-PowerShellDataFile -Path $pesterManifest.FullName
    $vendoredVersion = [Version]$manifestData.ModuleVersion
} catch {
    Write-Host "[ERROR] Could not read Pester manifest: $_" -ForegroundColor Red
    exit 1
}

if ($vendoredVersion -lt $script:MinPesterVersion) {
    Write-Host "[ERROR] Pester version < 5 detected ($vendoredVersion). This repo requires Pester 5.x." -ForegroundColor Red
    exit 1
}

# Prepend vendor path to PSModulePath
if ($env:PSModulePath -notlike "*$script:VendorPath*") {
    $env:PSModulePath = "$script:VendorPath$([IO.Path]::PathSeparator)$env:PSModulePath"
}

# Import vendored Pester
Import-Module $pesterManifest.FullName -Force -ErrorAction Stop

$pester = Get-Module -Name Pester
if (-not $pester -or $pester.Version -lt $script:MinPesterVersion) {
    Write-Host "[ERROR] Failed to load Pester 5.x" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "Provisioning Tests" -ForegroundColor Cyan
Write-Host "==================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Pester version: $($pester.Version) (vendored)" -ForegroundColor DarkGray

# Build explicit test paths - unit tests only by default
$unitTestDir = Join-Path $PSScriptRoot "unit"
$testPaths = @()

if (Test-Path $unitTestDir) {
    $unitTests = Get-ChildItem -Path $unitTestDir -Filter "*.Tests.ps1" -File
    foreach ($test in $unitTests) {
        $testPaths += $test.FullName
    }
}

if ($IncludeIntegration) {
    Write-Host "Mode: Unit + Integration tests" -ForegroundColor Yellow
    # Add integration tests from root tests directory
    $integrationTests = Get-ChildItem -Path $PSScriptRoot -Filter "*.tests.ps1" -File
    foreach ($test in $integrationTests) {
        $testPaths += $test.FullName
    }
} else {
    Write-Host "Mode: Unit tests only (use -IncludeIntegration for all)" -ForegroundColor DarkGray
}

Write-Host "Test files: $($testPaths.Count)" -ForegroundColor DarkGray
Write-Host ""

if ($testPaths.Count -eq 0) {
    Write-Host "[WARN] No test files found." -ForegroundColor Yellow
    exit 0
}

# Pester 5.x configuration (Pester 3.x/4.x fallback removed - not supported)
$config = New-PesterConfiguration
$config.Run.Path = $testPaths
$config.Run.Exit = $false
$config.Output.Verbosity = "Detailed"
$config.TestResult.Enabled = $true
$config.TestResult.OutputPath = Join-Path $PSScriptRoot "test-results.xml"

$result = Invoke-Pester -Configuration $config
$failedCount = $result.FailedCount

# Summary
Write-Host ""
if ($failedCount -eq 0) {
    Write-Host "All tests passed!" -ForegroundColor Green
} else {
    Write-Host "$failedCount test(s) failed." -ForegroundColor Red
}

exit $failedCount
