<#
.SYNOPSIS
    Root test entrypoint for Pester tests.

.DESCRIPTION
    Runs all Pester tests in the endstate repository.
    Bootstraps Pester 5.5.0+ via ensure-pester.ps1 for deterministic test runs.

.PARAMETER Path
    Optional path to specific test file or directory. Defaults to all tests.

.PARAMETER Tag
    Optional tag filter for running specific test categories.

.EXAMPLE
    .\scripts\test_pester.ps1
    Run all tests.

.EXAMPLE
    .\scripts\test_pester.ps1 -Path "tests/unit"
    Run only unit tests.

.EXAMPLE
    .\scripts\test_pester.ps1 -Tag "Manifest"
    Run tests tagged with "Manifest".
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$Path,
    
    [Parameter(Mandatory = $false)]
    [string[]]$Tag
)

$ErrorActionPreference = "Stop"
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:MinPesterVersion = [Version]"5.5.0"

# Bootstrap Pester via ensure-pester.ps1
$ensurePesterScript = Join-Path $PSScriptRoot "ensure-pester.ps1"
if (-not (Test-Path $ensurePesterScript)) {
    Write-Host "[ERROR] ensure-pester.ps1 not found at: $ensurePesterScript" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host " Endstate - Pester Tests" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# ensure-pester.ps1 handles import and verification
# Dot-source to run in current scope so module import persists
. $ensurePesterScript -MinimumVersion $script:MinPesterVersion

$pester = Get-Module -Name Pester
if (-not $pester) {
    Write-Host "[ERROR] Pester module not loaded after ensure-pester.ps1" -ForegroundColor Red
    exit 1
}

Write-Host "Pester version: $($pester.Version)" -ForegroundColor DarkGray
Write-Host "PowerShell: $($PSVersionTable.PSVersion)" -ForegroundColor DarkGray
Write-Host ""

# Determine test path(s)
$testPaths = if ($Path) {
    if ([System.IO.Path]::IsPathRooted($Path)) {
        @($Path)
    } else {
        @(Join-Path $script:RepoRoot $Path)
    }
} else {
    # Run all tests in tests/ directory
    @(
        (Join-Path $script:RepoRoot "tests")
    ) | Where-Object { Test-Path $_ }
}

if ($testPaths.Count -eq 0) {
    Write-Host "[ERROR] No test paths found." -ForegroundColor Red
    exit 1
}

foreach ($tp in $testPaths) {
    Write-Host "Test path: $tp" -ForegroundColor DarkGray
}
Write-Host ""

# Configure Pester for CI mode
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

# Run tests with CI mode
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
Write-Host ""
