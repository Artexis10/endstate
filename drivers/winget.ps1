# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Winget driver for Provisioning.

.DESCRIPTION
    Provides functions to detect, install, and verify applications
    using Windows Package Manager (winget).
#>

function Test-WingetAvailable {
    try {
        $null = Get-Command winget -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

function Test-AppInstalled {
    <#
    .SYNOPSIS
        Check if an app is installed via winget.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId
    )
    
    try {
        $output = & winget list --id $PackageId --accept-source-agreements 2>&1
        $outputStr = $output | Out-String
        
        # If the package is found, winget list will show it
        # If not found, it will say "No installed package found"
        if ($outputStr -match "No installed package found") {
            return $false
        }
        
        # Check if the package ID appears in the output
        if ($outputStr -match [regex]::Escape($PackageId)) {
            return $true
        }
        
        return $false
    } catch {
        return $false
    }
}

function Install-AppViaWinget {
    <#
    .SYNOPSIS
        Install an app via winget.
    .DESCRIPTION
        Idempotent installation - skips if already installed.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId,
        
        [Parameter(Mandatory = $false)]
        [switch]$Silent
    )
    
    $result = @{
        Success = $false
        AlreadyInstalled = $false
        UserDenied = $false
        Error = $null
    }
    
    # Check if already installed
    if (Test-AppInstalled -PackageId $PackageId) {
        $result.Success = $true
        $result.AlreadyInstalled = $true
        return $result
    }
    
    try {
        # Install the package
        $installArgs = @(
            "install"
            "--id", $PackageId
            "--accept-source-agreements"
            "--accept-package-agreements"
        )
        
        if ($Silent) {
            $installArgs += "--silent"
        }
        
        $output = & winget @installArgs 2>&1
        $outputStr = $output | Out-String
        $exitCode = $LASTEXITCODE
        
        # Check for success indicators
        if ($outputStr -match "Successfully installed" -or $outputStr -match "Found an existing package") {
            $result.Success = $true
        }
        elseif ($outputStr -match "No package found matching") {
            $result.Error = "Package not found: $PackageId"
        }
        # Detect user denial/cancellation
        # Exit code 0xC0000409 (-1073740791) or output containing cancellation indicators
        elseif ($exitCode -eq -1073740791 -or $exitCode -eq 0xC0000409 -or 
                $outputStr -match "(?i)(cancel|user.*cancel|operation.*cancel|user.*denied|user.*abort)" -or
                $outputStr -match "(?i)(user.*decline|installation.*cancel)") {
            $result.UserDenied = $true
            $result.Error = "User cancelled installation"
        }
        elseif ($outputStr -match "error" -or $exitCode -ne 0) {
            $result.Error = "Installation failed: $outputStr"
        }
        else {
            # Verify installation
            if (Test-AppInstalled -PackageId $PackageId) {
                $result.Success = $true
            } else {
                $result.Error = "Installation completed but package not detected"
            }
        }
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

function Get-AppVersion {
    <#
    .SYNOPSIS
        Get the installed version of an app.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId
    )
    
    try {
        $output = & winget list --id $PackageId --accept-source-agreements 2>&1
        $outputStr = $output | Out-String
        
        # Parse version from output
        # Format: Name  Id  Version  Available  Source
        $lines = $outputStr -split "`n"
        foreach ($line in $lines) {
            if ($line -match [regex]::Escape($PackageId)) {
                $parts = $line -split '\s{2,}'
                if ($parts.Count -ge 3) {
                    return $parts[2].Trim()
                }
            }
        }
        
        return $null
    } catch {
        return $null
    }
}

function Test-AppVerified {
    <#
    .SYNOPSIS
        Verify an app is installed and optionally responds to a command.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId,
        
        [Parameter(Mandatory = $false)]
        [string]$VerifyCommand
    )
    
    # First check winget reports it as installed
    if (-not (Test-AppInstalled -PackageId $PackageId)) {
        return @{
            Success = $false
            Message = "Package not found in winget list"
        }
    }
    
    # If a verify command is provided, run it
    if ($VerifyCommand) {
        try {
            $output = Invoke-Expression $VerifyCommand 2>&1
            return @{
                Success = $true
                Message = "Command succeeded: $($output | Select-Object -First 1)"
            }
        } catch {
            return @{
                Success = $false
                Message = "Command failed: $($_.Exception.Message)"
            }
        }
    }
    
    return @{
        Success = $true
        Message = "Package is installed"
    }
}

# Functions exported: Test-WingetAvailable, Test-AppInstalled, Install-AppViaWinget, Get-AppVersion, Test-AppVerified
