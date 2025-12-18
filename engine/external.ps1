<#
.SYNOPSIS
    External process wrapper for Provisioning.

.DESCRIPTION
    Provides mockable wrappers around external process calls (winget, etc.).
    All external calls should go through these functions to enable testing.
#>

function Invoke-WingetList {
    <#
    .SYNOPSIS
        Wrapper for winget list command.
    .DESCRIPTION
        Returns raw output from winget list. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$PackageId
    )
    
    $wingetArgs = @("list", "--accept-source-agreements")
    if ($PackageId) {
        $wingetArgs += @("--id", $PackageId)
    }
    
    try {
        $output = & winget @wingetArgs 2>&1
        return $output
    } catch {
        return $null
    }
}

function Invoke-WingetInstall {
    <#
    .SYNOPSIS
        Wrapper for winget install command.
    .DESCRIPTION
        Installs a package via winget. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageId,
        
        [Parameter(Mandatory = $false)]
        [switch]$Silent
    )
    
    $installArgs = @(
        "install"
        "--id", $PackageId
        "--accept-source-agreements"
        "--accept-package-agreements"
    )
    
    if ($Silent) {
        $installArgs += "--silent"
    }
    
    try {
        $output = & winget @installArgs 2>&1
        return @{
            Output = $output
            ExitCode = $LASTEXITCODE
        }
    } catch {
        return @{
            Output = $_.Exception.Message
            ExitCode = 1
        }
    }
}

function Invoke-WingetExportWrapper {
    <#
    .SYNOPSIS
        Wrapper for winget export command.
    .DESCRIPTION
        Exports installed packages to JSON. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ExportPath
    )
    
    try {
        & winget export -o $ExportPath --accept-source-agreements 2>&1 | Out-Null
        return $LASTEXITCODE -eq 0
    } catch {
        return $false
    }
}

function Test-CommandExists {
    <#
    .SYNOPSIS
        Check if a command/executable exists in PATH.
    .DESCRIPTION
        Returns true if the command is resolvable. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$CommandName
    )
    
    try {
        $null = Get-Command $CommandName -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

function Get-RegistryValue {
    <#
    .SYNOPSIS
        Get a registry value. Windows only.
    .DESCRIPTION
        Returns registry value or null if not found. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [string]$Name
    )
    
    try {
        if ($Name) {
            return Get-ItemPropertyValue -Path $Path -Name $Name -ErrorAction Stop
        } else {
            return Test-Path $Path
        }
    } catch {
        return $null
    }
}

function Get-CommandInfo {
    <#
    .SYNOPSIS
        Get detailed command info (path, type) for a command name.
    .DESCRIPTION
        Returns command info object or null if not found. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$CommandName
    )
    
    try {
        $cmd = Get-Command $CommandName -ErrorAction Stop | Select-Object -First 1
        return @{
            Name = $cmd.Name
            Path = if ($cmd.Source) { $cmd.Source } elseif ($cmd.Path) { $cmd.Path } else { $null }
            CommandType = $cmd.CommandType.ToString()
        }
    } catch {
        return $null
    }
}

function Get-CommandVersion {
    <#
    .SYNOPSIS
        Get version string for a command by running <command> --version.
    .DESCRIPTION
        Runs the command with --version flag and returns first line of output.
        Has a short timeout to avoid hanging. Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$CommandName,
        
        [Parameter(Mandatory = $false)]
        [int]$TimeoutSeconds = 5
    )
    
    try {
        # Use Start-Job for timeout capability
        $job = Start-Job -ScriptBlock {
            param($cmd)
            $output = & $cmd --version 2>&1 | Select-Object -First 1
            return "$output"
        } -ArgumentList $CommandName
        
        $completed = Wait-Job -Job $job -Timeout $TimeoutSeconds
        
        if ($completed) {
            $result = Receive-Job -Job $job
            Remove-Job -Job $job -Force
            
            # Extract version number from common formats
            # "git version 2.43.0.windows.1" -> "2.43.0.windows.1"
            # "Python 3.12.0" -> "3.12.0"
            # "v20.10.0" -> "20.10.0"
            if ($result -match '(\d+\.\d+[\.\d+]*)') {
                return $Matches[0]
            }
            return $result
        } else {
            Stop-Job -Job $job
            Remove-Job -Job $job -Force
            return $null
        }
    } catch {
        return $null
    }
}

function Get-RegistryUninstallEntries {
    <#
    .SYNOPSIS
        Get uninstall entries from a registry path.
    .DESCRIPTION
        Returns array of objects with DisplayName, DisplayVersion, Publisher, InstallLocation.
        Mockable for testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    try {
        $entries = @()
        $items = Get-ItemProperty -Path $Path -ErrorAction SilentlyContinue
        
        foreach ($item in $items) {
            if ($item.DisplayName) {
                $entries += @{
                    DisplayName = $item.DisplayName
                    DisplayVersion = $item.DisplayVersion
                    Publisher = $item.Publisher
                    InstallLocation = $item.InstallLocation
                }
            }
        }
        
        return $entries
    } catch {
        return @()
    }
}

# Functions exported: Invoke-WingetList, Invoke-WingetInstall, Invoke-WingetExportWrapper, Test-CommandExists, Get-RegistryValue, Get-CommandInfo, Get-CommandVersion, Get-RegistryUninstallEntries
