# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Trace engine for automated module generation.

.DESCRIPTION
    Provides file tracing capabilities for generating config modules:
    - Snapshot: Capture file state under %LOCALAPPDATA% and %APPDATA%
    - Diff: Compare baseline and after snapshots to find added/modified files
    - Root Detection: Group changed files by highest common parent ("A strategy")
    - Module Draft: Generate executable module.jsonc from trace data
#>

# Default exclusion patterns for module generation (v1, conservative)
$script:DefaultExcludePatterns = @(
    "**\Logs\**",
    "**\Log\**",
    "**\Cache\**",
    "**\GPUCache\**",
    "**\Code Cache\**",
    "**\Crashpad\**",
    "**\Temp\**",
    "**\tmp\**"
)

# Traced root directories (environment variable names)
$script:TracedRoots = @(
    "LOCALAPPDATA",
    "APPDATA"
)

#region Snapshot Functions

function New-TraceSnapshot {
    <#
    .SYNOPSIS
        Create a snapshot of files under traced directories.
    .DESCRIPTION
        Scans %LOCALAPPDATA% and %APPDATA% and records:
        - path (with env vars preserved, e.g. %LOCALAPPDATA%\Vendor\App\file.json)
        - size (bytes)
        - lastWriteTime (ISO 8601 format)
    .PARAMETER OutputPath
        Optional path to write snapshot JSON. If not provided, returns object only.
    .OUTPUTS
        Hashtable with metadata and files array.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$OutputPath = $null
    )
    
    $snapshot = @{
        version = 1
        timestamp = (Get-Date).ToUniversalTime().ToString("o")
        roots = @{}
        files = @()
    }
    
    foreach ($rootVar in $script:TracedRoots) {
        $rootPath = [Environment]::GetEnvironmentVariable($rootVar)
        if (-not $rootPath -or -not (Test-Path $rootPath)) {
            continue
        }
        
        $snapshot.roots[$rootVar] = $rootPath
        
        # Scan all files under this root
        $files = Get-ChildItem -Path $rootPath -Recurse -File -Force -ErrorAction SilentlyContinue
        
        foreach ($file in $files) {
            # Convert absolute path to env-var-prefixed path
            $relativePath = $file.FullName.Substring($rootPath.Length).TrimStart('\', '/')
            $envPath = "%$rootVar%\$relativePath"
            
            $snapshot.files += @{
                path = $envPath
                size = $file.Length
                lastWriteTime = $file.LastWriteTime.ToUniversalTime().ToString("o")
            }
        }
    }
    
    # Write to file if path provided
    if ($OutputPath) {
        $json = $snapshot | ConvertTo-Json -Depth 10
        $parentDir = Split-Path -Parent $OutputPath
        if ($parentDir -and -not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        $json | Out-File -FilePath $OutputPath -Encoding UTF8
    }
    
    return $snapshot
}

function Read-TraceSnapshot {
    <#
    .SYNOPSIS
        Read a trace snapshot from a JSON file.
    .PARAMETER Path
        Path to the snapshot JSON file.
    .OUTPUTS
        Hashtable with snapshot data.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    if (-not (Test-Path $Path)) {
        throw "Snapshot file not found: $Path"
    }
    
    $content = Get-Content -Path $Path -Raw -Encoding UTF8
    $snapshot = $content | ConvertFrom-Json -AsHashtable
    
    return $snapshot
}

#endregion

#region Diff Functions

function Compare-TraceSnapshots {
    <#
    .SYNOPSIS
        Compare baseline and after snapshots to find added/modified files.
    .DESCRIPTION
        Returns files that are:
        - Added: present in after but not in baseline
        - Modified: present in both but size or lastWriteTime differs
        
        Deleted files are ignored for v1.
    .PARAMETER Baseline
        Baseline snapshot (hashtable or path to JSON).
    .PARAMETER After
        After snapshot (hashtable or path to JSON).
    .OUTPUTS
        Hashtable with added and modified arrays.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Baseline,
        
        [Parameter(Mandatory = $true)]
        $After
    )
    
    # Load snapshots if paths provided
    if ($Baseline -is [string]) {
        $Baseline = Read-TraceSnapshot -Path $Baseline
    }
    if ($After -is [string]) {
        $After = Read-TraceSnapshot -Path $After
    }
    
    # Build lookup from baseline
    $baselineIndex = @{}
    foreach ($file in $Baseline.files) {
        $baselineIndex[$file.path] = $file
    }
    
    $result = @{
        added = @()
        modified = @()
    }
    
    foreach ($file in $After.files) {
        $path = $file.path
        
        if (-not $baselineIndex.ContainsKey($path)) {
            # Added file
            $result.added += $file
        } else {
            # Check if modified (size or mtime changed)
            $baseFile = $baselineIndex[$path]
            if ($file.size -ne $baseFile.size -or $file.lastWriteTime -ne $baseFile.lastWriteTime) {
                $result.modified += $file
            }
        }
    }
    
    return $result
}

#endregion

#region Root Detection Functions

function Get-TraceRootFolders {
    <#
    .SYNOPSIS
        Group changed files by their highest common parent directory.
    .DESCRIPTION
        Implements the "A strategy": finds the root folder for each app
        under %LOCALAPPDATA% or %APPDATA%.
        
        Example: Files under %LOCALAPPDATA%\Microsoft\PowerToys\...
        → root = %LOCALAPPDATA%\Microsoft\PowerToys
    .PARAMETER DiffResult
        Result from Compare-TraceSnapshots (hashtable with added/modified).
    .OUTPUTS
        Array of root folder objects with path and files.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$DiffResult
    )
    
    # Combine added and modified files
    $allChangedFiles = @()
    $allChangedFiles += $DiffResult.added
    $allChangedFiles += $DiffResult.modified
    
    if ($allChangedFiles.Count -eq 0) {
        return @()
    }
    
    # Group files by their root (first two path segments after env var)
    # e.g., %LOCALAPPDATA%\Vendor\App\file.json → %LOCALAPPDATA%\Vendor\App
    $rootGroups = @{}
    
    foreach ($file in $allChangedFiles) {
        $path = $file.path
        
        # Parse: %ENVVAR%\Segment1\Segment2\...
        if ($path -match '^(%[^%]+%)(\\[^\\]+)(\\[^\\]+)?') {
            $envVar = $Matches[1]
            $segment1 = $Matches[2]
            $segment2 = if ($Matches[3]) { $Matches[3] } else { "" }
            
            # Root is env var + first two segments (Vendor\App pattern)
            # If only one segment exists, use that
            $rootPath = if ($segment2) {
                "$envVar$segment1$segment2"
            } else {
                "$envVar$segment1"
            }
            
            if (-not $rootGroups.ContainsKey($rootPath)) {
                $rootGroups[$rootPath] = @{
                    path = $rootPath
                    envVar = $envVar
                    files = [System.Collections.ArrayList]@()
                }
            }
            
            [void]$rootGroups[$rootPath].files.Add($file)
        }
    }
    
    # Convert to array sorted by path - use ArrayList to prevent unwrapping
    $result = [System.Collections.ArrayList]@()
    foreach ($rootGroup in ($rootGroups.Values | Sort-Object -Property path)) {
        [void]$result.Add($rootGroup)
    }
    
    return ,$result.ToArray()
}

function Merge-TraceRootsToApp {
    <#
    .SYNOPSIS
        Merge multiple roots into a single app definition.
    .DESCRIPTION
        If both %APPDATA% and %LOCALAPPDATA% roots exist for the same app,
        emit ONE module with multiple restore/capture entries.
    .PARAMETER Roots
        Array of root folder objects from Get-TraceRootFolders.
    .OUTPUTS
        Array of app definitions, each potentially with multiple roots.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [array]$Roots
    )
    
    if ($Roots.Count -eq 0) {
        return @()
    }
    
    # Group by app name (last segment of root path, ignoring env var)
    # This allows merging %APPDATA%\Vendor\App and %LOCALAPPDATA%\Vendor\App
    $appGroups = @{}
    
    foreach ($root in $Roots) {
        # Extract segments from path: %ENVVAR%\Vendor\AppName → ["", "Vendor", "AppName"]
        # Note: path is like %LOCALAPPDATA%\Vendor\App
        $pathWithoutEnv = $root.path -replace '^%[^%]+%', ''
        $segments = $pathWithoutEnv.TrimStart('\') -split '\\'
        
        # App name is the last segment
        $appName = if ($segments.Count -gt 0) { $segments[-1] } else { "unknown" }
        
        # Create a normalized key (vendor\app pattern without env var)
        $vendorApp = if ($segments.Count -ge 2) {
            "$($segments[0])\$($segments[1])"
        } elseif ($segments.Count -eq 1) {
            $segments[0]
        } else {
            "unknown"
        }
        
        if (-not $appGroups.ContainsKey($vendorApp)) {
            $appGroups[$vendorApp] = @{
                name = $appName
                vendorApp = $vendorApp
                roots = [System.Collections.ArrayList]@()
            }
        }
        
        [void]$appGroups[$vendorApp].roots.Add($root)
    }
    
    # Convert to array - ensure we always return an array even with single item
    $result = [System.Collections.ArrayList]@()
    foreach ($appGroup in ($appGroups.Values | Sort-Object -Property vendorApp)) {
        [void]$result.Add($appGroup)
    }
    
    # Return as proper array
    return ,$result.ToArray()
}

#endregion

#region Exclusion Functions

function Get-DefaultExcludePatterns {
    <#
    .SYNOPSIS
        Returns the default exclusion patterns for module generation.
    #>
    return $script:DefaultExcludePatterns
}

function Test-PathMatchesExclude {
    <#
    .SYNOPSIS
        Test if a path matches any exclusion pattern.
    .PARAMETER Path
        The path to test (can be relative or absolute).
    .PARAMETER Patterns
        Array of exclusion patterns.
    .OUTPUTS
        $true if path matches any pattern, $false otherwise.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [string[]]$Patterns = @()
    )
    
    if ($Patterns.Count -eq 0) {
        return $false
    }
    
    # Normalize path separators to backslash
    $normalizedPath = $Path -replace '/', '\'
    
    foreach ($pattern in $Patterns) {
        # Normalize pattern separators
        $normalizedPattern = $pattern -replace '/', '\'
        
        # Use simple wildcard matching with -like
        # For **\Logs\** pattern, we need to check if path contains \Logs\
        # Extract the key folder name from pattern like **\Logs\**
        if ($normalizedPattern -match '^\*\*\\([^\\]+)\\\*\*$') {
            $folderName = $Matches[1]
            # Check if path contains this folder as a segment
            if ($normalizedPath -match "(^|\\)$([regex]::Escape($folderName))(\\|$)") {
                return $true
            }
        } else {
            # Fallback to -like matching for other patterns
            if ($normalizedPath -like $normalizedPattern) {
                return $true
            }
        }
    }
    
    return $false
}

#endregion

#region Module Draft Functions

function New-ModuleDraft {
    <#
    .SYNOPSIS
        Generate a module.jsonc from trace data.
    .DESCRIPTION
        Creates an executable module definition with:
        - id (derived from folder/app name)
        - displayName (best-effort from app name)
        - restore entries with directory-level copy
        - capture entries
        - exclude patterns (defaults applied)
    .PARAMETER TracePath
        Path to directory containing baseline.json and after.json.
    .PARAMETER OutputPath
        Path to write the generated module.jsonc.
    .PARAMETER AppName
        Optional app name override. If not provided, derived from trace data.
    .OUTPUTS
        The generated module hashtable.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$TracePath,
        
        [Parameter(Mandatory = $true)]
        [string]$OutputPath,
        
        [Parameter(Mandatory = $false)]
        [string]$AppName = $null
    )
    
    # Load snapshots
    $baselinePath = Join-Path $TracePath "baseline.json"
    $afterPath = Join-Path $TracePath "after.json"
    
    if (-not (Test-Path $baselinePath)) {
        throw "Baseline snapshot not found: $baselinePath"
    }
    if (-not (Test-Path $afterPath)) {
        throw "After snapshot not found: $afterPath"
    }
    
    $baseline = Read-TraceSnapshot -Path $baselinePath
    $after = Read-TraceSnapshot -Path $afterPath
    
    # Compute diff
    $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
    
    if ($diff.added.Count -eq 0 -and $diff.modified.Count -eq 0) {
        throw "No changes detected between baseline and after snapshots"
    }
    
    # Detect root folders
    $roots = Get-TraceRootFolders -DiffResult $diff
    
    if ($roots.Count -eq 0) {
        throw "No root folders detected from changes"
    }
    
    # Merge roots into app definition
    $apps = Merge-TraceRootsToApp -Roots $roots
    
    if ($apps.Count -eq 0) {
        throw "No apps detected from root folders"
    }
    
    # Use first app (one module per app for v1)
    $app = $apps[0]
    
    # Derive app name and ID
    $derivedName = if ($AppName) { $AppName } elseif ($app.name) { $app.name } else { "unknown-app" }
    if (-not $derivedName -or $derivedName -eq "") {
        $derivedName = "unknown-app"
    }
    $moduleId = "apps." + ($derivedName.ToLower() -replace '[^a-z0-9]', '-' -replace '-+', '-' -replace '^-|-$', '')
    $displayName = $derivedName -replace '[-_]', ' '
    # Title case the display name
    $displayName = (Get-Culture).TextInfo.ToTitleCase($displayName.ToLower())
    
    # Build restore entries
    $restoreEntries = @()
    $captureFiles = @()
    
    foreach ($root in $app.roots) {
        # Derive relative source path for restore
        # e.g., %LOCALAPPDATA%\Microsoft\PowerToys → ./configs/powertoys
        $configSubpath = $derivedName.ToLower() -replace '[^a-z0-9]', '-'
        
        $restoreEntry = @{
            type = "copy"
            source = "./configs/$configSubpath"
            target = $root.path
            backup = $true
            exclude = $script:DefaultExcludePatterns
        }
        $restoreEntries += $restoreEntry
        
        # Build capture entry
        $captureFile = @{
            source = $root.path
            dest = "apps/$configSubpath"
            optional = $true
        }
        $captureFiles += $captureFile
    }
    
    # Build module structure
    $module = [ordered]@{
        id = $moduleId
        displayName = $displayName
        sensitivity = "low"
        matches = [ordered]@{
            winget = @()
            exe = @()
            uninstallDisplayName = @()
        }
        verify = @()
        restore = $restoreEntries
        capture = [ordered]@{
            files = $captureFiles
            exclude = $script:DefaultExcludePatterns
        }
        notes = "Auto-generated module from trace. Review and update matches, verify, and exclude patterns as needed."
    }
    
    # Write module to file
    $parentDir = Split-Path -Parent $OutputPath
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    # Convert to JSONC with comments
    $jsonContent = ConvertTo-ModuleJsonc -Module $module
    $jsonContent | Out-File -FilePath $OutputPath -Encoding UTF8
    
    return $module
}

function ConvertTo-ModuleJsonc {
    <#
    .SYNOPSIS
        Convert a module hashtable to JSONC format with helpful comments.
    .PARAMETER Module
        The module hashtable to convert.
    .OUTPUTS
        JSONC string.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Module
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    [void]$sb.AppendLine("{")
    [void]$sb.AppendLine("  // Auto-generated config module - review and customize before use")
    [void]$sb.AppendLine("  ")
    [void]$sb.AppendLine("  `"id`": `"$($Module.id)`",")
    [void]$sb.AppendLine("  `"displayName`": `"$($Module.displayName)`",")
    [void]$sb.AppendLine("  `"sensitivity`": `"$($Module.sensitivity)`",")
    [void]$sb.AppendLine("  ")
    
    # Matches section
    [void]$sb.AppendLine("  // TODO: Add matchers to identify when this app is installed")
    [void]$sb.AppendLine("  `"matches`": {")
    [void]$sb.AppendLine("    `"winget`": [],")
    [void]$sb.AppendLine("    `"exe`": [],")
    [void]$sb.AppendLine("    `"uninstallDisplayName`": []")
    [void]$sb.AppendLine("  },")
    [void]$sb.AppendLine("  ")
    
    # Verify section
    [void]$sb.AppendLine("  // TODO: Add verification checks")
    [void]$sb.AppendLine("  `"verify`": [],")
    [void]$sb.AppendLine("  ")
    
    # Restore section
    [void]$sb.AppendLine("  `"restore`": [")
    $restoreCount = $Module.restore.Count
    for ($i = 0; $i -lt $restoreCount; $i++) {
        $entry = $Module.restore[$i]
        $comma = if ($i -lt $restoreCount - 1) { "," } else { "" }
        
        [void]$sb.AppendLine("    {")
        [void]$sb.AppendLine("      `"type`": `"$($entry.type)`",")
        [void]$sb.AppendLine("      `"source`": `"$($entry.source -replace '\\', '\\')`",")
        [void]$sb.AppendLine("      `"target`": `"$($entry.target -replace '\\', '\\')`",")
        [void]$sb.AppendLine("      `"backup`": $($entry.backup.ToString().ToLower()),")
        [void]$sb.AppendLine("      `"exclude`": [")
        
        $excludeCount = $entry.exclude.Count
        for ($j = 0; $j -lt $excludeCount; $j++) {
            $pattern = $entry.exclude[$j] -replace '\\', '\\'
            $excludeComma = if ($j -lt $excludeCount - 1) { "," } else { "" }
            [void]$sb.AppendLine("        `"$pattern`"$excludeComma")
        }
        
        [void]$sb.AppendLine("      ]")
        [void]$sb.AppendLine("    }$comma")
    }
    [void]$sb.AppendLine("  ],")
    [void]$sb.AppendLine("  ")
    
    # Capture section
    [void]$sb.AppendLine("  `"capture`": {")
    [void]$sb.AppendLine("    `"files`": [")
    
    $filesCount = $Module.capture.files.Count
    for ($i = 0; $i -lt $filesCount; $i++) {
        $file = $Module.capture.files[$i]
        $comma = if ($i -lt $filesCount - 1) { "," } else { "" }
        
        [void]$sb.AppendLine("      { `"source`": `"$($file.source -replace '\\', '\\')`", `"dest`": `"$($file.dest)`", `"optional`": $($file.optional.ToString().ToLower()) }$comma")
    }
    
    [void]$sb.AppendLine("    ],")
    [void]$sb.AppendLine("    `"exclude`": [")
    
    $captureExcludeCount = $Module.capture.exclude.Count
    for ($i = 0; $i -lt $captureExcludeCount; $i++) {
        $pattern = $Module.capture.exclude[$i] -replace '\\', '\\'
        $comma = if ($i -lt $captureExcludeCount - 1) { "," } else { "" }
        [void]$sb.AppendLine("      `"$pattern`"$comma")
    }
    
    [void]$sb.AppendLine("    ]")
    [void]$sb.AppendLine("  },")
    [void]$sb.AppendLine("  ")
    [void]$sb.AppendLine("  `"notes`": `"$($Module.notes)`"")
    [void]$sb.AppendLine("}")
    
    return $sb.ToString()
}

#endregion

# Exported functions:
# - New-TraceSnapshot
# - Read-TraceSnapshot
# - Compare-TraceSnapshots
# - Get-TraceRootFolders
# - Merge-TraceRootsToApp
# - Get-DefaultExcludePatterns
# - Test-PathMatchesExclude
# - New-ModuleDraft
