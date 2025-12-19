<#
.SYNOPSIS
    Ensures Pester >= 5.5.0 is available for PowerShell.

.DESCRIPTION
    Bootstraps Pester 5.5.0+ for the automation-suite test runner.
    Prefers repo-local vendoring in tools/pester/ for deterministic builds.
    Falls back to Install-Module -Scope CurrentUser if local vendor is absent.

.PARAMETER MinimumVersion
    Minimum Pester version required. Default: 5.5.0

.EXAMPLE
    .\scripts\ensure-pester.ps1
    Ensures Pester 5.5.0+ is available.

.OUTPUTS
    Returns the path to the Pester module that should be imported.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [Version]$MinimumVersion = "5.5.0"
)

$ErrorActionPreference = "Stop"
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:VendorPath = Join-Path $script:RepoRoot "tools\pester"

function Get-VendoredPester {
    if (Test-Path $script:VendorPath) {
        $pesterModule = Get-ChildItem -Path $script:VendorPath -Filter "Pester.psd1" -Recurse | Select-Object -First 1
        if ($pesterModule) {
            $manifest = Import-PowerShellDataFile -Path $pesterModule.FullName
            if ([Version]$manifest.ModuleVersion -ge $MinimumVersion) {
                # Return the .psd1 file path for Import-Module
                return $pesterModule.FullName
            }
        }
    }
    return $null
}

function Install-VendoredPester {
    Write-Host "[ensure-pester] Installing Pester $MinimumVersion to $script:VendorPath..." -ForegroundColor Cyan
    
    if (-not (Test-Path $script:VendorPath)) {
        New-Item -ItemType Directory -Path $script:VendorPath -Force | Out-Null
    }
    
    try {
        Save-Module -Name Pester -Path $script:VendorPath -MinimumVersion $MinimumVersion -Force -Repository PSGallery
        Write-Host "[ensure-pester] Pester installed to vendor path." -ForegroundColor Green
        return Get-VendoredPester
    } catch {
        Write-Host "[ensure-pester] Failed to save module to vendor path: $_" -ForegroundColor Yellow
        return $null
    }
}

function Get-InstalledPester {
    $installed = Get-Module -ListAvailable -Name Pester | 
        Where-Object { $_.Version -ge $MinimumVersion } | 
        Sort-Object Version -Descending | 
        Select-Object -First 1
    
    if ($installed) {
        return $installed.ModuleBase
    }
    return $null
}

function Install-UserPester {
    Write-Host "[ensure-pester] Installing Pester $MinimumVersion to CurrentUser scope..." -ForegroundColor Cyan
    
    try {
        Install-Module -Name Pester -MinimumVersion $MinimumVersion -Scope CurrentUser -Force -SkipPublisherCheck -AllowClobber
        Write-Host "[ensure-pester] Pester installed to CurrentUser scope." -ForegroundColor Green
        return Get-InstalledPester
    } catch {
        Write-Host "[ensure-pester] Failed to install module: $_" -ForegroundColor Red
        return $null
    }
}

# Main logic
Write-Host "[ensure-pester] Checking for Pester >= $MinimumVersion..." -ForegroundColor Cyan

# 1. Check vendored path first
$vendoredPath = Get-VendoredPester
if ($vendoredPath) {
    Write-Host "[ensure-pester] Found vendored Pester at: $vendoredPath" -ForegroundColor Green
    return $vendoredPath
}

# 2. Try to install to vendor path
$vendoredPath = Install-VendoredPester
if ($vendoredPath) {
    return $vendoredPath
}

# 3. Check if already installed in user/system scope
$installedPath = Get-InstalledPester
if ($installedPath) {
    Write-Host "[ensure-pester] Found installed Pester at: $installedPath" -ForegroundColor Green
    return $installedPath
}

# 4. Fallback: install to CurrentUser scope
$installedPath = Install-UserPester
if ($installedPath) {
    return $installedPath
}

Write-Host "[ensure-pester] ERROR: Could not ensure Pester >= $MinimumVersion is available." -ForegroundColor Red
exit 1
