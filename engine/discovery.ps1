<#
.SYNOPSIS
    Discovery module for Provisioning capture.

.DESCRIPTION
    Detects software present on the system but not managed by winget.
    Provides PATH detection, registry uninstall detection, and winget ownership cross-check.
    All external calls go through external.ps1 wrappers for testability.
#>

# Import external wrapper (if not already loaded)
if (-not (Get-Command -Name Get-CommandInfo -ErrorAction SilentlyContinue)) {
    . "$PSScriptRoot\external.ps1"
}

# Known tools to discover via PATH
$script:DiscoverableTools = @(
    @{ Name = "git"; SuggestedWingetId = "Git.Git" }
    @{ Name = "python"; SuggestedWingetId = "Python.Python.3.12" }
    @{ Name = "node"; SuggestedWingetId = "OpenJS.NodeJS.LTS" }
    @{ Name = "docker"; SuggestedWingetId = "Docker.DockerDesktop" }
    @{ Name = "pwsh"; SuggestedWingetId = "Microsoft.PowerShell" }
)

# Registry uninstall key paths
$script:UninstallRegistryPaths = @(
    "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*"
    "HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*"
    "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*"
)

# Known mappings from registry DisplayName patterns to winget IDs
$script:RegistryToWingetMap = @{
    "^Git\b" = "Git.Git"
    "^Git version" = "Git.Git"
    "^Python 3" = "Python.Python.3"
    "^Node\.js" = "OpenJS.NodeJS"
    "^Docker Desktop" = "Docker.DockerDesktop"
    "^PowerShell \d" = "Microsoft.PowerShell"
}

function Invoke-Discovery {
    <#
    .SYNOPSIS
        Run discovery detectors and return discovered software.
    .PARAMETER WingetInstalledIds
        Array of winget package IDs already installed (from winget list).
        Used for ownership cross-check.
    .OUTPUTS
        Array of discovery entries, sorted deterministically.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string[]]$WingetInstalledIds = @()
    )
    
    $discovered = @()
    
    # PATH detector
    $pathFindings = Invoke-PathDetector
    $discovered += $pathFindings
    
    # Registry uninstall detector (Git-focused)
    $registryFindings = Invoke-RegistryUninstallDetector
    $discovered += $registryFindings
    
    # Cross-check winget ownership and deduplicate
    $discovered = Add-WingetOwnership -Discoveries $discovered -WingetInstalledIds $WingetInstalledIds
    
    # Filter to only non-winget-owned entries (the interesting ones)
    # Keep all entries but mark ownership - caller decides what to surface
    
    # Sort deterministically by (name, method, path)
    $sorted = $discovered | Sort-Object -Property @(
        @{ Expression = { $_.name }; Ascending = $true }
        @{ Expression = { $_.method }; Ascending = $true }
        @{ Expression = { $_.path }; Ascending = $true }
    )
    
    return @($sorted)
}

function Invoke-PathDetector {
    <#
    .SYNOPSIS
        Detect tools available on PATH.
    .OUTPUTS
        Array of discovery entries with method = "path".
    #>
    $findings = @()
    
    foreach ($tool in $script:DiscoverableTools) {
        $toolName = $tool.Name
        $suggestedId = $tool.SuggestedWingetId
        
        # Use external wrapper for testability
        $cmdInfo = Get-CommandInfo -CommandName $toolName
        
        if ($cmdInfo) {
            $version = Get-CommandVersion -CommandName $toolName
            
            $entry = @{
                name = $toolName
                path = $cmdInfo.Path
                version = if ($version) { $version.Trim() } else { $null }
                method = "path"
                suggestedWingetId = $suggestedId
            }
            
            $findings += $entry
        }
    }
    
    return $findings
}

function Invoke-RegistryUninstallDetector {
    <#
    .SYNOPSIS
        Detect installed software via registry uninstall keys.
        Focused on Git and other known tools.
    .OUTPUTS
        Array of discovery entries with method = "registry".
    #>
    $findings = @()
    $seenDisplayNames = @{}
    
    foreach ($regPath in $script:UninstallRegistryPaths) {
        $entries = Get-RegistryUninstallEntries -Path $regPath
        
        foreach ($entry in $entries) {
            $displayName = $entry.DisplayName
            if (-not $displayName) { continue }
            
            # Check against known patterns
            foreach ($pattern in $script:RegistryToWingetMap.Keys) {
                if ($displayName -match $pattern) {
                    # Avoid duplicates (same DisplayName from different registry locations)
                    $key = "$displayName|$($entry.DisplayVersion)"
                    if ($seenDisplayNames.ContainsKey($key)) { continue }
                    $seenDisplayNames[$key] = $true
                    
                    $suggestedId = $script:RegistryToWingetMap[$pattern]
                    
                    # Derive a simple name from the pattern
                    $simpleName = switch -Regex ($pattern) {
                        "Git" { "git" }
                        "Python" { "python" }
                        "Node" { "node" }
                        "Docker" { "docker" }
                        "PowerShell" { "pwsh" }
                        default { $displayName.ToLower() -replace '\s+', '-' -replace '[^a-z0-9-]', '' }
                    }
                    
                    $finding = @{
                        name = $simpleName
                        displayName = $displayName
                        displayVersion = if ($entry.DisplayVersion) { $entry.DisplayVersion.Trim() } else { $null }
                        publisher = $entry.Publisher
                        installLocation = $entry.InstallLocation
                        method = "registry"
                        suggestedWingetId = $suggestedId
                    }
                    
                    $findings += $finding
                    break  # Only match first pattern per entry
                }
            }
        }
    }
    
    return $findings
}

function Add-WingetOwnership {
    <#
    .SYNOPSIS
        Cross-check discoveries against winget installed packages.
    .PARAMETER Discoveries
        Array of discovery entries.
    .PARAMETER WingetInstalledIds
        Array of winget package IDs currently installed.
    .OUTPUTS
        Discoveries with ownedByWinget property added.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [array]$Discoveries,
        
        [Parameter(Mandatory = $false)]
        [string[]]$WingetInstalledIds = @()
    )
    
    # Normalize winget IDs to lowercase for comparison
    $normalizedWingetIds = @($WingetInstalledIds | ForEach-Object { $_.ToLower() })
    
    foreach ($discovery in $Discoveries) {
        $suggestedId = $discovery.suggestedWingetId
        $owned = $false
        
        if ($suggestedId) {
            $normalizedSuggested = $suggestedId.ToLower()
            
            # Check exact match or prefix match (e.g., Git.Git matches Git.Git)
            foreach ($installedId in $normalizedWingetIds) {
                if ($installedId -eq $normalizedSuggested -or $installedId.StartsWith("$normalizedSuggested.")) {
                    $owned = $true
                    break
                }
                # Also check if installed ID starts with the base (e.g., Python.Python.3.12 matches Python.Python.3)
                if ($normalizedSuggested.StartsWith($installedId) -or $installedId.StartsWith($normalizedSuggested.Split('.')[0..1] -join '.')) {
                    $owned = $true
                    break
                }
            }
        }
        
        $discovery.ownedByWinget = $owned
    }
    
    return $Discoveries
}

function Get-WingetInstalledPackageIds {
    <#
    .SYNOPSIS
        Get list of winget package IDs currently installed.
    .DESCRIPTION
        Parses winget list output to extract package identifiers.
        Uses external wrapper for testability.
    .OUTPUTS
        Array of package ID strings.
    #>
    $output = Invoke-WingetList
    
    if (-not $output) {
        return @()
    }
    
    $ids = @()
    $inTable = $false
    $idColumnStart = -1
    $idColumnEnd = -1
    
    foreach ($line in $output) {
        $lineStr = "$line"
        
        # Detect header line (contains "Id" column)
        if ($lineStr -match '^\s*Name\s+Id\s+' -or $lineStr -match '^Name\s+Id\s+') {
            $inTable = $true
            # Find Id column position
            $idMatch = [regex]::Match($lineStr, '\bId\b')
            if ($idMatch.Success) {
                $idColumnStart = $idMatch.Index
                # Find next column (Version) to determine end
                $versionMatch = [regex]::Match($lineStr, '\bVersion\b')
                if ($versionMatch.Success) {
                    $idColumnEnd = $versionMatch.Index
                }
            }
            continue
        }
        
        # Skip separator line
        if ($lineStr -match '^-+$' -or $lineStr -match '^\s*-+\s*-+') {
            continue
        }
        
        # Parse data rows
        if ($inTable -and $idColumnStart -ge 0 -and $lineStr.Length -gt $idColumnStart) {
            $endPos = if ($idColumnEnd -gt $idColumnStart) { $idColumnEnd } else { $lineStr.Length }
            $idPart = $lineStr.Substring($idColumnStart, [Math]::Min($endPos - $idColumnStart, $lineStr.Length - $idColumnStart))
            $id = $idPart.Trim()
            
            if ($id -and $id -ne "Id" -and $id -notmatch '^-+$') {
                $ids += $id
            }
        }
    }
    
    return $ids
}

function Write-ManualIncludeTemplate {
    <#
    .SYNOPSIS
        Generate a manual include file with commented app suggestions.
    .PARAMETER Path
        Output file path.
    .PARAMETER ProfileName
        Profile name for comments.
    .PARAMETER Discoveries
        Array of discovery entries (non-winget-owned).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfileName,
        
        [Parameter(Mandatory = $true)]
        [array]$Discoveries
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    [void]$sb.AppendLine("{")
    [void]$sb.AppendLine("  // Manual Include Suggestions for profile: $ProfileName")
    [void]$sb.AppendLine("  // Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    [void]$sb.AppendLine("  //")
    [void]$sb.AppendLine("  // These apps were discovered on your system but are NOT managed by winget.")
    [void]$sb.AppendLine("  // Uncomment entries you want to install via winget on a fresh machine.")
    [void]$sb.AppendLine("  // Review each suggestion - some may not be exact matches.")
    [void]$sb.AppendLine("")
    [void]$sb.AppendLine("  `"apps`": [")
    
    # Filter to non-winget-owned with suggested IDs
    $suggestions = @($Discoveries | Where-Object { 
        -not $_.ownedByWinget -and $_.suggestedWingetId 
    } | Sort-Object -Property name -Unique)
    
    if ($suggestions.Count -gt 0) {
        foreach ($discovery in $suggestions) {
            $id = $discovery.suggestedWingetId -replace '\.', '-'
            $id = $id.ToLower()
            $wingetId = $discovery.suggestedWingetId
            
            # Build comment with detection info
            $comment = "Detected: $($discovery.name)"
            if ($discovery.version) {
                $comment += " $($discovery.version)"
            } elseif ($discovery.displayVersion) {
                $comment += " $($discovery.displayVersion)"
            }
            if ($discovery.path) {
                $comment += " at $($discovery.path)"
            } elseif ($discovery.installLocation) {
                $comment += " at $($discovery.installLocation)"
            }
            
            [void]$sb.AppendLine("    // { `"id`": `"$id`", `"refs`": { `"windows`": `"$wingetId`" } }  // $comment")
        }
    } else {
        [void]$sb.AppendLine("    // No non-winget-managed software discovered")
    }
    
    [void]$sb.AppendLine("  ]")
    [void]$sb.AppendLine("}")
    
    $parentDir = Split-Path -Parent $Path
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    $sb.ToString() | Out-File -FilePath $Path -Encoding UTF8 -NoNewline
    
    return $Path
}

# Functions exported: Invoke-Discovery, Invoke-PathDetector, Invoke-RegistryUninstallDetector, Add-WingetOwnership, Get-WingetInstalledPackageIds, Write-ManualIncludeTemplate
