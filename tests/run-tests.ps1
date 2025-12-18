<#
.SYNOPSIS
    Run Pester tests for Provisioning.

.DESCRIPTION
    Executes all Pester tests in the provisioning/tests directory.
    Requires Pester module (Install-Module Pester -Force -SkipPublisherCheck).

.EXAMPLE
    .\run-tests.ps1
    Run all tests.

.EXAMPLE
    .\run-tests.ps1 -Verbose
    Run all tests with verbose output.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

# Ensure Pester is available
$pester = Get-Module -ListAvailable -Name Pester | Sort-Object Version -Descending | Select-Object -First 1

if (-not $pester) {
    Write-Host "[ERROR] Pester module not found." -ForegroundColor Red
    Write-Host ""
    Write-Host "Install Pester with:" -ForegroundColor Yellow
    Write-Host "  Install-Module Pester -Force -SkipPublisherCheck" -ForegroundColor DarkGray
    Write-Host ""
    exit 1
}

Write-Host ""
Write-Host "Provisioning Tests" -ForegroundColor Cyan
Write-Host "==================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Pester version: $($pester.Version)" -ForegroundColor DarkGray
Write-Host ""

# Import Pester
Import-Module Pester -Force

# Run tests based on Pester version
if ($pester.Version -ge [Version]"5.0.0") {
    # Pester 5.x configuration
    $config = New-PesterConfiguration
    $config.Run.Path = $PSScriptRoot
    $config.Run.Exit = $true
    $config.Output.Verbosity = "Detailed"
    $config.TestResult.Enabled = $true
    $config.TestResult.OutputPath = Join-Path $PSScriptRoot "test-results.xml"
    
    $result = Invoke-Pester -Configuration $config
    $failedCount = $result.FailedCount
} else {
    # Pester 3.x/4.x legacy mode
    Write-Host "[INFO] Using Pester legacy mode (v$($pester.Version))" -ForegroundColor Yellow
    Write-Host ""
    
    $result = Invoke-Pester -Path $PSScriptRoot -PassThru
    $failedCount = $result.FailedCount
}

# Summary
Write-Host ""
if ($failedCount -eq 0) {
    Write-Host "All tests passed!" -ForegroundColor Green
} else {
    Write-Host "$failedCount test(s) failed." -ForegroundColor Red
}

exit $failedCount
