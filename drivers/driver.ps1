# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Driver interface and registry for Endstate package management.

.DESCRIPTION
    Provides a pluggable driver abstraction layer that decouples the engine
    from specific package managers. Currently implements winget for Windows,
    with architecture ready for future brew/apt/etc. support.
    
    The engine should NEVER directly import driver implementations.
    Instead, use Get-ActiveDriver to obtain the current driver, then call
    driver functions through the returned interface.
    
    Driver Interface Contract:
    - Name: string (e.g., "winget", "brew", "apt")
    - Test-Available: () -> bool
    - Test-PackageInstalled: (packageId) -> bool
    - Install-Package: (packageId, options) -> { Success, AlreadyInstalled, UserDenied, Error }
    - Get-PackageVersion: (packageId) -> string | null
    - Get-InstalledPackages: () -> string[]
#>

# Module state
$script:RegisteredDrivers = @{}
$script:ActiveDriverName = $null
$script:DriversLoaded = $false

function Register-Driver {
    <#
    .SYNOPSIS
        Register a package manager driver with the registry.
    .PARAMETER Name
        Unique driver name (e.g., "winget", "brew").
    .PARAMETER Driver
        Hashtable containing driver interface functions.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$Driver
    )
    
    # Validate required interface functions
    $requiredFunctions = @(
        'TestAvailable',
        'TestPackageInstalled',
        'InstallPackage',
        'GetInstalledPackages'
    )
    
    foreach ($fn in $requiredFunctions) {
        if (-not $Driver.ContainsKey($fn)) {
            throw "Driver '$Name' missing required function: $fn"
        }
    }
    
    # Ensure Name is set on the driver
    $Driver['Name'] = $Name
    
    $script:RegisteredDrivers[$Name] = $Driver
}

function Get-RegisteredDrivers {
    <#
    .SYNOPSIS
        Returns list of registered driver names.
    #>
    return @($script:RegisteredDrivers.Keys)
}

function Get-Driver {
    <#
    .SYNOPSIS
        Get a specific driver by name.
    .PARAMETER Name
        Driver name to retrieve.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )
    
    Initialize-Drivers
    
    if (-not $script:RegisteredDrivers.ContainsKey($Name)) {
        return $null
    }
    
    return $script:RegisteredDrivers[$Name]
}

function Get-ActiveDriver {
    <#
    .SYNOPSIS
        Get the currently active package manager driver.
    .DESCRIPTION
        Returns the active driver for the current platform.
        On Windows, defaults to "winget".
        On other platforms, returns null (no driver available yet).
    #>
    
    Initialize-Drivers
    
    if ($script:ActiveDriverName -and $script:RegisteredDrivers.ContainsKey($script:ActiveDriverName)) {
        return $script:RegisteredDrivers[$script:ActiveDriverName]
    }
    
    return $null
}

function Get-ActiveDriverName {
    <#
    .SYNOPSIS
        Get the name of the currently active driver.
    #>
    
    Initialize-Drivers
    
    return $script:ActiveDriverName
}

function Set-ActiveDriver {
    <#
    .SYNOPSIS
        Set the active driver by name.
    .PARAMETER Name
        Driver name to activate.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )
    
    Initialize-Drivers
    
    if (-not $script:RegisteredDrivers.ContainsKey($Name)) {
        throw "Driver not registered: $Name"
    }
    
    $script:ActiveDriverName = $Name
}

function Initialize-Drivers {
    <#
    .SYNOPSIS
        Initialize the driver registry by loading available drivers.
    .DESCRIPTION
        Loads driver implementations and sets the default active driver
        based on the current platform.
    #>
    
    if ($script:DriversLoaded) {
        return
    }
    
    # Load winget driver on Windows
    $onWindows = Test-IsWindowsPlatform
    
    if ($onWindows) {
        $wingetDriverPath = Join-Path $PSScriptRoot "winget.ps1"
        if (Test-Path $wingetDriverPath) {
            . $wingetDriverPath
            
            # Create driver interface wrapping winget functions
            $wingetDriver = @{
                Name = "winget"
                TestAvailable = { Test-WingetAvailable }
                TestPackageInstalled = { param($PackageId) Test-AppInstalled -PackageId $PackageId }
                InstallPackage = { param($PackageId, $Options) 
                    $silent = if ($Options -and $Options.Silent) { $true } else { $false }
                    Install-AppViaWinget -PackageId $PackageId -Silent:$silent
                }
                GetPackageVersion = { param($PackageId) Get-AppVersion -PackageId $PackageId }
                GetInstalledPackages = { Get-InstalledPackagesViaDriver }
            }
            
            Register-Driver -Name "winget" -Driver $wingetDriver
            $script:ActiveDriverName = "winget"
        }
    }
    
    # Future: Load brew driver on macOS
    # Future: Load apt/dnf/pacman drivers on Linux
    
    $script:DriversLoaded = $true
}

function Test-IsWindowsPlatform {
    <#
    .SYNOPSIS
        Check if running on Windows platform.
    .DESCRIPTION
        Uses $IsWindows automatic variable (PS6+) or falls back to
        environment variable check for PS5.1 compatibility.
    #>
    
    # PowerShell 6+ has $IsWindows automatic variable
    if (Get-Variable -Name 'IsWindows' -Scope Global -ErrorAction SilentlyContinue) {
        return (Get-Variable -Name 'IsWindows' -Scope Global -ValueOnly)
    }
    
    # PowerShell 5.1 on Windows doesn't have $IsWindows, but is always Windows
    if ($env:OS -eq "Windows_NT") {
        return $true
    }
    
    return $false
}

function Get-InstalledPackagesViaDriver {
    <#
    .SYNOPSIS
        Get list of installed package IDs via winget.
    .DESCRIPTION
        Wrapper that calls the winget-specific function from plan.ps1.
        This exists to bridge the old function name to the new driver interface.
    #>
    
    # This function is defined in plan.ps1 - we call it here for backward compatibility
    # The actual implementation parses winget list output
    try {
        $output = & winget list --accept-source-agreements 2>&1
        
        $installedIds = @()
        $inTable = $false
        
        foreach ($line in $output) {
            $lineStr = $line.ToString()
            
            # Skip header lines
            if ($lineStr -match '^-+$') {
                $inTable = $true
                continue
            }
            
            if (-not $inTable) { continue }
            if ([string]::IsNullOrWhiteSpace($lineStr)) { continue }
            
            # Parse the line - winget list output is space-separated columns
            # Format: Name  Id  Version  Available  Source
            $parts = $lineStr -split '\s{2,}'
            if ($parts.Count -ge 2) {
                $id = $parts[1].Trim()
                if ($id -and $id -ne "Id" -and $id -notmatch '^-+$') {
                    $installedIds += $id
                }
            }
        }
        
        return $installedIds
        
    } catch {
        return @()
    }
}

# Driver interface helper functions for engine use

function Invoke-DriverTestAvailable {
    <#
    .SYNOPSIS
        Test if the active driver is available.
    #>
    $driver = Get-ActiveDriver
    if (-not $driver) {
        return $false
    }
    return & $driver.TestAvailable
}

function Invoke-DriverTestPackageInstalled {
    <#
    .SYNOPSIS
        Test if a package is installed via the active driver.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId
    )
    
    $driver = Get-ActiveDriver
    if (-not $driver) {
        return $false
    }
    return & $driver.TestPackageInstalled $PackageId
}

function Invoke-DriverInstallPackage {
    <#
    .SYNOPSIS
        Install a package via the active driver.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId,
        
        [Parameter(Mandatory = $false)]
        [hashtable]$Options = @{}
    )
    
    $driver = Get-ActiveDriver
    if (-not $driver) {
        return @{
            Success = $false
            AlreadyInstalled = $false
            UserDenied = $false
            Error = "No package driver available"
        }
    }
    return & $driver.InstallPackage $PackageId $Options
}

function Invoke-DriverGetInstalledPackages {
    <#
    .SYNOPSIS
        Get list of installed packages via the active driver.
    #>
    $driver = Get-ActiveDriver
    if (-not $driver) {
        return @()
    }
    return & $driver.GetInstalledPackages
}

function Invoke-DriverGetPackageVersion {
    <#
    .SYNOPSIS
        Get version of an installed package via the active driver.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId
    )
    
    $driver = Get-ActiveDriver
    if (-not $driver) {
        return $null
    }
    
    if ($driver.GetPackageVersion) {
        return & $driver.GetPackageVersion $PackageId
    }
    return $null
}

# Functions exported: Register-Driver, Get-RegisteredDrivers, Get-Driver, Get-ActiveDriver, 
#                     Get-ActiveDriverName, Set-ActiveDriver, Initialize-Drivers, Test-IsWindowsPlatform,
#                     Invoke-DriverTestAvailable, Invoke-DriverTestPackageInstalled, 
#                     Invoke-DriverInstallPackage, Invoke-DriverGetInstalledPackages, Invoke-DriverGetPackageVersion
