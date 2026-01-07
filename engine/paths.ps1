# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Centralized path resolution for Endstate engine.

.DESCRIPTION
    Provides a single API for expanding paths in manifests and restore operations.
    Supports:
    - Windows environment variables (%LOCALAPPDATA%, %APPDATA%, etc.)
    - Tilde (~) expansion for home directory
    - Logical tokens (${home}, ${appdata}, ${localappdata}, ${config}) for future cross-platform support
    - Relative path resolution against a base directory
    
    This module is backward-compatible: existing manifests using Windows-style
    env vars continue to work unchanged. Logical tokens are optional and can
    be adopted incrementally.
#>

# Logical token definitions per platform
# These map abstract tokens to platform-specific paths
$script:LogicalTokens = @{
    # Windows mappings
    windows = @{
        '${home}'         = { if ($env:USERPROFILE) { $env:USERPROFILE } else { $env:HOME } }
        '${appdata}'      = { $env:APPDATA }
        '${localappdata}' = { $env:LOCALAPPDATA }
        '${config}'       = { $env:APPDATA }
        '${cache}'        = { $env:LOCALAPPDATA }
        '${temp}'         = { $env:TEMP }
        '${programfiles}' = { $env:ProgramFiles }
        '${programdata}'  = { $env:ProgramData }
    }
    # macOS mappings (for future use)
    macos = @{
        '${home}'         = { $env:HOME }
        '${appdata}'      = { Join-Path $env:HOME "Library/Application Support" }
        '${localappdata}' = { Join-Path $env:HOME "Library/Caches" }
        '${config}'       = { Join-Path $env:HOME "Library/Preferences" }
        '${cache}'        = { Join-Path $env:HOME "Library/Caches" }
        '${temp}'         = { $env:TMPDIR }
    }
    # Linux mappings (for future use)
    linux = @{
        '${home}'         = { $env:HOME }
        '${appdata}'      = { if ($env:XDG_DATA_HOME) { $env:XDG_DATA_HOME } else { Join-Path $env:HOME ".local/share" } }
        '${localappdata}' = { if ($env:XDG_DATA_HOME) { $env:XDG_DATA_HOME } else { Join-Path $env:HOME ".local/share" } }
        '${config}'       = { if ($env:XDG_CONFIG_HOME) { $env:XDG_CONFIG_HOME } else { Join-Path $env:HOME ".config" } }
        '${cache}'        = { if ($env:XDG_CACHE_HOME) { $env:XDG_CACHE_HOME } else { Join-Path $env:HOME ".cache" } }
        '${temp}'         = { "/tmp" }
    }
}

function Get-CurrentPlatform {
    <#
    .SYNOPSIS
        Detect the current platform.
    .OUTPUTS
        "windows", "macos", or "linux"
    #>
    
    # PowerShell 6+ has automatic variables
    if (Get-Variable -Name 'IsWindows' -Scope Global -ErrorAction SilentlyContinue) {
        $isWin = Get-Variable -Name 'IsWindows' -Scope Global -ValueOnly
        $isMac = Get-Variable -Name 'IsMacOS' -Scope Global -ValueOnly -ErrorAction SilentlyContinue
        $isLin = Get-Variable -Name 'IsLinux' -Scope Global -ValueOnly -ErrorAction SilentlyContinue
        
        if ($isWin) { return "windows" }
        if ($isMac) { return "macos" }
        if ($isLin) { return "linux" }
    }
    
    # PowerShell 5.1 fallback - always Windows
    if ($env:OS -eq "Windows_NT") {
        return "windows"
    }
    
    # Unknown platform - default to linux-like behavior
    return "linux"
}

function Expand-EndstatePath {
    <#
    .SYNOPSIS
        Expand a path with environment variables, tilde, and logical tokens.
    .DESCRIPTION
        Central path resolution function for Endstate. Supports:
        1. Windows environment variables: %LOCALAPPDATA%, %APPDATA%, etc.
        2. Tilde (~) expansion: ~/path -> $HOME/path or $USERPROFILE/path
        3. Logical tokens: ${home}, ${appdata}, ${localappdata}, ${config}
        4. Relative paths: ./path resolved against BasePath
        
        Processing order:
        1. Logical tokens (${...}) are expanded first
        2. Environment variables (%...%) are expanded
        3. Tilde (~) is expanded
        4. Relative paths are resolved
        
    .PARAMETER Path
        The path to expand.
    .PARAMETER BasePath
        Optional base path for resolving relative paths (./... or ../...).
    .PARAMETER Platform
        Optional platform override. Defaults to current platform.
    .OUTPUTS
        Expanded absolute path string.
    .EXAMPLE
        Expand-EndstatePath -Path "~/.config/app"
        # Returns: C:\Users\username\.config\app (on Windows)
    .EXAMPLE
        Expand-EndstatePath -Path "%LOCALAPPDATA%\MyApp"
        # Returns: C:\Users\username\AppData\Local\MyApp
    .EXAMPLE
        Expand-EndstatePath -Path '${localappdata}/MyApp'
        # Returns: C:\Users\username\AppData\Local\MyApp (on Windows)
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [string]$BasePath = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$Platform = $null
    )
    
    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $Path
    }
    
    $expanded = $Path
    
    # Determine platform
    $currentPlatform = if ($Platform) { $Platform } else { Get-CurrentPlatform }
    
    # Step 1: Expand logical tokens (${...})
    $tokenMap = $script:LogicalTokens[$currentPlatform]
    if ($tokenMap) {
        foreach ($token in $tokenMap.Keys) {
            if ($expanded -match [regex]::Escape($token)) {
                $value = & $tokenMap[$token]
                if ($value) {
                    $expanded = $expanded -replace [regex]::Escape($token), $value
                }
            }
        }
    }
    
    # Step 2: Expand Windows-style environment variables (%VAR%)
    # This works on all platforms via .NET
    $expanded = [Environment]::ExpandEnvironmentVariables($expanded)
    
    # Step 3: Expand tilde (~) for home directory
    if ($expanded.StartsWith("~")) {
        $homeDir = Get-HomeDirectory
        $expanded = $expanded -replace "^~", $homeDir
    }
    
    # Step 4: Resolve relative paths against BasePath
    if ($BasePath -and ($expanded.StartsWith("./") -or $expanded.StartsWith("../"))) {
        $expanded = Join-Path $BasePath $expanded
        $expanded = [System.IO.Path]::GetFullPath($expanded)
    }
    
    # Normalize path separators for current platform
    if ($currentPlatform -eq "windows") {
        $expanded = $expanded -replace '/', '\'
    } else {
        $expanded = $expanded -replace '\\', '/'
    }
    
    return $expanded
}

function Get-HomeDirectory {
    <#
    .SYNOPSIS
        Get the user's home directory in a cross-platform way.
    #>
    
    # Try HOME first (Unix-style, also set on some Windows configs)
    if ($env:HOME) {
        return $env:HOME
    }
    
    # Fall back to USERPROFILE (Windows)
    if ($env:USERPROFILE) {
        return $env:USERPROFILE
    }
    
    # Last resort: construct from HOMEDRIVE + HOMEPATH
    if ($env:HOMEDRIVE -and $env:HOMEPATH) {
        return Join-Path $env:HOMEDRIVE $env:HOMEPATH
    }
    
    # Give up - return empty string
    return ""
}

function ConvertTo-BackupPath {
    <#
    .SYNOPSIS
        Normalize a path for use in backup directory structure.
    .DESCRIPTION
        Converts an absolute path to a safe relative path for backup storage.
        On Windows: strips drive letter and colon (C:\Users\... -> Users\...)
        On Unix: strips leading slashes (/home/... -> home/...)
        
        The result is a path-safe string that can be joined to a backup root.
    .PARAMETER Path
        The absolute path to normalize.
    .OUTPUTS
        Normalized path string safe for backup directory structure.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $Path
    }
    
    $normalized = $Path
    
    # Handle Windows drive letters (C:, D:, etc.)
    # Match pattern: single letter followed by colon
    if ($normalized -match '^[A-Za-z]:') {
        $normalized = $normalized -replace '^[A-Za-z]:', ''
    }
    
    # Strip leading slashes and backslashes
    $normalized = $normalized -replace '^[/\\]+', ''
    
    # Replace any remaining problematic characters
    # Colons are invalid in paths on most filesystems
    $normalized = $normalized -replace ':', '_'
    
    return $normalized
}

function Test-IsAbsolutePath {
    <#
    .SYNOPSIS
        Check if a path is absolute.
    .DESCRIPTION
        Cross-platform check for absolute paths:
        - Windows: starts with drive letter (C:\) or UNC (\\)
        - Unix: starts with /
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $false
    }
    
    # Windows drive letter (C:\, D:\, etc.)
    if ($Path -match '^[A-Za-z]:[/\\]') {
        return $true
    }
    
    # Windows UNC path (\\server\share)
    if ($Path -match '^\\\\') {
        return $true
    }
    
    # Unix absolute path
    if ($Path.StartsWith('/')) {
        return $true
    }
    
    return $false
}

function Get-LogicalTokens {
    <#
    .SYNOPSIS
        Get the logical token definitions for a platform.
    .DESCRIPTION
        Returns a hashtable of token names to their expanded values.
        Useful for documentation or debugging.
    .PARAMETER Platform
        Platform to get tokens for. Defaults to current platform.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$Platform = $null
    )
    
    $currentPlatform = if ($Platform) { $Platform } else { Get-CurrentPlatform }
    $tokenMap = $script:LogicalTokens[$currentPlatform]
    
    if (-not $tokenMap) {
        return @{}
    }
    
    $result = @{}
    foreach ($token in $tokenMap.Keys) {
        $result[$token] = & $tokenMap[$token]
    }
    
    return $result
}

# Functions exported: Expand-EndstatePath, Get-HomeDirectory, ConvertTo-BackupPath, 
#                     Test-IsAbsolutePath, Get-CurrentPlatform, Get-LogicalTokens
