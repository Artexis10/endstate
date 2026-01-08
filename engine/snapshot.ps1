# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Filesystem snapshot and diff helpers for sandbox discovery.

.DESCRIPTION
    Provides functions to capture filesystem state before/after software installation
    and compute differences for module generation.
    
    Functions:
    - Get-FilesystemSnapshot: Capture file metadata from specified roots
    - Compare-FilesystemSnapshots: Compute added/modified files between snapshots
    - Apply-ExcludeHeuristics: Filter out junk paths (logs, cache, temp, etc.)
    - ConvertTo-LogicalToken: Convert absolute paths to logical tokens
#>

# Import paths module for token conversion
$script:PathsModule = Join-Path $PSScriptRoot "paths.ps1"
if (Test-Path $script:PathsModule) {
    . $script:PathsModule
}

# Exclude patterns for common junk directories/files
$script:ExcludePatterns = @(
    # Logs and temp
    '\\Logs\\',
    '\\Log\\',
    '\\Temp\\',
    '\\Tmp\\',
    '\\temp\\',
    '\\tmp\\',
    '\.log$',
    '\.tmp$',
    
    # Cache directories
    '\\Cache\\',
    '\\Caches\\',
    '\\cache\\',
    '\\GPUCache\\',
    '\\ShaderCache\\',
    '\\Code Cache\\',
    '\\DawnCache\\',
    
    # Crash dumps and diagnostics
    '\\Crashpad\\',
    '\\CrashDumps\\',
    '\\Crash Reports\\',
    '\\crash\\',
    '\.dmp$',
    
    # Telemetry and analytics
    '\\Telemetry\\',
    '\\telemetry\\',
    '\\Analytics\\',
    
    # Session and lock files
    '\.lock$',
    '\\lockfile$',
    '-journal$',
    '-wal$',
    '-shm$',
    
    # Browser/Electron junk
    '\\IndexedDB\\',
    '\\Local Storage\\',
    '\\Session Storage\\',
    '\\Service Worker\\',
    '\\blob_storage\\',
    '\\Network\\',
    '\\WebStorage\\',
    
    # Windows-specific junk
    '\\Windows\\Prefetch\\',
    '\\Windows\\Temp\\',
    '\\Windows\\Logs\\',
    '\\Windows\\SoftwareDistribution\\',
    '\\Windows\\WinSxS\\',
    '\\Windows\\Installer\\',
    '\\Windows\\assembly\\',
    
    # Package manager caches
    '\\npm-cache\\',
    '\\pip\\cache\\',
    '\\nuget\\packages\\',
    '\\__pycache__\\',
    
    # Thumbnails and previews
    '\\Thumbs\.db$',
    '\\desktop\.ini$',
    
    # Backup files
    '\.bak$',
    '\.backup$',
    '~$'
)

function Get-FilesystemSnapshot {
    <#
    .SYNOPSIS
        Capture filesystem metadata from specified root directories.
    .PARAMETER Roots
        Array of root directories to scan.
    .PARAMETER MaxDepth
        Maximum recursion depth (default: 10).
    .OUTPUTS
        Array of objects with: path, size, lastWriteUtc, isDirectory
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Roots,
        
        [Parameter(Mandatory = $false)]
        [int]$MaxDepth = 10
    )
    
    $snapshot = @()
    
    foreach ($root in $Roots) {
        if (-not (Test-Path $root)) {
            continue
        }
        
        $items = Get-ChildItem -Path $root -Recurse -Force -ErrorAction SilentlyContinue -Depth $MaxDepth
        
        foreach ($item in $items) {
            $entry = [PSCustomObject]@{
                path         = $item.FullName
                size         = if ($item.PSIsContainer) { 0 } else { $item.Length }
                lastWriteUtc = $item.LastWriteTimeUtc.ToString("o")
                isDirectory  = $item.PSIsContainer
            }
            $snapshot += $entry
        }
    }
    
    # Sort deterministically by path
    $snapshot = $snapshot | Sort-Object -Property path
    
    return @($snapshot)
}

function Compare-FilesystemSnapshots {
    <#
    .SYNOPSIS
        Compare two snapshots and return added/modified files.
    .PARAMETER PreSnapshot
        Snapshot taken before installation.
    .PARAMETER PostSnapshot
        Snapshot taken after installation.
    .OUTPUTS
        Object with: added (array), modified (array)
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$PreSnapshot,
        
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$PostSnapshot
    )
    
    # Handle null/empty inputs
    if ($null -eq $PreSnapshot) { $PreSnapshot = @() }
    if ($null -eq $PostSnapshot) { $PostSnapshot = @() }
    
    # Build lookup from pre-snapshot
    $preLookup = @{}
    foreach ($entry in $PreSnapshot) {
        if ($null -ne $entry -and $null -ne $entry.path) {
            $preLookup[$entry.path] = $entry
        }
    }
    
    $added = @()
    $modified = @()
    
    foreach ($postEntry in $PostSnapshot) {
        $path = $postEntry.path
        
        if (-not $preLookup.ContainsKey($path)) {
            # New file/directory
            $added += $postEntry
        } else {
            # Check if modified (size or lastWriteUtc changed)
            $preEntry = $preLookup[$path]
            if ($postEntry.size -ne $preEntry.size -or $postEntry.lastWriteUtc -ne $preEntry.lastWriteUtc) {
                $modified += $postEntry
            }
        }
    }
    
    # Sort deterministically (handle empty arrays)
    if ($added.Count -gt 0) {
        $added = @($added | Sort-Object -Property path)
    } else {
        $added = @()
    }
    
    if ($modified.Count -gt 0) {
        $modified = @($modified | Sort-Object -Property path)
    } else {
        $modified = @()
    }
    
    return [PSCustomObject]@{
        added    = $added
        modified = $modified
    }
}

function Test-PathMatchesExcludePattern {
    <#
    .SYNOPSIS
        Check if a path matches any exclude pattern.
    .PARAMETER Path
        The path to test.
    .PARAMETER Patterns
        Array of regex patterns to match against.
    .OUTPUTS
        $true if path should be excluded, $false otherwise.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [string[]]$Patterns = $script:ExcludePatterns
    )
    
    foreach ($pattern in $Patterns) {
        if ($Path -match $pattern) {
            return $true
        }
    }
    
    return $false
}

function Apply-ExcludeHeuristics {
    <#
    .SYNOPSIS
        Filter out junk paths from a list of filesystem entries.
    .PARAMETER Entries
        Array of snapshot entries (objects with 'path' property).
    .PARAMETER AdditionalPatterns
        Optional additional regex patterns to exclude.
    .OUTPUTS
        Filtered array of entries.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$Entries,
        
        [Parameter(Mandatory = $false)]
        [string[]]$AdditionalPatterns = @()
    )
    
    # Handle null/empty inputs
    if ($null -eq $Entries -or $Entries.Count -eq 0) {
        return @()
    }
    
    $patterns = $script:ExcludePatterns + $AdditionalPatterns
    
    $filtered = @()
    
    foreach ($entry in $Entries) {
        if ($null -ne $entry -and $null -ne $entry.path) {
            if (-not (Test-PathMatchesExcludePattern -Path $entry.path -Patterns $patterns)) {
                $filtered += $entry
            }
        }
    }
    
    return @($filtered)
}

function ConvertTo-LogicalToken {
    <#
    .SYNOPSIS
        Convert an absolute path to use logical tokens.
    .DESCRIPTION
        Replaces known Windows paths with logical tokens for portability:
        - %LOCALAPPDATA% -> ${localappdata}
        - %APPDATA% -> ${appdata}
        - %USERPROFILE% -> ${home}
        - %PROGRAMFILES% -> ${programfiles}
        - %PROGRAMDATA% -> ${programdata}
    .PARAMETER Path
        The absolute path to convert.
    .OUTPUTS
        Path with logical tokens substituted.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    $result = $Path
    
    # Order matters - more specific paths first
    $replacements = @(
        @{ Pattern = [regex]::Escape($env:LOCALAPPDATA); Token = '${localappdata}' }
        @{ Pattern = [regex]::Escape($env:APPDATA); Token = '${appdata}' }
        @{ Pattern = [regex]::Escape($env:USERPROFILE); Token = '${home}' }
        @{ Pattern = [regex]::Escape($env:ProgramFiles); Token = '${programfiles}' }
        @{ Pattern = [regex]::Escape(${env:ProgramFiles(x86)}); Token = '${programfiles(x86)}' }
        @{ Pattern = [regex]::Escape($env:ProgramData); Token = '${programdata}' }
    )
    
    foreach ($r in $replacements) {
        if ($r.Pattern -and $result -match "^$($r.Pattern)") {
            $result = $result -replace "^$($r.Pattern)", $r.Token
            break  # Only replace the first match
        }
    }
    
    # Normalize path separators to forward slashes for JSONC
    $result = $result -replace '\\', '/'
    
    return $result
}

function Get-ExcludePatterns {
    <#
    .SYNOPSIS
        Get the list of default exclude patterns.
    .OUTPUTS
        Array of regex pattern strings.
    #>
    return $script:ExcludePatterns
}

# Export functions
# Get-FilesystemSnapshot, Compare-FilesystemSnapshots, Apply-ExcludeHeuristics, 
# ConvertTo-LogicalToken, Test-PathMatchesExcludePattern, Get-ExcludePatterns
