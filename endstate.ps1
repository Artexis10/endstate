# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Endstate - Root orchestrator CLI.

.DESCRIPTION
    Primary entrypoint for the Endstate project.
    Delegates commands to appropriate subsystems (currently provisioning).

.PARAMETER Command
    The command to execute: apply, capture, plan, verify, report, doctor

.PARAMETER Profile
    Profile name (maps to provisioning -Profile).

.PARAMETER Manifest
    Path to manifest file (bypasses profile mapping, passed directly to provisioning).

.PARAMETER DryRun
    Preview changes without applying them.

.PARAMETER EnableRestore
    Enable restore operations during apply (opt-in for safety).

.PARAMETER Latest
    Show most recent run for report command.

.PARAMETER RunId
    Specific run ID to retrieve for report command.

.PARAMETER Last
    Show N most recent runs for report command.

.PARAMETER Json
    Output report as JSON.

.EXAMPLE
    .\endstate.ps1 apply -Profile hugo-win11
    Apply the hugo-win11 profile manifest.

.EXAMPLE
    .\endstate.ps1 apply -Profile hugo-win11 -DryRun
    Preview what would be applied.

.EXAMPLE
    .\endstate.ps1 capture -Profile hugo-win11
    Capture current machine state to hugo-win11 profile.

.EXAMPLE
    .\endstate.ps1 report -Latest
    Show most recent provisioning run.
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0, Mandatory = $false)]
    [ValidateSet("apply", "capture", "plan", "verify", "report", "doctor", "state", "bootstrap", "capabilities", "validate", "")]
    [string]$Command,
    
    # Internal flag for dot-sourcing to load functions without running main logic
    [Parameter(Mandatory = $false)]
    [switch]$LoadFunctionsOnly,

    [Parameter(Mandatory = $false)]
    [Alias("v")]
    [switch]$Version,

    [Parameter(Mandatory = $false)]
    [string]$Profile,

    [Parameter(Mandatory = $false)]
    [string]$Manifest,

    [Parameter(Mandatory = $false)]
    [string]$Out,

    [Parameter(Mandatory = $false)]
    [switch]$Example,

    [Parameter(Mandatory = $false)]
    [switch]$Sanitize,

    [Parameter(Mandatory = $false)]
    [string]$Name,

    [Parameter(Mandatory = $false)]
    [string]$ExamplesDir,

    [Parameter(Mandatory = $false)]
    [switch]$Force,

    [Parameter(Mandatory = $false)]
    [switch]$DryRun,

    [Parameter(Mandatory = $false)]
    [switch]$OnlyApps,

    [Parameter(Mandatory = $false)]
    [switch]$EnableRestore,

    [Parameter(Mandatory = $false)]
    [switch]$Latest,

    [Parameter(Mandatory = $false)]
    [string]$RunId,

    [Parameter(Mandatory = $false)]
    [int]$Last = 0,

    [Parameter(Mandatory = $false)]
    [switch]$Json,

    # State subcommand (e.g., "reset", "export", "import")
    [Parameter(Position = 1, Mandatory = $false)]
    [string]$SubCommand,

    # State import: input file path
    [Parameter(Mandatory = $false)]
    [string]$In,

    # State import: merge mode (default)
    [Parameter(Mandatory = $false)]
    [switch]$Merge,

    # State import: replace mode
    [Parameter(Mandatory = $false)]
    [switch]$Replace,

    # Bootstrap: repo root path
    [Parameter(Mandatory = $false)]
    [string]$RepoRoot,
    
    # Streaming events output format (jsonl for NDJSON to stderr)
    [Parameter(Mandatory = $false)]
    [ValidateSet("jsonl", "")]
    [string]$Events,
    
    # Capture remaining arguments for GNU-style flag processing
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$RemainingArgs
)

$ErrorActionPreference = "Stop"
$script:EndstateRoot = $PSScriptRoot

#region GNU-style Flag Normalization
# Normalize GNU-style double-dash flags to PowerShell convention
# This allows commands like: endstate apply --profile Hugo-Laptop --json
# to work alongside PowerShell-style: endstate apply -Profile Hugo-Laptop -Json

# Process remaining arguments captured by ValueFromRemainingArguments
if ($RemainingArgs) {
    $i = 0
    while ($i -lt $RemainingArgs.Count) {
        $arg = $RemainingArgs[$i]
        
        switch ($arg) {
            '--json' {
                $Json = $true
                $i++
            }
            '--profile' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    $Profile = $RemainingArgs[$i + 1]
                    $i += 2
                } else {
                    $i++
                }
            }
            '--manifest' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    $Manifest = $RemainingArgs[$i + 1]
                    $i += 2
                } else {
                    $i++
                }
            }
            '--out' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    $Out = $RemainingArgs[$i + 1]
                    $i += 2
                } else {
                    $i++
                }
            }
            '--latest' {
                $Latest = $true
                $i++
            }
            '--runid' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    $RunId = $RemainingArgs[$i + 1]
                    $i += 2
                } else {
                    $i++
                }
            }
            '--last' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    try {
                        $Last = [int]$RemainingArgs[$i + 1]
                        $i += 2
                    } catch {
                        $i++
                    }
                } else {
                    $i++
                }
            }
            '--dry-run' {
                $DryRun = $true
                $i++
            }
            '--events' {
                if ($i + 1 -lt $RemainingArgs.Count) {
                    $Events = $RemainingArgs[$i + 1]
                    $i += 2
                } else {
                    $i++
                }
            }
            '--enable-restore' {
                $EnableRestore = $true
                $i++
            }
            '--help' {
                $Command = ""
                $i++
            }
            default {
                # Skip unknown args
                $i++
            }
        }
    }
}

# Also check $MyInvocation.Line for --json (fallback for direct script invocation)
$commandLine = $MyInvocation.Line
if ($commandLine -match '\s--json(\s|$)') {
    $Json = $true
}

#endregion GNU-style Flag Normalization

function Get-EndstateVersion {
    <#
    .SYNOPSIS
        Returns the current version of Endstate.
    .DESCRIPTION
        If VERSION.txt exists (release build), returns its content.
        Otherwise returns dev version: 0.0.0-dev+<short git sha>
    #>
    $versionFile = Join-Path $script:EndstateRoot "VERSION.txt"
    
    if (Test-Path $versionFile) {
        $version = (Get-Content -Path $versionFile -Raw).Trim()
        return $version
    }
    
    # Dev version: try to get git sha
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return "0.0.0-dev+$gitSha"
        }
    } catch {
        # Git not available
    }
    
    return "0.0.0-dev"
}

function Get-GitSha {
    <#
    .SYNOPSIS
        Returns the current git commit SHA (short form), or $null if unavailable.
    #>
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return $gitSha.Trim()
        }
    } catch {
        # Git not available
    }
    return $null
}

$script:VersionString = Get-EndstateVersion

# Provisioning CLI path will be resolved lazily by Get-ProvisioningCliPath function
# to avoid calling Get-RepoRootPath before it's defined
$script:ProvisioningCliPath = $null

# Allow override of winget script for testing (path to .ps1 file)
$script:WingetScript = $env:ENDSTATE_WINGET_SCRIPT

# Local manifests directory (gitignored)
$script:LocalManifestsDir = Join-Path $script:EndstateRoot "manifests\local"

# Examples directory (committed, shareable)
$script:ExamplesManifestsDir = Join-Path $script:EndstateRoot "manifests\examples"

# State directory (repo-local, gitignored)
$script:EndstateStateDir = Join-Path $script:EndstateRoot ".endstate"
$script:EndstateStatePath = Join-Path $script:EndstateStateDir "state.json"

#region State Store Helpers

function Get-EndstateStatePath {
    return $script:EndstateStatePath
}

function Get-EndstateStateDir {
    return $script:EndstateStateDir
}

function Read-EndstateState {
    $statePath = Get-EndstateStatePath
    if (-not (Test-Path $statePath)) {
        return $null
    }
    try {
        $content = Get-Content -Path $statePath -Raw -ErrorAction Stop
        return $content | ConvertFrom-Json
    } catch {
        Write-Host "[WARN] Failed to read state file: $_" -ForegroundColor Yellow
        return $null
    }
}

function Write-EndstateStateAtomic {
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State
    )
    
    $stateDir = Get-EndstateStateDir
    $statePath = Get-EndstateStatePath
    
    # Ensure state directory exists
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    
    # Write to temp file first, then move (atomic on same filesystem)
    $tempPath = Join-Path $stateDir "state.tmp.$([guid]::NewGuid().ToString('N').Substring(0,8)).json"
    
    try {
        $jsonContent = $State | ConvertTo-Json -Depth 10
        Set-Content -Path $tempPath -Value $jsonContent -Encoding UTF8 -ErrorAction Stop
        
        # Move temp to final (atomic replace)
        Move-Item -Path $tempPath -Destination $statePath -Force -ErrorAction Stop
        return $true
    } catch {
        Write-Host "[ERROR] Failed to write state file: $_" -ForegroundColor Red
        if (Test-Path $tempPath) {
            Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
        }
        return $false
    }
}

function New-EndstateState {
    return @{
        schemaVersion = 1
        lastApplied = $null
        lastVerify = $null
        appsObserved = @{}
    }
}

function Get-ManifestHash {
    param([string]$Path)
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    # Read as bytes and normalize line endings for deterministic hash
    $content = Get-Content -Path $Path -Raw
    # Normalize CRLF to LF for consistent hashing across platforms
    $normalized = $content -replace "`r`n", "`n"
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalized)
    
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    $hashBytes = $sha256.ComputeHash($bytes)
    $hashString = [BitConverter]::ToString($hashBytes) -replace '-', ''
    
    return $hashString.ToLower()
}

function Get-InstalledAppsMap {
    # Returns a hashtable of winget ID -> version (or $true if version unknown)
    $installedApps = Get-InstalledApps
    $map = @{}
    
    $headerPassed = $false
    foreach ($line in $installedApps) {
        if (-not $line) { continue }
        
        # Skip header lines (look for separator line with dashes)
        if ($line -match '^-+$') {
            $headerPassed = $true
            continue
        }
        if (-not $headerPassed) { continue }
        
        # Parse line: Name, Id, Version (tab or multi-space separated)
        # Winget output is column-aligned, so we look for the ID pattern
        if ($line -match '\s+([A-Za-z0-9._-]+\.[A-Za-z0-9._-]+)\s+([\d.]+)') {
            $id = $Matches[1]
            $version = $Matches[2]
            $map[$id] = $version
        } elseif ($line -match '\s+([A-Za-z0-9._-]+\.[A-Za-z0-9._-]+)') {
            $id = $Matches[1]
            $map[$id] = $true
        }
    }
    
    return $map
}

function Compute-Drift {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        [Parameter(Mandatory = $false)]
        [hashtable]$InstalledAppsMap = $null
    )
    
    $manifest = Read-Manifest -Path $ManifestPath
    if (-not $manifest) {
        return @{
            Success = $false
            Error = "Failed to read manifest"
            Missing = @()
            Extra = @()
            VersionMismatches = @()
        }
    }
    
    if (-not $InstalledAppsMap) {
        $InstalledAppsMap = Get-InstalledAppsMap
    }
    
    # Get required app IDs from manifest
    $requiredIds = @()
    foreach ($app in $manifest.apps) {
        $wingetId = $app.refs.windows
        if ($wingetId) {
            $requiredIds += $wingetId
        }
    }
    
    # Missing: required but not installed
    $missing = @()
    foreach ($id in $requiredIds) {
        $found = $false
        foreach ($installedId in $InstalledAppsMap.Keys) {
            if ($installedId -eq $id) {
                $found = $true
                break
            }
        }
        if (-not $found) {
            $missing += $id
        }
    }
    
    # Extra: installed but not in manifest (observed extras)
    $extra = @()
    foreach ($installedId in $InstalledAppsMap.Keys) {
        if ($installedId -notin $requiredIds) {
            $extra += $installedId
        }
    }
    
    return @{
        Success = $true
        Missing = $missing
        Extra = $extra
        VersionMismatches = @()  # MVP: not comparing versions yet
        MissingCount = $missing.Count
        ExtraCount = $extra.Count
    }
}

#endregion State Store Helpers

#region PATH Installation

function Get-RepoRootPath {
    <#
    .SYNOPSIS
        Get the repo root path from environment variable or persisted file.
    .DESCRIPTION
        Priority:
        1. $env:ENDSTATE_ROOT (if set)
        2. %LOCALAPPDATA%\Endstate\repo-root.txt (if exists)
        3. $null (not configured)
    #>
    
    # Priority 1: Environment variable override
    if ($env:ENDSTATE_ROOT) {
        if (Test-Path $env:ENDSTATE_ROOT) {
            return $env:ENDSTATE_ROOT
        } else {
            Write-Warning "ENDSTATE_ROOT is set but path does not exist: $env:ENDSTATE_ROOT"
        }
    }
    
    # Priority 2: Persisted repo-root.txt
    $repoRootFile = Join-Path $env:LOCALAPPDATA "Endstate\repo-root.txt"
    if (Test-Path $repoRootFile) {
        try {
            $persistedRoot = (Get-Content -Path $repoRootFile -Raw -ErrorAction Stop).Trim()
            if ($persistedRoot -and (Test-Path $persistedRoot)) {
                return $persistedRoot
            }
        } catch {
            Write-Warning "Failed to read repo-root.txt: $_"
        }
    }
    
    return $null
}

function Find-RepoRoot {
    <#
    .SYNOPSIS
        Detect repo root by walking up from current directory.
    .DESCRIPTION
        Searches for:
        1. Current directory if it contains manifests directory
        2. Parent directories until finding .git or manifests directory
        Returns $null if not found.
    #>
    param(
        [string]$StartPath = (Get-Location).Path
    )
    
    $currentPath = $StartPath
    
    # Check if current directory contains manifests
    $manifestsPath = Join-Path $currentPath "manifests"
    if (Test-Path $manifestsPath) {
        return $currentPath
    }
    
    # Walk up parent directories
    while ($currentPath) {
        # Check for .git directory
        $gitPath = Join-Path $currentPath ".git"
        if (Test-Path $gitPath) {
            # Verify it also has manifests
            $manifestsPath = Join-Path $currentPath "manifests"
            if (Test-Path $manifestsPath) {
                return $currentPath
            }
        }
        
        # Check for manifests directly
        $manifestsPath = Join-Path $currentPath "manifests"
        if (Test-Path $manifestsPath) {
            return $currentPath
        }
        
        # Move to parent
        $parent = Split-Path -Parent $currentPath
        if ($parent -eq $currentPath) {
            # Reached root of filesystem
            break
        }
        $currentPath = $parent
    }
    
    return $null
}

function Set-RepoRootPath {
    <#
    .SYNOPSIS
        Persist repo root path to %LOCALAPPDATA%\Endstate\repo-root.txt
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    $repoRootFile = Join-Path $env:LOCALAPPDATA "Endstate\repo-root.txt"
    
    # Ensure directory exists
    $parentDir = Split-Path -Parent $repoRootFile
    if (-not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    try {
        Set-Content -Path $repoRootFile -Value $Path -Encoding UTF8 -ErrorAction Stop
        return $true
    } catch {
        Write-Host "[ERROR] Failed to write repo-root.txt: $_" -ForegroundColor Red
        return $false
    }
}

function Install-EndstateToPath {
    <#
    .SYNOPSIS
        Install endstate command to user PATH (idempotent).
    .DESCRIPTION
        Creates %LOCALAPPDATA%\Endstate\bin directory, installs CLI entrypoint,
        creates CMD shim, and adds to user PATH if not already present.
        Optionally persists repo root path for profile resolution.
        Fully idempotent - safe to run multiple times.
    .PARAMETER RepoRootPath
        Optional explicit repo root path. If not provided, attempts auto-detection.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$RepoRootPath
    )
    
    $binDir = Join-Path $env:LOCALAPPDATA "Endstate\bin"
    $cliEntrypoint = Join-Path $binDir "endstate.ps1"
    $cmdShim = Join-Path $binDir "endstate.cmd"
    
    Write-Host ""
    Write-Host "=== Endstate Bootstrap ==="  -ForegroundColor Cyan
    Write-Host ""
    
    # Create bin directory if it doesn't exist
    if (-not (Test-Path $binDir)) {
        Write-Host "[CREATE] Creating directory: $binDir" -ForegroundColor Green
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    } else {
        Write-Host "[OK] Directory exists: $binDir" -ForegroundColor DarkGray
    }
    
    # Copy endstate.ps1 to bin directory
    $sourceScript = $PSCommandPath
    if (Test-Path $cliEntrypoint) {
        Write-Host "[UPDATE] Updating CLI entrypoint: $cliEntrypoint" -ForegroundColor Yellow
    } else {
        Write-Host "[INSTALL] Installing CLI entrypoint: $cliEntrypoint" -ForegroundColor Green
    }
    Copy-Item -Path $sourceScript -Destination $cliEntrypoint -Force
    
    # Create CMD shim
    $shimContent = @"
@echo off
REM Endstate CLI shim - forwards all arguments to PowerShell
pwsh -NoProfile -ExecutionPolicy Bypass -File "%LOCALAPPDATA%\Endstate\bin\endstate.ps1" %*
"@
    
    if (Test-Path $cmdShim) {
        Write-Host "[UPDATE] Updating CMD shim: $cmdShim" -ForegroundColor Yellow
    } else {
        Write-Host "[INSTALL] Creating CMD shim: $cmdShim" -ForegroundColor Green
    }
    Set-Content -Path $cmdShim -Value $shimContent -Encoding ASCII
    
    # Add to user PATH if not already present
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = $userPath -split ';' | Where-Object { $_ }
    
    $alreadyInPath = $false
    foreach ($entry in $pathEntries) {
        $normalizedEntry = [System.IO.Path]::GetFullPath($entry).TrimEnd('\\')
        $normalizedBinDir = [System.IO.Path]::GetFullPath($binDir).TrimEnd('\\')
        if ($normalizedEntry -eq $normalizedBinDir) {
            $alreadyInPath = $true
            break
        }
    }
    
    if ($alreadyInPath) {
        Write-Host "[OK] Already in PATH: $binDir" -ForegroundColor DarkGray
    } else {
        Write-Host "[PATH] Adding to user PATH: $binDir" -ForegroundColor Green
        $newPath = if ($userPath) { "$userPath;$binDir" } else { $binDir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        
        # Update current session PATH
        $env:Path = "$env:Path;$binDir"
        
        Write-Host ""
        Write-Host "[INFO] PATH updated. You may need to restart your terminal for changes to take effect." -ForegroundColor Cyan
    }
    
    # Handle repo root persistence
    $repoRootConfigured = $false
    $repoRootPath = $null
    
    if ($RepoRootPath) {
        # Explicit -RepoRoot provided
        if (Test-Path $RepoRootPath) {
            $manifestsPath = Join-Path $RepoRootPath "manifests"
            if (Test-Path $manifestsPath) {
                Write-Host "[REPO-ROOT] Using provided path: $RepoRootPath" -ForegroundColor Green
                if (Set-RepoRootPath -Path $RepoRootPath) {
                    $repoRootConfigured = $true
                    $repoRootPath = $RepoRootPath
                }
            } else {
                Write-Host "[WARN] Provided repo root does not contain manifests: $RepoRootPath" -ForegroundColor Yellow
                Write-Host "       Profile resolution may not work correctly." -ForegroundColor Yellow
            }
        } else {
            Write-Host "[WARN] Provided repo root path does not exist: $RepoRootPath" -ForegroundColor Yellow
        }
    } else {
        # Auto-detect repo root
        $detectedRoot = Find-RepoRoot
        if ($detectedRoot) {
            Write-Host "[REPO-ROOT] Auto-detected: $detectedRoot" -ForegroundColor Green
            if (Set-RepoRootPath -Path $detectedRoot) {
                $repoRootConfigured = $true
                $repoRootPath = $detectedRoot
            }
        } else {
            Write-Host "[WARN] Could not auto-detect repo root." -ForegroundColor Yellow
            Write-Host "       To enable profile resolution, run:" -ForegroundColor Yellow
            Write-Host "       endstate bootstrap -RepoRoot <path-to-endstate>" -ForegroundColor Cyan
            Write-Host "       Or set environment variable: `$env:ENDSTATE_ROOT" -ForegroundColor Cyan
        }
    }
    
    Write-Host ""
    Write-Host "[SUCCESS] Bootstrap complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "You can now run 'endstate --help' from any directory." -ForegroundColor Cyan
    
    if ($repoRootConfigured) {
        Write-Host "Profile resolution configured for: $repoRootPath" -ForegroundColor Cyan
    }
    
    Write-Host ""
    
    return @{ Success = $true; ExitCode = 0; BinDir = $binDir; RepoRootConfigured = $repoRootConfigured; RepoRoot = $repoRootPath }
}

#endregion PATH Installation

function Show-Banner {
    # In JSON mode, route banner to Information stream instead of stdout
    # This keeps stdout pure for JSON output
    if ($Json.IsPresent) {
        Write-Information "" -InformationAction Continue
        Write-Information "Endstate - $script:VersionString" -InformationAction Continue
        Write-Information "" -InformationAction Continue
    } else {
        Write-Host ""
        Write-Host "Endstate - $script:VersionString" -ForegroundColor Cyan
        Write-Host ""
    }
}

function Show-Help {
    Show-Banner
    Write-Host "USAGE:" -ForegroundColor Yellow
    Write-Host "    .\endstate.ps1 <command> [options]"
    Write-Host ""
    Write-Host "COMMANDS:" -ForegroundColor Yellow
    Write-Host "    bootstrap     Install endstate command to user PATH"
    Write-Host "    capture       Capture current machine state into a manifest"
    Write-Host "    apply         Apply manifest to current machine"
    Write-Host "    verify        Verify current state matches manifest"
    Write-Host "    plan          Generate execution plan from manifest"
    Write-Host "    validate      Validate a profile manifest against the contract"
    Write-Host "    report        Show state summary and drift"
    Write-Host "    doctor        Diagnose environment issues"
    Write-Host "    state         Manage endstate state (subcommands: reset, export, import)"
    Write-Host "    capabilities  List available commands (use -Json for machine-readable output)"
    Write-Host ""
    Write-Host "CAPTURE OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Out <path>        Output path (overrides all defaults)"
    Write-Host "    -Sanitize          Remove machine-specific fields, secrets, local paths; stable sort"
    Write-Host "    -Name <string>     Manifest name (used for filename when -Sanitize)"
    Write-Host "    -ExamplesDir <p>   Examples directory (default: provisioning/manifests/examples/)"
    Write-Host "    -Force             Overwrite existing example manifests without prompting"
    Write-Host "    -Example           (Legacy) Generate sanitized example manifest"
    Write-Host ""
    Write-Host "APPLY OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>   Path to manifest file"
    Write-Host "    -Profile <name>    Profile name (resolves to manifests/<name>.jsonc)"
    Write-Host "    -DryRun            Preview changes without applying"
    Write-Host "    -OnlyApps          Install apps only (skip restore/verify)"
    Write-Host "    -EnableRestore     Enable config restoration during apply"
    Write-Host ""
    Write-Host "VERIFY OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>   Path to manifest file"
    Write-Host "    -Profile <name>    Profile name (resolves to manifests/<name>.jsonc)"
    Write-Host ""
    Write-Host "REPORT OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>   Include current drift against manifest"
    Write-Host "    -Json              Output as JSON (pure JSON to stdout, no wrapper text)"
    Write-Host "    -Out <path>        Write JSON to file (atomic write)"
    Write-Host ""
    Write-Host "STATE SUBCOMMANDS:" -ForegroundColor Yellow
    Write-Host "    reset              Delete .endstate/state.json (non-destructive)"
    Write-Host "    export -Out <p>    Export state to file (atomic, valid schema even if empty)"
    Write-Host "    import -In <p>     Import state from file (default: merge)"
    Write-Host "      [-Merge]         Merge incoming (newer timestamps win)"
    Write-Host "      [-Replace]       Replace entirely (backup existing first)"
    Write-Host ""
    Write-Host "EXAMPLES:" -ForegroundColor Yellow
    Write-Host "    .\endstate.ps1 capture                                    # Capture to local/<machine>.jsonc"
    Write-Host "    .\endstate.ps1 capture -Out my-manifest.jsonc             # Capture to specific path"
    Write-Host "    .\endstate.ps1 capture -Sanitize -Name example-win-core   # Sanitized to examples/"
    Write-Host "    .\endstate.ps1 capture -Example                           # (Legacy) Generate example fixture"
    Write-Host "    .\endstate.ps1 apply -Manifest manifest.jsonc   # Apply manifest"
    Write-Host "    .\endstate.ps1 apply -Manifest manifest.jsonc -DryRun"
    Write-Host "    .\endstate.ps1 verify -Manifest manifest.jsonc  # Verify apps installed"
    Write-Host "    .\endstate.ps1 report -Latest"
    Write-Host "    .\endstate.ps1 doctor"
    Write-Host ""
}

function Get-ProvisioningCliPath {
    <#
    .SYNOPSIS
        Resolve provisioning CLI path using repo root resolution (lazy evaluation).
    .DESCRIPTION
        Priority: 1) ENDSTATE_PROVISIONING_CLI env var (testing override)
                  2) Repo root resolution (ENDSTATE_ROOT -> repo-root.txt -> fallback)
    #>
    
    # Check for testing override first
    if ($env:ENDSTATE_PROVISIONING_CLI) {
        return $env:ENDSTATE_PROVISIONING_CLI
    }
    
    # Determine repo root using the same logic as profile resolution
    $repoRoot = Get-RepoRootPath
    
    if (-not $repoRoot) {
        # Fallback: if running from repo, use $PSScriptRoot
        $repoRoot = $script:EndstateRoot
        
        # Verify this is actually a repo root by checking for cli.ps1
        $cliPath = Join-Path $repoRoot "cli.ps1"
        if (-not (Test-Path $cliPath)) {
            # Not in repo and no configured repo root - return null
            return $null
        }
    }
    
    return Join-Path $repoRoot "cli.ps1"
}

function Resolve-ManifestPath {
    <#
    .SYNOPSIS
        Resolve profile name or file path to manifest path.
    .DESCRIPTION
        Accepts either:
        1. A full or relative file path (contains path separator, has .json/.jsonc extension, or exists as file)
           -> Returns the path as-is (resolved to absolute if relative)
        2. A simple profile name
           -> Resolves under repo manifests/ directory
        
        Uses repo root from:
        1. $env:ENDSTATE_ROOT (if set)
        2. Persisted repo-root.txt
        3. $script:EndstateRoot (fallback for in-repo execution)
    #>
    param([string]$ProfileName)
    
    # Check if ProfileName is actually a file path
    $isFilePath = $false
    
    # Heuristic 1: Contains path separator
    if ($ProfileName -match '[/\\]') {
        $isFilePath = $true
    }
    # Heuristic 2: Has .json/.jsonc/.json5 extension
    elseif ($ProfileName -match '\.(jsonc?|json5)$') {
        $isFilePath = $true
    }
    # Heuristic 3: File exists at this path
    elseif (Test-Path -LiteralPath $ProfileName -PathType Leaf) {
        $isFilePath = $true
    }
    
    # If it's a file path, resolve to absolute and return
    if ($isFilePath) {
        if ([System.IO.Path]::IsPathRooted($ProfileName)) {
            return $ProfileName
        } else {
            return $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($ProfileName)
        }
    }
    
    # Otherwise, treat as profile name and resolve under repo manifests/
    # Try to get configured repo root
    $repoRoot = Get-RepoRootPath
    
    if (-not $repoRoot) {
        # Fallback: if running from repo, use $PSScriptRoot
        $repoRoot = $script:EndstateRoot
        
        # Verify this is actually a repo root
        $manifestsDir = Join-Path $repoRoot "manifests"
        if (-not (Test-Path $manifestsDir)) {
            Write-Host "[ERROR] Repo root not configured. Run 'endstate bootstrap -RepoRoot <path>' or set ENDSTATE_ROOT." -ForegroundColor Red
            return $null
        }
    }
    
    $manifestPath = Join-Path $repoRoot "manifests\$ProfileName.jsonc"
    return $manifestPath
}

function Invoke-ProvisioningCli {
    param(
        [string]$ProvisioningCommand,
        [hashtable]$Arguments
    )
    
    # Resolve provisioning CLI path (lazy evaluation)
    $cliPath = Get-ProvisioningCliPath
    
    # Check if repo root resolution failed (cliPath is null)
    if (-not $cliPath) {
        Write-Host "[ERROR] Repo root not configured. Cannot locate provisioning CLI." -ForegroundColor Red
        Write-Host "        Run 'endstate bootstrap -RepoRoot <path>' or set ENDSTATE_ROOT environment variable." -ForegroundColor Yellow
        Write-Host "" -ForegroundColor Yellow
        Write-Host "Example:" -ForegroundColor Cyan
        Write-Host "  endstate bootstrap -RepoRoot C:\path\to\endstate" -ForegroundColor Cyan
        return @{ Success = $false; ExitCode = 1; Error = "Repo root not configured" }
    }
    
    if (-not (Test-Path $cliPath)) {
        Write-Host "[ERROR] Provisioning CLI not found: $cliPath" -ForegroundColor Red
        Write-Host "        Verify repo root is configured correctly." -ForegroundColor Yellow
        return @{ Success = $false; ExitCode = 1; Error = "Provisioning CLI not found" }
    }
    
    # Emit stable wrapper line via Write-Output for testability
    Write-Output "[endstate] Delegating to provisioning subsystem..."
    Write-Host ""
    
    $params = @{ Command = $ProvisioningCommand }
    
    foreach ($key in $Arguments.Keys) {
        if ($null -ne $Arguments[$key]) {
            $params[$key] = $Arguments[$key]
        }
    }
    
    & $cliPath @params
    
    $exitCode = if ($LASTEXITCODE) { $LASTEXITCODE } else { 0 }
    return @{ Success = ($exitCode -eq 0); ExitCode = $exitCode }
}

function Invoke-ApplyCore {
    param(
        [string]$ManifestPath,
        [bool]$IsDryRun,
        [bool]$IsOnlyApps,
        [switch]$SkipStateWrite
    )
    
    Write-Output "[endstate] Apply: reading manifest $ManifestPath"
    $manifest = Read-Manifest -Path $ManifestPath
    
    if (-not $manifest) {
        return @{ Success = $false; ExitCode = 1; Error = "Failed to read manifest" }
    }
    
    Write-Output "[endstate] Apply: installing apps"
    
    $installed = 0
    $skipped = 0
    $failed = 0
    $upgraded = 0
    $alreadyInstalled = 0
    $skippedFiltered = 0
    $timestampUtc = (Get-Date).ToUniversalTime().ToString("o")
    
    # Track per-app items for structured output
    $items = @()
    
    foreach ($app in $manifest.apps) {
        $driver = Get-AppDriver -App $app
        $appDisplayId = if ($driver -eq 'winget') { Get-AppWingetId -App $app } else { $app.id }
        
        if (-not $appDisplayId) {
            Write-Host "  [SKIP] $($app.id) - no installable ref for driver '$driver'" -ForegroundColor Yellow
            $skipped++
            $skippedFiltered++
            $items += @{
                id = $app.id
                driver = $driver
                status = "skipped"
                reason = "no_installable_ref"
                message = "No installable ref for driver '$driver'"
            }
            continue
        }
        
        # Check if already installed using driver abstraction
        $installStatus = Test-AppInstalledWithDriver -App $app
        
        if ($installStatus.Installed) {
            # Check version constraint if present
            $versionConstraint = Parse-VersionConstraint -Constraint $app.version
            
            if ($versionConstraint) {
                $versionCheck = Test-VersionConstraint -InstalledVersion $installStatus.Version -Constraint $versionConstraint
                
                if (-not $versionCheck.Satisfied) {
                    # Version mismatch - attempt upgrade for winget, report for custom
                    if ($driver -eq 'winget') {
                        if ($IsDryRun) {
                            Write-Host "  [PLAN] $appDisplayId - would upgrade ($($versionCheck.Reason))" -ForegroundColor Cyan
                            $upgraded++
                            $items += @{
                                id = $appDisplayId
                                driver = $driver
                                status = "ok"
                                reason = "would_upgrade"
                                message = "Would upgrade: $($versionCheck.Reason)"
                            }
                        } else {
                            Write-Host "  [UPGRADE] $appDisplayId ($($versionCheck.Reason))" -ForegroundColor Yellow
                            $result = Install-AppWithDriver -App $app -DryRun $false -IsUpgrade $true
                            if ($result.Success) {
                                $upgraded++
                                $items += @{
                                    id = $appDisplayId
                                    driver = $driver
                                    status = "ok"
                                    reason = "upgraded"
                                    message = "Upgraded: $($versionCheck.Reason)"
                                }
                            } else {
                                Write-Host "    [WARN] Upgrade may have issues: $($result.Error)" -ForegroundColor Yellow
                                $upgraded++
                                $items += @{
                                    id = $appDisplayId
                                    driver = $driver
                                    status = "ok"
                                    reason = "upgraded"
                                    message = "Upgraded with warnings: $($result.Error)"
                                }
                            }
                        }
                    } else {
                        Write-Host "  [MANUAL] $appDisplayId - version mismatch, manual intervention needed ($($versionCheck.Reason))" -ForegroundColor Yellow
                        $failed++
                        $items += @{
                            id = $appDisplayId
                            driver = $driver
                            status = "failed"
                            reason = "version_mismatch"
                            message = "Version mismatch, manual intervention needed: $($versionCheck.Reason)"
                        }
                    }
                    continue
                }
            }
            
            Write-Host "  [SKIP] $appDisplayId - already installed" -ForegroundColor DarkGray
            $skipped++
            $alreadyInstalled++
            $items += @{
                id = $appDisplayId
                driver = $driver
                status = "skipped"
                reason = "already_installed"
                message = "Already installed"
            }
            continue
        }
        
        # Not installed - install it
        if ($IsDryRun) {
            Write-Host "  [PLAN] $appDisplayId - would install (driver: $driver)" -ForegroundColor Cyan
            $installed++
            $items += @{
                id = $appDisplayId
                driver = $driver
                status = "ok"
                reason = "would_install"
                message = "Would install"
            }
        } else {
            Write-Host "  [INSTALL] $appDisplayId (driver: $driver)" -ForegroundColor Green
            $result = Install-AppWithDriver -App $app -DryRun $false -IsUpgrade $false
            
            if ($result.Success) {
                $installed++
                $items += @{
                    id = $appDisplayId
                    driver = $driver
                    status = "ok"
                    reason = "installed"
                    message = "Installed successfully"
                }
            } else {
                Write-Host "    [ERROR] Failed to install: $($result.Error)" -ForegroundColor Red
                $failed++
                $items += @{
                    id = $appDisplayId
                    driver = $driver
                    status = "failed"
                    reason = "install_failed"
                    message = $result.Error
                }
            }
        }
    }
    
    Write-Host ""
    Write-Host "[endstate] Apply: Summary" -ForegroundColor Cyan
    Write-Host "  Installed: $installed"
    Write-Host "  Upgraded:  $upgraded"
    Write-Host "  Skipped:   $skipped"
    if ($failed -gt 0) {
        Write-Host "  Failed:    $failed" -ForegroundColor Red
    }
    
    # Write state for apply (unless dry-run or skipped)
    if (-not $IsDryRun -and -not $SkipStateWrite) {
        $manifestHash = Get-ManifestHash -Path $ManifestPath
        $state = Read-EndstateState
        if (-not $state) {
            $state = New-EndstateState
        }
        
        # Convert PSCustomObject to hashtable if needed
        if ($state -is [PSCustomObject]) {
            $stateHash = @{}
            $state.PSObject.Properties | ForEach-Object { $stateHash[$_.Name] = $_.Value }
            $state = $stateHash
        }
        
        $state.lastApplied = @{
            manifestPath = $ManifestPath
            manifestHash = $manifestHash
            timestampUtc = $timestampUtc
        }
        
        Write-EndstateStateAtomic -State $state | Out-Null
    }
    
    # Run verify unless -OnlyApps or -DryRun
    $verifyResult = $null
    if (-not $IsOnlyApps -and -not $IsDryRun) {
        Write-Host ""
        $verifyResult = Invoke-VerifyCore -ManifestPath $ManifestPath -SkipStateWrite:$SkipStateWrite
    }
    
    Write-Output "[endstate] Apply: completed ExitCode=$(if ($failed -gt 0) { 1 } else { 0 })"
    
    # Build structured counts
    $counts = @{
        total = $manifest.apps.Count
        installed = $installed
        alreadyInstalled = $alreadyInstalled
        skippedFiltered = $skippedFiltered
        failed = $failed
    }
    
    # DryRun always succeeds; otherwise propagate verify result if run
    if ($IsDryRun) {
        return @{ Success = $true; ExitCode = 0; Installed = $installed; Upgraded = $upgraded; Skipped = $skipped; Failed = $failed; Counts = $counts; Items = $items }
    }
    
    if ($verifyResult) {
        return @{ 
            Success = $verifyResult.Success
            ExitCode = $verifyResult.ExitCode
            Installed = $installed
            Upgraded = $upgraded
            Skipped = $skipped
            Failed = $failed
            Counts = $counts
            Items = $items
            VerifyResult = $verifyResult
        }
    }
    
    return @{ Success = ($failed -eq 0); ExitCode = (if ($failed -gt 0) { 1 } else { 0 }); Installed = $installed; Upgraded = $upgraded; Skipped = $skipped; Failed = $failed; Counts = $counts; Items = $items }
}

function Get-InstalledApps {
    # Get list of installed apps via winget (or mock script for testing)
    if ($script:WingetScript) {
        $wingetOutput = & pwsh -NoProfile -File $script:WingetScript list 2>$null
    } else {
        $wingetOutput = & winget list --accept-source-agreements 2>$null
    }
    return $wingetOutput
}

function Test-AppInstalled {
    param([string]$WingetId)
    
    $installedApps = Get-InstalledApps
    foreach ($line in $installedApps) {
        if ($line -match [regex]::Escape($WingetId)) {
            return $true
        }
    }
    return $false
}

#region Driver Abstraction (Bundle C)

<#
.SYNOPSIS
    Parse version constraint string.
.DESCRIPTION
    Supports:
    - Exact match: "1.2.3"
    - Minimum: ">=1.2.3"
    Returns hashtable with Type (exact|minimum) and Version.
#>
function Parse-VersionConstraint {
    param([string]$Constraint)
    
    if (-not $Constraint) {
        return $null
    }
    
    $Constraint = $Constraint.Trim()
    
    if ($Constraint -match '^>=(.+)$') {
        return @{
            Type = 'minimum'
            Version = $Matches[1].Trim()
        }
    } else {
        return @{
            Type = 'exact'
            Version = $Constraint
        }
    }
}

<#
.SYNOPSIS
    Compare two version strings.
.DESCRIPTION
    Returns:
    - -1 if $Version1 < $Version2
    - 0 if $Version1 == $Version2
    - 1 if $Version1 > $Version2
    Handles dotted version strings (e.g., 1.2.3, 2.43.0).
#>
function Compare-Versions {
    param(
        [string]$Version1,
        [string]$Version2
    )
    
    if (-not $Version1 -or -not $Version2) {
        return $null
    }
    
    # Split into parts and compare numerically
    $parts1 = $Version1 -split '\.'
    $parts2 = $Version2 -split '\.'
    
    $maxParts = [Math]::Max($parts1.Count, $parts2.Count)
    
    for ($i = 0; $i -lt $maxParts; $i++) {
        $p1 = if ($i -lt $parts1.Count) { 
            $num = 0
            if ([int]::TryParse($parts1[$i], [ref]$num)) { $num } else { 0 }
        } else { 0 }
        
        $p2 = if ($i -lt $parts2.Count) { 
            $num = 0
            if ([int]::TryParse($parts2[$i], [ref]$num)) { $num } else { 0 }
        } else { 0 }
        
        if ($p1 -lt $p2) { return -1 }
        if ($p1 -gt $p2) { return 1 }
    }
    
    return 0
}

<#
.SYNOPSIS
    Test if installed version satisfies constraint.
.DESCRIPTION
    Returns hashtable with:
    - Satisfied: $true/$false
    - Reason: explanation string
#>
function Test-VersionConstraint {
    param(
        [string]$InstalledVersion,
        [hashtable]$Constraint
    )
    
    if (-not $Constraint) {
        return @{ Satisfied = $true; Reason = 'no constraint' }
    }
    
    if (-not $InstalledVersion -or $InstalledVersion -eq $true) {
        # Unknown version - fail by default (CI-safe)
        return @{ Satisfied = $false; Reason = 'version unknown' }
    }
    
    $cmp = Compare-Versions -Version1 $InstalledVersion -Version2 $Constraint.Version
    
    if ($null -eq $cmp) {
        return @{ Satisfied = $false; Reason = 'version comparison failed' }
    }
    
    switch ($Constraint.Type) {
        'exact' {
            if ($cmp -eq 0) {
                return @{ Satisfied = $true; Reason = "exact match $InstalledVersion" }
            } else {
                return @{ Satisfied = $false; Reason = "expected $($Constraint.Version), got $InstalledVersion" }
            }
        }
        'minimum' {
            if ($cmp -ge 0) {
                return @{ Satisfied = $true; Reason = "$InstalledVersion >= $($Constraint.Version)" }
            } else {
                return @{ Satisfied = $false; Reason = "$InstalledVersion < $($Constraint.Version)" }
            }
        }
        default {
            return @{ Satisfied = $false; Reason = "unknown constraint type: $($Constraint.Type)" }
        }
    }
}

<#
.SYNOPSIS
    Get driver for an app entry.
.DESCRIPTION
    Returns driver name: 'winget' (default) or 'custom'.
#>
function Get-AppDriver {
    param([PSObject]$App)
    
    if ($App.driver) {
        return $App.driver.ToLower()
    }
    return 'winget'
}

<#
.SYNOPSIS
    Get winget ID for an app.
.DESCRIPTION
    Supports both old format (refs.windows) and new format (id with driver=winget).
#>
function Get-AppWingetId {
    param([PSObject]$App)
    
    # Prefer refs.windows for backward compatibility
    if ($App.refs -and $App.refs.windows) {
        return $App.refs.windows
    }
    
    # Fallback: if driver is winget and no refs, use id as winget id
    $driver = Get-AppDriver -App $App
    if ($driver -eq 'winget' -and $App.id) {
        return $App.id
    }
    
    return $null
}

<#
.SYNOPSIS
    Validate custom driver install script path.
.DESCRIPTION
    Security: Only allow scripts under repo root.
    Returns $true if path is safe, $false otherwise.
#>
function Test-CustomScriptPathSafe {
    param([string]$ScriptPath)
    
    if (-not $ScriptPath) {
        return $false
    }
    
    # Resolve to absolute path
    $absolutePath = if ([System.IO.Path]::IsPathRooted($ScriptPath)) {
        $ScriptPath
    } else {
        Join-Path $script:EndstateRoot $ScriptPath
    }
    
    try {
        $resolvedPath = [System.IO.Path]::GetFullPath($absolutePath)
        $repoRoot = [System.IO.Path]::GetFullPath($script:EndstateRoot)
        
        # Check if resolved path starts with repo root (prevent path traversal)
        return $resolvedPath.StartsWith($repoRoot, [System.StringComparison]::OrdinalIgnoreCase)
    } catch {
        return $false
    }
}

<#
.SYNOPSIS
    Test if custom app is installed using detect configuration.
.DESCRIPTION
    Supports detect types:
    - file: check if file exists at path
    - registry: check if registry key/value exists (optional)
    Returns hashtable with Installed and Version (if detectable).
#>
function Test-CustomAppInstalled {
    param([PSObject]$CustomConfig)
    
    if (-not $CustomConfig -or -not $CustomConfig.detect) {
        return @{ Installed = $false; Version = $null; Error = 'no detect config' }
    }
    
    $detect = $CustomConfig.detect
    
    switch ($detect.type) {
        'file' {
            if (-not $detect.path) {
                return @{ Installed = $false; Version = $null; Error = 'file detect missing path' }
            }
            
            # Expand environment variables in path
            $expandedPath = [Environment]::ExpandEnvironmentVariables($detect.path)
            $exists = Test-Path $expandedPath
            
            return @{ 
                Installed = $exists
                Version = $null  # File detection doesn't provide version
                DetectPath = $expandedPath
            }
        }
        'registry' {
            if (-not $detect.key) {
                return @{ Installed = $false; Version = $null; Error = 'registry detect missing key' }
            }
            
            try {
                $regValue = Get-ItemProperty -Path $detect.key -Name $detect.value -ErrorAction SilentlyContinue
                if ($regValue) {
                    $version = if ($detect.value) { $regValue.$($detect.value) } else { $null }
                    return @{ Installed = $true; Version = $version }
                }
                return @{ Installed = $false; Version = $null }
            } catch {
                return @{ Installed = $false; Version = $null; Error = $_.ToString() }
            }
        }
        default {
            return @{ Installed = $false; Version = $null; Error = "unknown detect type: $($detect.type)" }
        }
    }
}

<#
.SYNOPSIS
    Install custom app by running install script.
.DESCRIPTION
    Runs the installScript from custom config.
    Returns hashtable with Success, ExitCode, Output.
#>
function Install-CustomApp {
    param(
        [PSObject]$App,
        [bool]$DryRun = $false
    )
    
    $customConfig = $App.custom
    if (-not $customConfig -or -not $customConfig.installScript) {
        return @{ Success = $false; ExitCode = 1; Error = 'no installScript defined' }
    }
    
    $scriptPath = $customConfig.installScript
    
    # Security check: script must be under repo root
    if (-not (Test-CustomScriptPathSafe -ScriptPath $scriptPath)) {
        Write-Host "    [SECURITY] Install script path rejected (must be under repo root): $scriptPath" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = 'script path outside repo root' }
    }
    
    # Resolve to absolute path
    $absoluteScript = if ([System.IO.Path]::IsPathRooted($scriptPath)) {
        $scriptPath
    } else {
        Join-Path $script:EndstateRoot $scriptPath
    }
    
    if (-not (Test-Path $absoluteScript)) {
        return @{ Success = $false; ExitCode = 1; Error = "install script not found: $absoluteScript" }
    }
    
    if ($DryRun) {
        Write-Output "[endstate] CustomDriver: would run $absoluteScript"
        return @{ Success = $true; ExitCode = 0; DryRun = $true }
    }
    
    Write-Output "[endstate] CustomDriver: running $absoluteScript"
    
    try {
        $output = & pwsh -NoProfile -File $absoluteScript 2>&1
        $exitCode = if ($LASTEXITCODE) { $LASTEXITCODE } else { 0 }
        
        return @{ 
            Success = ($exitCode -eq 0)
            ExitCode = $exitCode
            Output = $output
        }
    } catch {
        return @{ Success = $false; ExitCode = 1; Error = $_.ToString() }
    }
}

<#
.SYNOPSIS
    Driver interface: Test if app is installed.
.DESCRIPTION
    Dispatches to appropriate driver based on app config.
    Returns hashtable with Installed, Version, Driver.
#>
function Test-AppInstalledWithDriver {
    param([PSObject]$App)
    
    $driver = Get-AppDriver -App $App
    
    switch ($driver) {
        'winget' {
            $wingetId = Get-AppWingetId -App $App
            if (-not $wingetId) {
                return @{ Installed = $false; Version = $null; Driver = 'winget'; Error = 'no winget id' }
            }
            
            $installedMap = Get-InstalledAppsMap
            $isInstalled = $installedMap.ContainsKey($wingetId)
            $version = if ($isInstalled) { $installedMap[$wingetId] } else { $null }
            
            # Handle version being $true (installed but version unknown)
            if ($version -eq $true) { $version = $null }
            
            return @{ 
                Installed = $isInstalled
                Version = $version
                Driver = 'winget'
                WingetId = $wingetId
            }
        }
        'custom' {
            $result = Test-CustomAppInstalled -CustomConfig $App.custom
            $result.Driver = 'custom'
            return $result
        }
        default {
            return @{ Installed = $false; Version = $null; Driver = $driver; Error = "unknown driver: $driver" }
        }
    }
}

<#
.SYNOPSIS
    Driver interface: Install app.
.DESCRIPTION
    Dispatches to appropriate driver based on app config.
    Returns hashtable with Success, ExitCode, Action.
#>
function Install-AppWithDriver {
    param(
        [PSObject]$App,
        [bool]$DryRun = $false,
        [bool]$IsUpgrade = $false
    )
    
    $driver = Get-AppDriver -App $App
    
    switch ($driver) {
        'winget' {
            $wingetId = Get-AppWingetId -App $App
            if (-not $wingetId) {
                return @{ Success = $false; ExitCode = 1; Error = 'no winget id'; Action = 'none' }
            }
            
            if ($DryRun) {
                $action = if ($IsUpgrade) { 'would upgrade' } else { 'would install' }
                return @{ Success = $true; ExitCode = 0; Action = $action; DryRun = $true }
            }
            
            try {
                $action = if ($IsUpgrade) { 'upgrade' } else { 'install' }
                
                if ($script:WingetScript) {
                    & pwsh -NoProfile -File $script:WingetScript $action --id $wingetId 2>&1 | Out-Null
                } else {
                    if ($IsUpgrade) {
                        & winget upgrade --id $wingetId --accept-source-agreements --accept-package-agreements -e 2>&1 | Out-Null
                    } else {
                        & winget install --id $wingetId --accept-source-agreements --accept-package-agreements -e 2>&1 | Out-Null
                    }
                }
                
                $exitCode = if ($LASTEXITCODE) { $LASTEXITCODE } else { 0 }
                return @{ Success = ($exitCode -eq 0); ExitCode = $exitCode; Action = $action }
            } catch {
                return @{ Success = $false; ExitCode = 1; Error = $_.ToString(); Action = 'failed' }
            }
        }
        'custom' {
            if ($IsUpgrade) {
                # Custom driver doesn't support upgrade - report manual intervention needed
                return @{ Success = $false; ExitCode = 1; Action = 'manual_upgrade_needed'; Error = 'custom driver does not support upgrade' }
            }
            return Install-CustomApp -App $App -DryRun $DryRun
        }
        default {
            return @{ Success = $false; ExitCode = 1; Error = "unknown driver: $driver"; Action = 'none' }
        }
    }
}

#endregion Driver Abstraction (Bundle C)

function Read-Manifest {
    param([string]$Path)
    
    if (-not (Test-Path $Path)) {
        Write-Host "[ERROR] Manifest not found: $Path" -ForegroundColor Red
        return $null
    }
    
    $content = Get-Content -Path $Path -Raw
    # Strip JSONC comments for parsing
    $jsonContent = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
    
    try {
        return $jsonContent | ConvertFrom-Json
    } catch {
        Write-Host "[ERROR] Failed to parse manifest: $_" -ForegroundColor Red
        return $null
    }
}

function Write-ExampleManifest {
    param([string]$Path)
    
    $example = @{
        version = 1
        name = "example"
        apps = @(
            @{ id = "7zip-7zip"; refs = @{ windows = "7zip.7zip" } }
            @{ id = "git-git"; refs = @{ windows = "Git.Git" } }
            @{ id = "microsoft-powershell"; refs = @{ windows = "Microsoft.PowerShell" } }
            @{ id = "microsoft-windowsterminal"; refs = @{ windows = "Microsoft.WindowsTerminal" } }
            @{ id = "videolan-vlc"; refs = @{ windows = "VideoLAN.VLC" } }
        )
        restore = @()
        verify = @()
    }
    
    $jsonContent = $example | ConvertTo-Json -Depth 10
    
    # Add header comment
    $header = @"
{
  // Deterministic example manifest
  // This file is committed and used for automated tests
  // Do NOT add machine-specific data or timestamps

"@
    
    # Convert to JSONC format with comments
    $jsoncContent = $header + ($jsonContent.TrimStart('{'))
    
    $parentDir = Split-Path -Parent $Path
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    Set-Content -Path $Path -Value $jsoncContent
    return $Path
}

#region Bundle D - Sanitization Helpers

<#
.SYNOPSIS
    Patterns for fields that look like secrets or local paths.
#>
$script:SensitiveFieldPatterns = @(
    'password', 'secret', 'token', 'apikey', 'api_key', 'api-key',
    'credential', 'auth', 'private', 'key'
)

$script:LocalPathPatterns = @(
    '^[A-Za-z]:\\Users\\',
    '^C:\\Users\\',
    '^/home/',
    '^/Users/',
    '\$env:USERPROFILE',
    '\$env:LOCALAPPDATA',
    '\$env:APPDATA'
)

function Test-IsExamplesDirectory {
    param([string]$Path)
    
    if (-not $Path) { return $false }
    
    try {
        $resolvedPath = [System.IO.Path]::GetFullPath($Path)
        $examplesDir = [System.IO.Path]::GetFullPath($script:ExamplesManifestsDir)
        return $resolvedPath.StartsWith($examplesDir, [System.StringComparison]::OrdinalIgnoreCase)
    } catch {
        return $false
    }
}

function Test-PathLooksLikeSecret {
    param([string]$Value)
    
    if (-not $Value) { return $false }
    
    $lowerValue = $Value.ToLower()
    foreach ($pattern in $script:SensitiveFieldPatterns) {
        if ($lowerValue -match $pattern) {
            return $true
        }
    }
    return $false
}

function Test-PathLooksLikeLocalPath {
    param([string]$Value)
    
    if (-not $Value) { return $false }
    
    foreach ($pattern in $script:LocalPathPatterns) {
        if ($Value -match $pattern) {
            return $true
        }
    }
    return $false
}

function Invoke-SanitizeManifest {
    <#
    .SYNOPSIS
        Sanitize a manifest by removing machine-specific fields, secrets, and local paths.
    .DESCRIPTION
        - Removes 'captured' timestamp
        - Removes 'machine' field if present
        - Removes fields that look like secrets or local paths (best-effort)
        - Sorts apps array by id for deterministic output
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest,
        
        [Parameter(Mandatory = $false)]
        [string]$NewName
    )
    
    $sanitized = @{}
    
    # Copy version
    if ($Manifest.version) {
        $sanitized.version = $Manifest.version
    } else {
        $sanitized.version = 1
    }
    
    # Set name (use provided or strip machine-specific parts)
    if ($NewName) {
        $sanitized.name = $NewName
    } elseif ($Manifest.name) {
        # Remove machine name from existing name if present
        $name = $Manifest.name -replace $env:COMPUTERNAME, '' -replace '--', '-' -replace '^-|-$', ''
        $sanitized.name = if ($name) { $name.ToLower() } else { 'sanitized' }
    } else {
        $sanitized.name = 'sanitized'
    }
    
    # DO NOT copy 'captured' timestamp - this is machine-specific
    # DO NOT copy 'machine' field if present
    
    # Copy includes if present (but not machine-specific ones)
    if ($Manifest.includes) {
        $sanitized.includes = @($Manifest.includes | Where-Object { 
            $_ -notmatch 'local/' -and $_ -notmatch $env:COMPUTERNAME
        })
        if ($sanitized.includes.Count -eq 0) {
            $sanitized.Remove('includes')
        }
    }
    
    # Sanitize and sort apps
    $sanitizedApps = @()
    if ($Manifest.apps) {
        foreach ($app in $Manifest.apps) {
            $sanitizedApp = @{}
            
            # Copy safe fields
            if ($app.id) { $sanitizedApp.id = $app.id }
            if ($app.refs) { $sanitizedApp.refs = $app.refs }
            if ($app.driver) { $sanitizedApp.driver = $app.driver }
            if ($app.version) { $sanitizedApp.version = $app.version }
            
            # Copy custom config but sanitize paths
            if ($app.custom) {
                $sanitizedCustom = @{}
                if ($app.custom.installScript -and -not (Test-PathLooksLikeLocalPath -Value $app.custom.installScript)) {
                    $sanitizedCustom.installScript = $app.custom.installScript
                }
                if ($app.custom.detect) {
                    $sanitizedDetect = @{}
                    if ($app.custom.detect.type) { $sanitizedDetect.type = $app.custom.detect.type }
                    # Only include path if it's not a user-specific path
                    if ($app.custom.detect.path -and -not (Test-PathLooksLikeLocalPath -Value $app.custom.detect.path)) {
                        $sanitizedDetect.path = $app.custom.detect.path
                    }
                    if ($app.custom.detect.key) { $sanitizedDetect.key = $app.custom.detect.key }
                    if ($app.custom.detect.value) { $sanitizedDetect.value = $app.custom.detect.value }
                    if ($sanitizedDetect.Count -gt 0) {
                        $sanitizedCustom.detect = $sanitizedDetect
                    }
                }
                if ($sanitizedCustom.Count -gt 0) {
                    $sanitizedApp.custom = $sanitizedCustom
                }
            }
            
            # Skip apps that look like they contain secrets
            $skipApp = $false
            foreach ($key in $app.Keys) {
                if (Test-PathLooksLikeSecret -Value $key) {
                    $skipApp = $true
                    break
                }
            }
            
            if (-not $skipApp -and $sanitizedApp.Count -gt 0) {
                $sanitizedApps += $sanitizedApp
            }
        }
        
        # Sort apps by id for deterministic output
        $sanitized.apps = @($sanitizedApps | Sort-Object -Property { $_.id })
    } else {
        $sanitized.apps = @()
    }
    
    # Copy restore and verify arrays (empty by default for examples)
    # Use explicit array creation to ensure non-null even when empty
    $sanitized.restore = [System.Collections.ArrayList]::new()
    $sanitized.verify = [System.Collections.ArrayList]::new()
    
    return $sanitized
}

#endregion Bundle D - Sanitization Helpers

function Invoke-CaptureCore {
    <#
    .SYNOPSIS
        Core capture logic. Returns structured result only - no stream output.
    #>
    param(
        [string]$OutputPath,
        [bool]$IsExample,
        [bool]$IsSanitize,
        [string]$ManifestName,
        [string]$CustomExamplesDir,
        [bool]$ForceOverwrite
    )
    
    # Determine effective examples directory
    $effectiveExamplesDir = if ($CustomExamplesDir) {
        $CustomExamplesDir
    } else {
        $script:ExamplesManifestsDir
    }
    
    # Legacy -Example flag: generate static example manifest
    if ($IsExample -and -not $IsSanitize) {
        $examplePath = if ($OutputPath) { $OutputPath } else { Join-Path $script:EndstateRoot "manifests\example.jsonc" }
        $null = Write-ExampleManifest -Path $examplePath
        Write-Host "[endstate] Capture: example manifest written to $examplePath" -ForegroundColor Green
        return @{ Success = $true; OutputPath = $examplePath; Sanitized = $false; IsExample = $true }
    }
    
    # Determine output path based on flags
    $outPath = $null
    $isExamplesTarget = $false
    
    if ($OutputPath) {
        # Explicit -Out overrides everything
        $outPath = $OutputPath
        $isExamplesTarget = Test-IsExamplesDirectory -Path $outPath
    } elseif ($IsSanitize) {
        # -Sanitize: output to examples directory
        $fileName = if ($ManifestName) { 
            "$($ManifestName.ToLower() -replace '\s+', '-').jsonc" 
        } else { 
            "sanitized.jsonc" 
        }
        
        if (-not (Test-Path $effectiveExamplesDir)) {
            New-Item -ItemType Directory -Path $effectiveExamplesDir -Force | Out-Null
        }
        $outPath = Join-Path $effectiveExamplesDir $fileName
        $isExamplesTarget = $true
    } else {
        # Default: local/<machine>.jsonc (gitignored)
        $machineName = $env:COMPUTERNAME.ToLower()
        if (-not (Test-Path $script:LocalManifestsDir)) {
            New-Item -ItemType Directory -Path $script:LocalManifestsDir -Force | Out-Null
        }
        $outPath = Join-Path $script:LocalManifestsDir "$machineName.jsonc"
    }
    
    # GUARDRAIL: Block non-sanitized writes to examples directory
    if ($isExamplesTarget -and -not $IsSanitize) {
        Write-Host "[ERROR] Cannot write non-sanitized capture to examples directory." -ForegroundColor Red
        Write-Host "        Use -Sanitize flag or choose a different output path." -ForegroundColor Yellow
        return @{ Success = $false; Error = "Non-sanitized write to examples directory blocked"; Blocked = $true }
    }
    
    # GUARDRAIL: Prevent overwrite of existing example manifests without -Force
    if ($isExamplesTarget -and (Test-Path $outPath) -and -not $ForceOverwrite) {
        Write-Host "[ERROR] Example manifest already exists: $outPath" -ForegroundColor Red
        Write-Host "        Use -Force to overwrite existing example manifests." -ForegroundColor Yellow
        return @{ Success = $false; Error = "Example manifest exists, use -Force to overwrite"; ExitCode = 1; Blocked = $true }
    }
    
    if ($IsSanitize) {
        Write-Host "[endstate] Capture: sanitization enabled" -ForegroundColor Cyan
        
        # First capture to a temp location
        $tempDir = Join-Path $env:TEMP "endstate-capture-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
        $tempPath = Join-Path $tempDir "raw-capture.jsonc"
        
        # Delegate to provisioning CLI for raw capture
        $cliArgs = @{ OutManifest = $tempPath }
        $captureResult = Invoke-ProvisioningCli -ProvisioningCommand "capture" -Arguments $cliArgs
        
        if (-not (Test-Path $tempPath)) {
            Write-Host "[ERROR] Raw capture failed - no output file generated" -ForegroundColor Red
            return @{ Success = $false; Error = "Raw capture failed" }
        }
        
        # Read and sanitize the manifest
        $rawContent = Get-Content -Path $tempPath -Raw
        $jsonContent = $rawContent -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
        $rawManifest = $jsonContent | ConvertFrom-Json
        
        # Convert PSCustomObject to hashtable for sanitization
        $manifestHash = @{}
        $rawManifest.PSObject.Properties | ForEach-Object { 
            $manifestHash[$_.Name] = $_.Value 
        }
        
        # Sanitize
        $sanitizedManifest = Invoke-SanitizeManifest -Manifest $manifestHash -NewName $ManifestName
        
        # Write sanitized manifest
        $parentDir = Split-Path -Parent $outPath
        if ($parentDir -and -not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        
        # Add header comment for sanitized manifests
        $header = @"
{
  // Sanitized example manifest
  // Generated via: endstate capture -Sanitize
  // This file is safe to commit - no machine-specific data or timestamps

"@
        $jsonBody = ($sanitizedManifest | ConvertTo-Json -Depth 10).TrimStart('{')
        $jsoncContent = $header + $jsonBody
        
        Set-Content -Path $outPath -Value $jsoncContent -Encoding UTF8
        
        # Cleanup temp
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
        
        Write-Host "[endstate] Capture: sanitized manifest written to $outPath" -ForegroundColor Green
        
        $appsIncluded = @()
        if ($sanitizedManifest.apps) {
            $appsIncluded = @($sanitizedManifest.apps | ForEach-Object {
                $appEntry = @{ id = if ($_.refs -and $_.refs.windows) { $_.refs.windows } else { $_.id } }
                # Determine source from _source metadata or infer from ID pattern
                if ($_._source) {
                    $appEntry.source = $_._source
                } else {
                    $appEntry.source = "winget"
                }
                $appEntry
            })
        }
        
        return @{ 
            Success = $true
            OutputPath = $outPath
            Sanitized = $true
            Counts = @{
                totalFound = $sanitizedManifest.apps.Count
                included = $sanitizedManifest.apps.Count
                skipped = 0
                filteredRuntimes = 0
                filteredStoreApps = 0
                sensitiveExcludedCount = 0
            }
            AppsIncluded = $appsIncluded
        }
    }
    
    # Non-sanitized capture: delegate to provisioning CLI
    $cliArgs = @{ OutManifest = $outPath }
    $null = Invoke-ProvisioningCli -ProvisioningCommand "capture" -Arguments $cliArgs
    
    # Read the generated manifest to get app count and list
    $result = @{ 
        Success = $true
        OutputPath = $outPath
        Sanitized = $false
        Counts = @{
            totalFound = 0
            included = 0
            skipped = 0
            filteredRuntimes = 0
            filteredStoreApps = 0
            sensitiveExcludedCount = 0
        }
        AppsIncluded = @()
    }
    
    if (Test-Path $outPath) {
        try {
            $rawContent = Get-Content -Path $outPath -Raw
            # Strip JSONC comments for parsing
            $jsonContent = $rawContent -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
            $manifest = $jsonContent | ConvertFrom-Json
            
            if ($manifest.apps) {
                $result.Counts.included = $manifest.apps.Count
                $result.Counts.totalFound = $manifest.apps.Count
                
                # Extract app IDs and sources for the GUI
                $result.AppsIncluded = @($manifest.apps | ForEach-Object {
                    $appEntry = @{ id = if ($_.refs -and $_.refs.windows) { $_.refs.windows } else { $_.id } }
                    # Determine source from _source metadata or infer from ID pattern
                    if ($_._source) {
                        $appEntry.source = $_._source
                    } else {
                        $appEntry.source = "winget"
                    }
                    $appEntry
                })
            }
        } catch {
            # If we can't read the manifest, still return success but without counts
            Write-Verbose "Could not read manifest for app counts: $_"
        }
    }
    
    return $result
}

function Invoke-VerifyCore {
    <#
    .SYNOPSIS
        Core verify logic. Returns structured result only - no stream output.
    #>
    param(
        [string]$ManifestPath,
        [switch]$SkipStateWrite
    )
    
    $manifest = Read-Manifest -Path $ManifestPath
    
    if (-not $manifest) {
        return @{ Success = $false; ExitCode = 1; Error = "Failed to read manifest"; OkCount = 0; MissingCount = 0; MissingApps = @(); VersionMismatches = 0 }
    }
    
    $okCount = 0
    $missingCount = 0
    $versionMismatchCount = 0
    $missingApps = @()
    $versionMismatchApps = @()
    $items = @()  # Structured per-app results for GUI
    $appsObserved = @{}
    $timestampUtc = (Get-Date).ToUniversalTime().ToString("o")
    
    # Get installed apps map for drift detection (winget only)
    $installedAppsMap = Get-InstalledAppsMap
    
    foreach ($app in $manifest.apps) {
        $driver = Get-AppDriver -App $app
        $appDisplayId = if ($driver -eq 'winget') { Get-AppWingetId -App $app } else { $app.id }
        
        if (-not $appDisplayId) {
            continue
        }
        
        # Use driver abstraction to check installation
        $installStatus = Test-AppInstalledWithDriver -App $app
        
        if ($installStatus.Installed) {
            # Check version constraint if present
            $versionConstraint = Parse-VersionConstraint -Constraint $app.version
            $versionSatisfied = $true
            $versionCheckResult = $null
            
            if ($versionConstraint) {
                $versionCheckResult = Test-VersionConstraint -InstalledVersion $installStatus.Version -Constraint $versionConstraint
                $versionSatisfied = $versionCheckResult.Satisfied
            }
            
            if ($versionSatisfied) {
                Write-Host "  [OK] $appDisplayId (driver: $driver)" -ForegroundColor Green
                $okCount++
                $items += @{
                    id = $appDisplayId
                    driver = $driver
                    status = 'ok'
                    version = $installStatus.Version
                }
            } else {
                Write-Host "  [VERSION] $appDisplayId - $($versionCheckResult.Reason)" -ForegroundColor Yellow
                $versionMismatchCount++
                $versionMismatchApps += @{
                    id = $appDisplayId
                    reason = $versionCheckResult.Reason
                    installedVersion = $installStatus.Version
                    constraint = $app.version
                }
                $items += @{
                    id = $appDisplayId
                    driver = $driver
                    status = 'version_mismatch'
                    version = $installStatus.Version
                    reason = $versionCheckResult.Reason
                    constraint = $app.version
                }
            }
            
            # Record observed app
            $appsObserved[$appDisplayId] = @{
                installed = $true
                driver = $driver
                version = $installStatus.Version
                versionConstraint = $app.version
                versionSatisfied = $versionSatisfied
                lastSeenUtc = $timestampUtc
            }
        } else {
            Write-Host "  [MISSING] $appDisplayId (driver: $driver)" -ForegroundColor Red
            $missingCount++
            $missingApps += $appDisplayId
            $items += @{
                id = $appDisplayId
                driver = $driver
                status = 'missing'
            }
            $appsObserved[$appDisplayId] = @{
                installed = $false
                driver = $driver
                version = $null
                versionConstraint = $app.version
                versionSatisfied = $false
                lastSeenUtc = $timestampUtc
            }
        }
    }
    
    # Compute drift (extras) - only for winget apps currently
    $drift = Compute-Drift -ManifestPath $ManifestPath -InstalledAppsMap $installedAppsMap
    $extraCount = $drift.ExtraCount
    
    Write-Host ""
    Write-Host "[endstate] Verify: Summary" -ForegroundColor Cyan
    Write-Host "  Installed OK:       $okCount" -ForegroundColor Green
    Write-Host "  Missing:            $missingCount" -ForegroundColor $(if ($missingCount -gt 0) { "Red" } else { "Green" })
    Write-Host "  Version Mismatches: $versionMismatchCount" -ForegroundColor $(if ($versionMismatchCount -gt 0) { "Yellow" } else { "Green" })
    
    # Update state (unless skipped, e.g., during tests with no state dir)
    if (-not $SkipStateWrite) {
        $manifestHash = Get-ManifestHash -Path $ManifestPath
        $state = Read-EndstateState
        if (-not $state) {
            $state = New-EndstateState
        }
        
        # Convert PSCustomObject to hashtable if needed
        if ($state -is [PSCustomObject]) {
            $stateHash = @{}
            $state.PSObject.Properties | ForEach-Object { $stateHash[$_.Name] = $_.Value }
            $state = $stateHash
        }
        
        $state.lastVerify = @{
            manifestPath = $ManifestPath
            manifestHash = $manifestHash
            timestampUtc = $timestampUtc
            okCount = $okCount
            missingCount = $missingCount
            versionMismatchCount = $versionMismatchCount
            missingApps = $missingApps
            versionMismatchApps = $versionMismatchApps
            success = ($missingCount -eq 0 -and $versionMismatchCount -eq 0)
        }
        
        # Merge appsObserved
        if (-not $state.appsObserved -or $state.appsObserved -is [PSCustomObject]) {
            $state.appsObserved = @{}
        }
        foreach ($key in $appsObserved.Keys) {
            $state.appsObserved[$key] = $appsObserved[$key]
        }
        
        Write-EndstateStateAtomic -State $state | Out-Null
    }
    
    # Determine overall success: missing OR version mismatch = failure
    $overallSuccess = ($missingCount -eq 0 -and $versionMismatchCount -eq 0)
    
    if ($missingCount -gt 0) {
        Write-Host ""
        Write-Host "Missing apps:" -ForegroundColor Yellow
        foreach ($app in $missingApps) {
            Write-Host "  - $app"
        }
    }
    
    if ($versionMismatchCount -gt 0) {
        Write-Host ""
        Write-Host "Version mismatches:" -ForegroundColor Yellow
        foreach ($mismatch in $versionMismatchApps) {
            Write-Host "  - $($mismatch.id): $($mismatch.reason)"
        }
    }
    
    if (-not $overallSuccess) {
        return @{ 
            Success = $false
            ExitCode = 1
            OkCount = $okCount
            MissingCount = $missingCount
            VersionMismatches = $versionMismatchCount
            MissingApps = $missingApps
            VersionMismatchApps = $versionMismatchApps
            ExtraCount = $extraCount
            Items = $items
        }
    }
    
    return @{ 
        Success = $true
        ExitCode = 0
        OkCount = $okCount
        MissingCount = $missingCount
        VersionMismatches = $versionMismatchCount
        MissingApps = @()
        VersionMismatchApps = @()
        ExtraCount = $extraCount
        Items = $items
    }
}

function Invoke-PlanCore {
    param(
        [string]$ManifestPath
    )
    
    $cliArgs = @{ Manifest = $ManifestPath }
    return Invoke-ProvisioningCli -ProvisioningCommand "plan" -Arguments $cliArgs
}

function Invoke-ReportCore {
    <#
    .SYNOPSIS
        Core report logic. Returns structured result only.
    .DESCRIPTION
        When -OutputJson is true, outputs JSON envelope to stdout (stream 1) only.
        All wrapper/status lines go to Information stream (6).
        When -OutPath is provided, writes JSON atomically to file.
        ALWAYS emits JSON envelope when OutputJson is true, even if no state exists.
    #>
    param(
        [string]$ManifestPath,
        [bool]$OutputJson,
        [string]$OutPath
    )
    
    $state = Read-EndstateState
    $hasState = $null -ne $state
    
    if ($OutputJson) {
        # JSON output mode - build data for envelope
        $data = [ordered]@{
            hasState = $hasState
        }
        
        if ($state) {
            $data.state = [ordered]@{
                schemaVersion = if ($state.schemaVersion) { $state.schemaVersion } else { 1 }
                lastApplied = $state.lastApplied
                lastVerify = $state.lastVerify
                appsObserved = $state.appsObserved
            }
        } else {
            $data.state = $null
        }
        
        if ($ManifestPath) {
            if (Test-Path $ManifestPath) {
                $manifestHash = Get-ManifestHash -Path $ManifestPath
                $drift = Compute-Drift -ManifestPath $ManifestPath
                $data.manifest = [ordered]@{
                    path = $ManifestPath
                    hash = $manifestHash
                }
                $data.drift = [ordered]@{
                    missing = $drift.Missing
                    extra = $drift.Extra
                    missingCount = $drift.MissingCount
                    extraCount = $drift.ExtraCount
                    versionMismatches = $drift.VersionMismatches
                }
            } else {
                $data.manifest = [ordered]@{
                    path = $ManifestPath
                    exists = $false
                }
            }
        }
        
        # Write to file if -Out specified (atomic write)
        if ($OutPath) {
            $envelope = [ordered]@{
                schemaVersion = "1.0"
                cliVersion = $script:VersionString
                command = "report"
                timestampUtc = (Get-Date).ToUniversalTime().ToString("o")
                success = $true
                data = $data
                error = $null
            }
            $jsonOutput = $envelope | ConvertTo-Json -Depth 10
            
            $outDir = Split-Path -Parent $OutPath
            if ($outDir -and -not (Test-Path $outDir)) {
                New-Item -ItemType Directory -Path $outDir -Force | Out-Null
            }
            $tempPath = "$OutPath.tmp.$([guid]::NewGuid().ToString('N').Substring(0,8))"
            try {
                Set-Content -Path $tempPath -Value $jsonOutput -Encoding UTF8 -ErrorAction Stop
                Move-Item -Path $tempPath -Destination $OutPath -Force -ErrorAction Stop
            } catch {
                if (Test-Path $tempPath) { Remove-Item $tempPath -Force -ErrorAction SilentlyContinue }
                throw
            }
        }
        
        # Return data so main switch can call Write-JsonEnvelope
        return @{ Success = $true; ExitCode = 0; HasState = $hasState; Data = $data; OutputJson = $true }
    }
    
    # Human-readable mode - check for state first
    if (-not $state) {
        Write-Host "No endstate state found. Run 'apply' or 'verify' to create state." -ForegroundColor Yellow
        return @{ Success = $true; ExitCode = 0; HasState = $false }
    }
    
    # Human-readable output
    Write-Host ""
    Write-Host "=== Endstate Report ===" -ForegroundColor Cyan
    Write-Host ""
    
    if ($state.lastApplied) {
        Write-Host "Last Applied:" -ForegroundColor Yellow
        Write-Host "  Manifest: $($state.lastApplied.manifestPath)"
        Write-Host "  Hash:     $($state.lastApplied.manifestHash)"
        Write-Host "  Time:     $($state.lastApplied.timestampUtc)"
        Write-Host ""
    } else {
        Write-Host "Last Applied: (none)" -ForegroundColor DarkGray
        Write-Host ""
    }
    
    if ($state.lastVerify) {
        Write-Host "Last Verify:" -ForegroundColor Yellow
        Write-Host "  Manifest: $($state.lastVerify.manifestPath)"
        Write-Host "  Hash:     $($state.lastVerify.manifestHash)"
        Write-Host "  Time:     $($state.lastVerify.timestampUtc)"
        Write-Host "  Result:   $(if ($state.lastVerify.success) { 'PASSED' } else { 'FAILED' })" -ForegroundColor $(if ($state.lastVerify.success) { 'Green' } else { 'Red' })
        Write-Host "  OK:       $($state.lastVerify.okCount)  Missing: $($state.lastVerify.missingCount)"
        Write-Host ""
    } else {
        Write-Host "Last Verify: (none)" -ForegroundColor DarkGray
        Write-Host ""
    }
    
    # If manifest provided, show current drift
    if ($ManifestPath) {
        Write-Host "Current Drift (vs $ManifestPath):" -ForegroundColor Yellow
        $drift = Compute-Drift -ManifestPath $ManifestPath
        if ($drift.Success) {
            Write-Host "  Missing: $($drift.MissingCount)"
            Write-Host "  Extra:   $($drift.ExtraCount)"
        } else {
            Write-Host "  Error computing drift: $($drift.Error)" -ForegroundColor Red
        }
    }
    
    return @{ Success = $true; ExitCode = 0; HasState = $true }
}

function Invoke-DoctorCore {
    <#
    .SYNOPSIS
        Core doctor logic. Returns structured result only - no stream output.
    #>
    param(
        [string]$ManifestPath
    )
    
    Write-Host ""
    Write-Host "=== Endstate Doctor ===" -ForegroundColor Cyan
    Write-Host ""
    
    # Check state
    $state = Read-EndstateState
    $hasState = $null -ne $state
    $stateStatus = if ($hasState) { "present" } else { "absent" }
    
    # Compute drift counts for stable marker (default 0 if no manifest)
    $driftMissing = 0
    $driftExtra = 0
    
    Write-Host "State:" -ForegroundColor Yellow
    if ($hasState) {
        Write-Host "  [OK] State file exists" -ForegroundColor Green
        
        if ($state.lastApplied) {
            Write-Host "  Last applied: $($state.lastApplied.timestampUtc)" -ForegroundColor DarkGray
            Write-Host "    Manifest hash: $($state.lastApplied.manifestHash.Substring(0, 16))..." -ForegroundColor DarkGray
        }
        
        if ($state.lastVerify) {
            $verifyStatus = if ($state.lastVerify.success) { "PASSED" } else { "FAILED" }
            Write-Host "  Last verify: $($state.lastVerify.timestampUtc) ($verifyStatus)" -ForegroundColor DarkGray
        }
    } else {
        Write-Host "  [INFO] No state file (run apply or verify to create)" -ForegroundColor DarkGray
    }
    Write-Host ""
    
    # Check manifest hash drift if manifest provided
    if ($ManifestPath -and $hasState -and $state.lastApplied) {
        Write-Host "Manifest Drift:" -ForegroundColor Yellow
        $currentHash = Get-ManifestHash -Path $ManifestPath
        $lastHash = $state.lastApplied.manifestHash
        
        if ($currentHash -eq $lastHash) {
            Write-Host "  [OK] Manifest unchanged since last apply" -ForegroundColor Green
        } else {
            Write-Host "  [DRIFT] Manifest has changed since last apply" -ForegroundColor Yellow
            Write-Host "    Last applied: $($lastHash.Substring(0, 16))..." -ForegroundColor DarkGray
            Write-Host "    Current:      $($currentHash.Substring(0, 16))..." -ForegroundColor DarkGray
            Write-Host "    Suggestion: Run 'apply' to converge" -ForegroundColor Cyan
        }
        Write-Host ""
        
        # Show drift summary
        Write-Host "App Drift:" -ForegroundColor Yellow
        $drift = Compute-Drift -ManifestPath $ManifestPath
        if ($drift.Success) {
            if ($drift.MissingCount -eq 0 -and $drift.ExtraCount -eq 0) {
                Write-Host "  [OK] No drift detected" -ForegroundColor Green
            } else {
                if ($drift.MissingCount -gt 0) {
                    Write-Host "  [MISSING] $($drift.MissingCount) app(s) required but not installed" -ForegroundColor Red
                    Write-Host "    Suggestion: Run 'apply -Manifest $ManifestPath' to install" -ForegroundColor Cyan
                }
                if ($drift.ExtraCount -gt 0) {
                    Write-Host "  [EXTRA] $($drift.ExtraCount) app(s) installed but not in manifest" -ForegroundColor Yellow
                    Write-Host "    Suggestion: Update manifest to include observed extras" -ForegroundColor Cyan
                }
            }
            $driftMissing = $drift.MissingCount
            $driftExtra = $drift.ExtraCount
        } else {
            Write-Host "  [ERROR] Could not compute drift: $($drift.Error)" -ForegroundColor Red
        }
        Write-Host ""
    }
    
    # Delegate to provisioning doctor for additional checks
    Write-Host "Provisioning Subsystem:" -ForegroundColor Yellow
    $provResult = Invoke-ProvisioningCli -ProvisioningCommand "doctor" -Arguments @{}
    
    return @{ Success = $true; ExitCode = 0; HasState = $hasState; StateStatus = $stateStatus; DriftMissing = $driftMissing; DriftExtra = $driftExtra }
}

function Invoke-StateResetCore {
    <#
    .SYNOPSIS
        Core state reset logic. Returns structured result only - no stream output.
    #>
    $statePath = Get-EndstateStatePath
    
    if (-not (Test-Path $statePath)) {
        Write-Host "No state file found at $statePath" -ForegroundColor Yellow
        return @{ Success = $true; ExitCode = 0; WasReset = $false }
    }
    
    try {
        Remove-Item -Path $statePath -Force -ErrorAction Stop
        Write-Host "State file deleted: $statePath" -ForegroundColor Green
        return @{ Success = $true; ExitCode = 0; WasReset = $true }
    } catch {
        Write-Host "[ERROR] Failed to delete state file: $_" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = $_.ToString() }
    }
}

function Invoke-StateExportCore {
    <#
    .SYNOPSIS
        Export state to a file. If no state exists, exports valid empty schema.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$OutPath
    )
    
    # Security: Validate output path is not outside reasonable bounds
    # (Allow any path for export - user controls destination)
    
    $state = Read-EndstateState
    
    if (-not $state) {
        # No state exists - export empty schema
        $state = New-EndstateState
        Write-Host "No existing state - exporting empty schema" -ForegroundColor Yellow
    }
    
    # Convert PSCustomObject to hashtable if needed
    if ($state -is [PSCustomObject]) {
        $stateHash = @{}
        $state.PSObject.Properties | ForEach-Object { $stateHash[$_.Name] = $_.Value }
        $state = $stateHash
    }
    
    # Ensure schemaVersion is present
    if (-not $state.schemaVersion) {
        $state.schemaVersion = 1
    }
    
    # Atomic write
    $outDir = Split-Path -Parent $OutPath
    if ($outDir -and -not (Test-Path $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    }
    
    $tempPath = "$OutPath.tmp.$([guid]::NewGuid().ToString('N').Substring(0,8))"
    try {
        $jsonContent = $state | ConvertTo-Json -Depth 10
        Set-Content -Path $tempPath -Value $jsonContent -Encoding UTF8 -ErrorAction Stop
        Move-Item -Path $tempPath -Destination $OutPath -Force -ErrorAction Stop
        Write-Host "State exported to: $OutPath" -ForegroundColor Green
        return @{ Success = $true; ExitCode = 0; OutputPath = $OutPath }
    } catch {
        if (Test-Path $tempPath) { Remove-Item $tempPath -Force -ErrorAction SilentlyContinue }
        Write-Host "[ERROR] Failed to export state: $_" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = $_.ToString() }
    }
}

function Invoke-StateImportCore {
    <#
    .SYNOPSIS
        Import state from a file with merge or replace behavior.
    .DESCRIPTION
        - Validates JSON and schemaVersion
        - Merge (default): incoming overwrites only if timestamp is newer
        - Replace: backup existing, then replace entirely
        - Security: Only writes under .endstate/, never outside repo root
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$InPath,
        
        [Parameter(Mandatory = $false)]
        [bool]$ReplaceMode = $false
    )
    
    # Validate input file exists
    if (-not (Test-Path $InPath)) {
        Write-Host "[ERROR] Import file not found: $InPath" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = "File not found" }
    }
    
    # Read and validate incoming state
    try {
        $incomingContent = Get-Content -Path $InPath -Raw -ErrorAction Stop
        $incoming = $incomingContent | ConvertFrom-Json -ErrorAction Stop
    } catch {
        Write-Host "[ERROR] Failed to parse import file as JSON: $_" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = "Invalid JSON" }
    }
    
    # Validate schemaVersion
    if (-not $incoming.schemaVersion) {
        Write-Host "[ERROR] Import file missing schemaVersion" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = "Missing schemaVersion" }
    }
    
    if ($incoming.schemaVersion -ne 1) {
        Write-Host "[ERROR] Unsupported schemaVersion: $($incoming.schemaVersion) (expected 1)" -ForegroundColor Red
        return @{ Success = $false; ExitCode = 1; Error = "Unsupported schemaVersion" }
    }
    
    # Convert incoming PSCustomObject to hashtable
    $incomingHash = @{}
    $incoming.PSObject.Properties | ForEach-Object { $incomingHash[$_.Name] = $_.Value }
    
    $stateDir = Get-EndstateStateDir
    $statePath = Get-EndstateStatePath
    
    # Ensure state directory exists
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    
    if ($ReplaceMode) {
        # Replace mode: backup existing state first
        if (Test-Path $statePath) {
            $backupDir = Join-Path $stateDir "backup"
            if (-not (Test-Path $backupDir)) {
                New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
            }
            $timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmss")
            $backupPath = Join-Path $backupDir "state.$timestamp.json"
            try {
                Copy-Item -Path $statePath -Destination $backupPath -Force -ErrorAction Stop
                Write-Host "Existing state backed up to: $backupPath" -ForegroundColor Cyan
            } catch {
                Write-Host "[ERROR] Failed to backup existing state: $_" -ForegroundColor Red
                return @{ Success = $false; ExitCode = 1; Error = "Backup failed" }
            }
        }
        
        # Write incoming state directly
        $result = Write-EndstateStateAtomic -State $incomingHash
        if ($result) {
            Write-Host "State replaced from: $InPath" -ForegroundColor Green
            return @{ Success = $true; ExitCode = 0; Mode = "replace" }
        } else {
            return @{ Success = $false; ExitCode = 1; Error = "Write failed" }
        }
    } else {
        # Merge mode (default): merge incoming into existing
        $existing = Read-EndstateState
        if (-not $existing) {
            $existing = New-EndstateState
        }
        
        # Convert existing PSCustomObject to hashtable if needed
        if ($existing -is [PSCustomObject]) {
            $existingHash = @{}
            $existing.PSObject.Properties | ForEach-Object { $existingHash[$_.Name] = $_.Value }
            $existing = $existingHash
        }
        
        # Merge logic: incoming overwrites only if timestamp is newer
        # lastApplied
        if ($incomingHash.lastApplied) {
            $incomingAppliedTime = $null
            $existingAppliedTime = $null
            
            if ($incomingHash.lastApplied.timestampUtc) {
                try { $incomingAppliedTime = [DateTime]::Parse($incomingHash.lastApplied.timestampUtc) } catch {}
            }
            if ($existing.lastApplied -and $existing.lastApplied.timestampUtc) {
                try { $existingAppliedTime = [DateTime]::Parse($existing.lastApplied.timestampUtc) } catch {}
            }
            
            if ($null -eq $existingAppliedTime -or ($null -ne $incomingAppliedTime -and $incomingAppliedTime -gt $existingAppliedTime)) {
                $existing.lastApplied = $incomingHash.lastApplied
            }
        }
        
        # lastVerify
        if ($incomingHash.lastVerify) {
            $incomingVerifyTime = $null
            $existingVerifyTime = $null
            
            if ($incomingHash.lastVerify.timestampUtc) {
                try { $incomingVerifyTime = [DateTime]::Parse($incomingHash.lastVerify.timestampUtc) } catch {}
            }
            if ($existing.lastVerify -and $existing.lastVerify.timestampUtc) {
                try { $existingVerifyTime = [DateTime]::Parse($existing.lastVerify.timestampUtc) } catch {}
            }
            
            if ($null -eq $existingVerifyTime -or ($null -ne $incomingVerifyTime -and $incomingVerifyTime -gt $existingVerifyTime)) {
                $existing.lastVerify = $incomingHash.lastVerify
            }
        }
        
        # appsObserved: merge by key, incoming wins on conflict
        if ($incomingHash.appsObserved) {
            if (-not $existing.appsObserved -or $existing.appsObserved -is [PSCustomObject]) {
                $existing.appsObserved = @{}
            }
            
            $incomingApps = $incomingHash.appsObserved
            if ($incomingApps -is [PSCustomObject]) {
                $incomingApps.PSObject.Properties | ForEach-Object {
                    $existing.appsObserved[$_.Name] = $_.Value
                }
            } elseif ($incomingApps -is [hashtable]) {
                foreach ($key in $incomingApps.Keys) {
                    $existing.appsObserved[$key] = $incomingApps[$key]
                }
            }
        }
        
        $result = Write-EndstateStateAtomic -State $existing
        if ($result) {
            Write-Host "State merged from: $InPath" -ForegroundColor Green
            return @{ Success = $true; ExitCode = 0; Mode = "merge" }
        } else {
            return @{ Success = $false; ExitCode = 1; Error = "Write failed" }
        }
    }
}

# Helper to resolve manifest path with validation
function Resolve-ManifestPathWithValidation {
    param(
        [string]$ProfileName,
        [string]$ManifestPath,
        [string]$CommandName
    )
    
    if ($ManifestPath) {
        return $ManifestPath
    } elseif ($ProfileName) {
        return Resolve-ManifestPath -ProfileName $ProfileName
    } else {
        # Don't write to stdout here - let command handlers emit proper JSON envelope
        return $null
    }
}

function Write-JsonEnvelope {
    <#
    .SYNOPSIS
        Emit a standard JSON envelope to stdout for GUI consumption.
    .DESCRIPTION
        Ensures pure JSON output on stdout with consistent schema.
        All non-JSON output (banner, progress) must go to other streams.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$CommandName,
        
        [Parameter(Mandatory = $true)]
        [bool]$Success,
        
        [Parameter(Mandatory = $false)]
        [object]$Data = $null,
        
        [Parameter(Mandatory = $false)]
        [hashtable]$Error = $null,
        
        [Parameter(Mandatory = $false)]
        [int]$ExitCode = 0
    )
    
    $envelope = [ordered]@{
        schemaVersion = "1.0"
        cliVersion = $script:VersionString
        command = $CommandName
        timestampUtc = (Get-Date).ToUniversalTime().ToString("o")
        success = $Success
        data = $Data
        error = $Error
    }
    
    # Emit ONLY to stdout (stream 1) as SINGLE LINE - no Write-Host, no Write-Information
    $jsonOutput = $envelope | ConvertTo-Json -Depth 10 -Compress
    Write-Output $jsonOutput
    
    # Set exit code
    $script:LASTEXITCODE = $ExitCode
}

# Main execution - skip if loading functions only (for testing)
if ($LoadFunctionsOnly) {
    return
}

# Handle --version flag before anything else
if ($Version.IsPresent) {
    Write-Host $script:VersionString
    exit 0
}

# Suppress banner for JSON output mode (any command with -Json needs pure JSON to stdout)
$suppressBanner = $Json.IsPresent

if (-not $suppressBanner) {
    Show-Banner
}

if (-not $Command) {
    Show-Help
    exit 0
}

$exitCode = 0

switch ($Command) {
    "bootstrap" {
        Write-Information "[endstate] Bootstrap: installing to PATH..." -InformationAction Continue
        $result = Install-EndstateToPath -RepoRootPath $RepoRoot
        if ($result.RepoRootConfigured) {
            Write-Information "[endstate] Bootstrap: repo root configured: $($result.RepoRoot)" -InformationAction Continue
        } else {
            Write-Information "[endstate] Bootstrap: repo root not configured (profile resolution may not work)" -InformationAction Continue
        }
        Write-Information "[endstate] Bootstrap: completed ExitCode=$($result.ExitCode)" -InformationAction Continue
        $exitCode = $result.ExitCode
    }
    "capture" {
        if (-not $Json) {
            Write-Information "[endstate] Capture: starting..." -InformationAction Continue
        }
        $captureResult = Invoke-CaptureCore -OutputPath $Out -IsExample $Example.IsPresent -IsSanitize $Sanitize.IsPresent -ManifestName $Name -CustomExamplesDir $ExamplesDir -ForceOverwrite $Force.IsPresent
        
        if ($Json) {
            # Emit JSON envelope for capture result
            if ($captureResult.Success) {
                $data = @{
                    outputPath = $captureResult.OutputPath
                    sanitized = $captureResult.Sanitized
                    isExample = $captureResult.IsExample
                }
                # Include structured counts
                if ($captureResult.Counts) {
                    $data.counts = $captureResult.Counts
                }
                # Include apps list
                if ($captureResult.AppsIncluded) {
                    $data.appsIncluded = $captureResult.AppsIncluded
                }
                Write-JsonEnvelope -CommandName "capture" -Success $true -Data $data -ExitCode 0
            } else {
                $errorDetail = @{
                    code = if ($captureResult.Blocked) { "CAPTURE_BLOCKED" } else { "CAPTURE_FAILED" }
                    message = if ($captureResult.Error) { $captureResult.Error } else { "Capture failed" }
                }
                $captureExitCode = if ($captureResult.ExitCode) { $captureResult.ExitCode } else { 1 }
                Write-JsonEnvelope -CommandName "capture" -Success $false -Data $null -Error $errorDetail -ExitCode $captureExitCode
            }
        } else {
            if ($captureResult.OutputPath) {
                Write-Information "[endstate] Capture: output path is $($captureResult.OutputPath)" -InformationAction Continue
            }
            if ($captureResult.Blocked) {
                Write-Information "[endstate] Capture: BLOCKED - $($captureResult.Error)" -InformationAction Continue
            }
            if ($captureResult.Success) {
                $completedMsg = if ($captureResult.Sanitized) { "completed (sanitized, $($captureResult.AppCount) apps)" } else { "completed" }
                Write-Information "[endstate] Capture: $completedMsg" -InformationAction Continue
            }
        }
        
        if ($captureResult -and $captureResult.ExitCode) {
            $exitCode = $captureResult.ExitCode
        } elseif ($captureResult -and -not $captureResult.Success) {
            $exitCode = 1
        }
    }
    "apply" {
        try {
            $resolvedPath = Resolve-ManifestPathWithValidation -ProfileName $Profile -ManifestPath $Manifest -CommandName "apply"
            if (-not $resolvedPath) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_NOT_FOUND"
                        message = "Either -Profile or -Manifest is required for 'apply' command."
                        detail = @{ 
                            profile = $Profile
                            manifestPath = $Manifest
                        }
                    }
                    Write-JsonEnvelope -CommandName "apply" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                }
                exit 1
            }
            
            # Check if manifest file actually exists
            if (-not (Test-Path $resolvedPath)) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_NOT_FOUND"
                        message = "Manifest file not found at path: $resolvedPath"
                        detail = @{ 
                            manifestPath = $resolvedPath
                            profile = $Profile
                        }
                    }
                    Write-JsonEnvelope -CommandName "apply" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                } else {
                    Write-Host "[ERROR] Manifest file not found: $resolvedPath" -ForegroundColor Red
                }
                exit 1
            }
            
            # Validate manifest against profile contract before apply
            . "$script:EndstateRoot\engine\manifest.ps1"
            $validationResult = Test-ProfileManifest -Path $resolvedPath
            if (-not $validationResult.Valid) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_VALIDATION_ERROR"
                        message = "Manifest validation failed"
                        detail = @{ 
                            manifestPath = $resolvedPath
                            errors = $validationResult.Errors
                        }
                    }
                    Write-JsonEnvelope -CommandName "apply" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                } else {
                    Write-Host "[ERROR] Manifest validation failed: $resolvedPath" -ForegroundColor Red
                    foreach ($err in $validationResult.Errors) {
                        Write-Host "        $($err.Code): $($err.Message)" -ForegroundColor Red
                    }
                }
                exit 1
            }
            
            if (-not $Json) {
                Write-Information "[endstate] Apply: starting with manifest $resolvedPath" -InformationAction Continue
            }
            $result = Invoke-ApplyCore -ManifestPath $resolvedPath -IsDryRun $DryRun.IsPresent -IsOnlyApps $OnlyApps.IsPresent
            
            if ($Json) {
                # Emit JSON envelope for apply result
                $data = @{
                    manifestPath = $resolvedPath
                    installed = $result.Installed
                    upgraded = $result.Upgraded
                    skipped = $result.Skipped
                    failed = $result.Failed
                    dryRun = $DryRun.IsPresent
                }
                # Include structured counts
                if ($result.Counts) {
                    $data.counts = $result.Counts
                }
                # Include per-app items
                if ($result.Items) {
                    $data.items = $result.Items
                }
                Write-JsonEnvelope -CommandName "apply" -Success $result.Success -Data $data -ExitCode $result.ExitCode
            } else {
                Write-Information "[endstate] Apply: completed ExitCode=$($result.ExitCode)" -InformationAction Continue
            }
            $exitCode = $result.ExitCode
        } catch {
            if ($Json) {
                $errorDetail = @{
                    code = "INTERNAL_ERROR"
                    message = $_.Exception.Message
                    detail = @{ exception = $_.ToString() }
                }
                Write-JsonEnvelope -CommandName "apply" -Success $false -Data $null -Error $errorDetail -ExitCode 1
            } else {
                Write-Host "[ERROR] Apply failed: $($_.Exception.Message)" -ForegroundColor Red
            }
            exit 1
        }
    }
    "verify" {
        try {
            $resolvedPath = Resolve-ManifestPathWithValidation -ProfileName $Profile -ManifestPath $Manifest -CommandName "verify"
            if (-not $resolvedPath) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_NOT_FOUND"
                        message = "Either -Profile or -Manifest is required for 'verify' command."
                        detail = @{ 
                            profile = $Profile
                            manifestPath = $Manifest
                        }
                    }
                    Write-JsonEnvelope -CommandName "verify" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                }
                exit 1
            }
            
            # Check if manifest file actually exists
            if (-not (Test-Path $resolvedPath)) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_NOT_FOUND"
                        message = "Manifest file not found at path: $resolvedPath"
                        detail = @{ 
                            manifestPath = $resolvedPath
                            profile = $Profile
                        }
                    }
                    Write-JsonEnvelope -CommandName "verify" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                } else {
                    Write-Host "[ERROR] Manifest file not found: $resolvedPath" -ForegroundColor Red
                }
                exit 1
            }
            
            # Validate manifest against profile contract before verify
            . "$script:EndstateRoot\engine\manifest.ps1"
            $validationResult = Test-ProfileManifest -Path $resolvedPath
            if (-not $validationResult.Valid) {
                if ($Json) {
                    $errorDetail = @{
                        code = "MANIFEST_VALIDATION_ERROR"
                        message = "Manifest validation failed"
                        detail = @{ 
                            manifestPath = $resolvedPath
                            errors = $validationResult.Errors
                        }
                    }
                    Write-JsonEnvelope -CommandName "verify" -Success $false -Data $null -Error $errorDetail -ExitCode 1
                } else {
                    Write-Host "[ERROR] Manifest validation failed: $resolvedPath" -ForegroundColor Red
                    foreach ($err in $validationResult.Errors) {
                        Write-Host "        $($err.Code): $($err.Message)" -ForegroundColor Red
                    }
                }
                exit 1
            }
            
            if (-not $Json) {
                Write-Information "[endstate] Verify: checking manifest $resolvedPath" -InformationAction Continue
            }
            $result = Invoke-VerifyCore -ManifestPath $resolvedPath
            
            if ($Json) {
                # Emit JSON envelope for verify result
                $data = @{
                    manifestPath = $resolvedPath
                    okCount = $result.OkCount
                    missingCount = $result.MissingCount
                    versionMismatches = $result.VersionMismatches
                    extraCount = $result.ExtraCount
                    missingApps = $result.MissingApps
                    versionMismatchApps = $result.VersionMismatchApps
                    items = $result.Items
                }
                Write-JsonEnvelope -CommandName "verify" -Success $result.Success -Data $data -ExitCode $result.ExitCode
            } else {
                Write-Information "[endstate] Verify: OkCount=$($result.OkCount) MissingCount=$($result.MissingCount) VersionMismatches=$($result.VersionMismatches) ExtraCount=$($result.ExtraCount)" -InformationAction Continue
                Write-Information "[endstate] Drift: Missing=$($result.MissingCount) Extra=$($result.ExtraCount) VersionMismatches=$($result.VersionMismatches)" -InformationAction Continue
                $passedFailed = if ($result.Success) { "PASSED" } else { "FAILED" }
                Write-Information "[endstate] Verify: $passedFailed" -InformationAction Continue
            }
            $exitCode = $result.ExitCode
        } catch {
            if ($Json) {
                $errorDetail = @{
                    code = "INTERNAL_ERROR"
                    message = $_.Exception.Message
                    detail = @{ exception = $_.ToString() }
                }
                Write-JsonEnvelope -CommandName "verify" -Success $false -Data $null -Error $errorDetail -ExitCode 1
            } else {
                Write-Host "[ERROR] Verify failed: $($_.Exception.Message)" -ForegroundColor Red
            }
            exit 1
        }
    }
    "plan" {
        $resolvedPath = Resolve-ManifestPathWithValidation -ProfileName $Profile -ManifestPath $Manifest -CommandName "plan"
        if (-not $resolvedPath) {
            exit 1
        }
        $result = Invoke-PlanCore -ManifestPath $resolvedPath
        $exitCode = $result.ExitCode
    }
    "report" {
        # For JSON mode, emit wrapper lines only to Information stream (not stdout)
        if (-not $Json.IsPresent) {
            Write-Information "[endstate] Report: reading state..." -InformationAction Continue
        }
        $result = Invoke-ReportCore -ManifestPath $Manifest -OutputJson $Json.IsPresent -OutPath $Out
        
        # If JSON output was requested, emit the envelope now
        if ($result.OutputJson) {
            Write-JsonEnvelope -CommandName "report" -Success $result.Success -Data $result.Data -ExitCode $result.ExitCode
        } elseif (-not $Json.IsPresent) {
            if ($result.HasState) {
                Write-Information "[endstate] Report: completed" -InformationAction Continue
            } else {
                Write-Information "[endstate] Report: no state found" -InformationAction Continue
            }
        }
        $exitCode = $result.ExitCode
    }
    "doctor" {
        Write-Information "[endstate] Doctor: checking environment..." -InformationAction Continue
        $result = Invoke-DoctorCore -ManifestPath $Manifest
        Write-Information "[endstate] Doctor: state=$($result.StateStatus) driftMissing=$($result.DriftMissing) driftExtra=$($result.DriftExtra)" -InformationAction Continue
        Write-Information "[endstate] Doctor: completed" -InformationAction Continue
        $exitCode = $result.ExitCode
    }
    "state" {
        switch ($SubCommand) {
            "reset" {
                Write-Information "[endstate] State: resetting..." -InformationAction Continue
                $result = Invoke-StateResetCore
                if ($result.WasReset) {
                    Write-Information "[endstate] State: reset completed" -InformationAction Continue
                } else {
                    Write-Information "[endstate] State: no state file to reset" -InformationAction Continue
                }
                $exitCode = $result.ExitCode
            }
            "export" {
                if (-not $Out) {
                    Write-Host "[ERROR] -Out <path> is required for 'state export'" -ForegroundColor Red
                    exit 1
                }
                Write-Information "[endstate] State: exporting..." -InformationAction Continue
                $result = Invoke-StateExportCore -OutPath $Out
                if ($result.Success) {
                    Write-Information "[endstate] State: export completed to $($result.OutputPath)" -InformationAction Continue
                } else {
                    Write-Information "[endstate] State: export failed" -InformationAction Continue
                }
                $exitCode = $result.ExitCode
            }
            "import" {
                if (-not $In) {
                    Write-Host "[ERROR] -In <path> is required for 'state import'" -ForegroundColor Red
                    exit 1
                }
                $replaceMode = $Replace.IsPresent
                $modeLabel = if ($replaceMode) { "replace" } else { "merge" }
                Write-Information "[endstate] State: importing ($modeLabel)..." -InformationAction Continue
                $result = Invoke-StateImportCore -InPath $In -ReplaceMode $replaceMode
                if ($result.Success) {
                    Write-Information "[endstate] State: import completed ($($result.Mode))" -InformationAction Continue
                } else {
                    Write-Information "[endstate] State: import failed - $($result.Error)" -InformationAction Continue
                }
                $exitCode = $result.ExitCode
            }
            default {
                if ($SubCommand) {
                    Write-Host "[ERROR] Unknown state subcommand: $SubCommand" -ForegroundColor Red
                } else {
                    Write-Host "[ERROR] State command requires a subcommand (e.g., 'reset', 'export', 'import')" -ForegroundColor Red
                }
                Write-Host "Usage:" -ForegroundColor Yellow
                Write-Host "  .\endstate.ps1 state reset" -ForegroundColor Yellow
                Write-Host "  .\endstate.ps1 state export -Out <path>" -ForegroundColor Yellow
                Write-Host "  .\endstate.ps1 state import -In <path> [-Merge] [-Replace]" -ForegroundColor Yellow
                $exitCode = 1
            }
        }
    }
    "validate" {
        # Validate a profile manifest against the contract
        # Usage: endstate validate <path>
        # The path can be provided via -Manifest or as the SubCommand (positional)
        $targetPath = if ($Manifest) { $Manifest } elseif ($SubCommand) { $SubCommand } else { $null }
        
        if (-not $targetPath) {
            if ($Json) {
                $errorDetail = @{
                    code = "MISSING_PATH"
                    message = "Usage: endstate validate <path>"
                }
                Write-JsonEnvelope -CommandName "validate" -Success $false -Data $null -Error $errorDetail -ExitCode 1
            } else {
                Write-Host "[ERROR] Usage: endstate validate <path>" -ForegroundColor Red
                Write-Host "        Validates a profile manifest against the Endstate profile contract." -ForegroundColor Yellow
            }
            exit 1
        }
        
        # Resolve path if relative
        if (-not [System.IO.Path]::IsPathRooted($targetPath)) {
            $targetPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($targetPath)
        }
        
        # Import manifest.ps1 to get Test-ProfileManifest
        . "$script:EndstateRoot\engine\manifest.ps1"
        
        $result = Test-ProfileManifest -Path $targetPath
        
        if ($Json) {
            $data = @{
                path = $targetPath
                valid = $result.Valid
                errors = $result.Errors
            }
            if ($result.Summary) {
                $data.summary = $result.Summary
            }
            if ($result.Warnings) {
                $data.warnings = $result.Warnings
            }
            $validateExitCode = if ($result.Valid) { 0 } else { 1 }
            Write-JsonEnvelope -CommandName "validate" -Success $result.Valid -Data $data -ExitCode $validateExitCode
        } else {
            if ($result.Valid) {
                Write-Host "[OK] Valid profile (v$($result.Summary.Version))" -ForegroundColor Green
                Write-Host "     Name: $($result.Summary.Name)" -ForegroundColor Gray
                Write-Host "     Apps: $($result.Summary.AppCount)" -ForegroundColor Gray
                if ($result.Summary.Captured) {
                    Write-Host "     Captured: $($result.Summary.Captured)" -ForegroundColor Gray
                }
                if ($result.Warnings -and $result.Warnings.Count -gt 0) {
                    Write-Host ""
                    Write-Host "[WARN] Warnings:" -ForegroundColor Yellow
                    foreach ($warn in $result.Warnings) {
                        Write-Host "       $($warn.Code): $($warn.Message)" -ForegroundColor Yellow
                    }
                }
            } else {
                Write-Host "[INVALID] Profile validation failed" -ForegroundColor Red
                foreach ($err in $result.Errors) {
                    Write-Host "          $($err.Code): $($err.Message)" -ForegroundColor Red
                }
            }
        }
        $exitCode = if ($result.Valid) { 0 } else { 1 }
    }
    "capabilities" {
        # Output JSON list of available commands for GUI integration
        if ($Json) {
            # Use standard JSON envelope for consistency
            $data = @{
                commands = @(
                    "bootstrap",
                    "capture",
                    "apply",
                    "plan",
                    "verify",
                    "validate",
                    "report",
                    "doctor",
                    "state",
                    "capabilities"
                )
                version = $script:VersionString
                supportedFlags = @{
                    apply = @("--profile", "--manifest", "--json", "--dry-run", "--enable-restore")
                    verify = @("--profile", "--manifest", "--json")
                    validate = @("--manifest", "--json")
                    report = @("--json", "--out", "--latest", "--runid", "--last")
                    capabilities = @("--json")
                }
            }
            Write-JsonEnvelope -CommandName "capabilities" -Success $true -Data $data -ExitCode 0
        } else {
            Write-Host "Available commands:" -ForegroundColor Cyan
            $commands = @("bootstrap", "capture", "apply", "plan", "verify", "validate", "report", "doctor", "state", "capabilities")
            foreach ($cmd in $commands) {
                Write-Host "  - $cmd" -ForegroundColor White
            }
            Write-Host ""
            Write-Host "Version: $($script:VersionString)" -ForegroundColor Gray
        }
        $exitCode = 0
    }
    default {
        Show-Help
        exit 1
    }
}

exit $exitCode
