# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning report - summarizes previous runs from saved state JSON files.

.DESCRIPTION
    Reads state files from provisioning/state/*.json and displays run history
    with summary counts, failed actions, and file paths.
#>

$script:ReportRoot = $PSScriptRoot | Split-Path -Parent

function Get-ReportVersion {
    <#
    .SYNOPSIS
        Returns the current version for report metadata.
    #>
    $versionFile = Join-Path $script:ReportRoot "VERSION.txt"
    
    if (Test-Path $versionFile) {
        return (Get-Content -Path $versionFile -Raw).Trim()
    }
    
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return "0.0.0-dev+$gitSha"
        }
    } catch { }
    
    return "0.0.0-dev"
}

function Get-ReportGitSha {
    <#
    .SYNOPSIS
        Returns the current git SHA for report metadata.
    #>
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return $gitSha.Trim()
        }
    } catch { }
    return $null
}

function Get-StateFiles {
    <#
    .SYNOPSIS
        Get state JSON files sorted by filename (descending = newest first).
    .PARAMETER StateDir
        Path to the state directory.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$StateDir
    )
    
    if (-not (Test-Path $StateDir)) {
        return @()
    }
    
    # Get all JSON files directly in state dir and any subdirectories
    $files = @()
    
    # Direct files in state/
    $directFiles = Get-ChildItem -Path $StateDir -Filter "*.json" -File -ErrorAction SilentlyContinue
    if ($directFiles) {
        $files += $directFiles
    }
    
    # Files in subdirectories (but not backups or capture)
    $subDirs = Get-ChildItem -Path $StateDir -Directory -ErrorAction SilentlyContinue | 
        Where-Object { $_.Name -notin @('backups', 'capture') }
    
    foreach ($subDir in $subDirs) {
        $subFiles = Get-ChildItem -Path $subDir.FullName -Filter "*.json" -File -ErrorAction SilentlyContinue
        if ($subFiles) {
            $files += $subFiles
        }
    }
    
    # Sort by name descending (runId format yyyyMMdd-HHmmss sorts chronologically)
    return $files | Sort-Object Name -Descending
}

function Read-StateFile {
    <#
    .SYNOPSIS
        Read and parse a state JSON file.
    .PARAMETER Path
        Path to the state file.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    try {
        # Load state file (supports JSONC format)
        . "$PSScriptRoot\manifest.ps1"
        $state = Read-JsoncFile -Path $Path -Depth 100
        
        # Convert to PSCustomObject for Add-Member compatibility
        $stateObj = $state | ConvertTo-Json -Depth 100 | ConvertFrom-Json
        
        # Add file path to state object for reference
        $stateObj | Add-Member -NotePropertyName '_filePath' -NotePropertyValue $Path -Force
        
        return $stateObj
    } catch {
        return $null
    }
}

function Get-ProvisioningReport {
    <#
    .SYNOPSIS
        Get provisioning run report(s) based on selection criteria.
    .PARAMETER StateDir
        Path to the state directory.
    .PARAMETER RunId
        Specific run ID to retrieve.
    .PARAMETER Latest
        Select the most recent run.
    .PARAMETER Last
        Select the N most recent runs.
    .OUTPUTS
        Array of state objects matching the criteria.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$StateDir,
        
        [Parameter(Mandatory = $false)]
        [string]$RunId,
        
        [Parameter(Mandatory = $false)]
        [switch]$Latest,
        
        [Parameter(Mandatory = $false)]
        [int]$Last = 0
    )
    
    $stateFiles = Get-StateFiles -StateDir $StateDir
    
    if ($stateFiles.Count -eq 0) {
        return @()
    }
    
    # Selection logic
    if ($RunId) {
        # Find specific run by ID
        $targetFile = $stateFiles | Where-Object { $_.BaseName -eq $RunId } | Select-Object -First 1
        if ($targetFile) {
            $state = Read-StateFile -Path $targetFile.FullName
            if ($state) {
                return @($state)
            }
        }
        return @()
    }
    
    if ($Last -gt 0) {
        # Get N most recent
        $selectedFiles = $stateFiles | Select-Object -First $Last
        $results = @()
        foreach ($file in $selectedFiles) {
            $state = Read-StateFile -Path $file.FullName
            if ($state) {
                $results += $state
            }
        }
        return $results
    }
    
    # Default: Latest (most recent)
    $latestFile = $stateFiles | Select-Object -First 1
    if ($latestFile) {
        $state = Read-StateFile -Path $latestFile.FullName
        if ($state) {
            return @($state)
        }
    }
    
    return @()
}

function Format-ReportHuman {
    <#
    .SYNOPSIS
        Format a single run report for human-readable console output.
    .PARAMETER State
        The state object to format.
    .PARAMETER ShowDetails
        Show detailed action lists (for single run view).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [object]$State,
        
        [Parameter(Mandatory = $false)]
        [switch]$ShowDetails
    )
    
    $output = @()
    
    # Header
    $dryRunTag = if ($State.dryRun) { " [DRY-RUN]" } else { "" }
    $output += ""
    $output += "Run: $($State.runId)$dryRunTag"
    $output += "Timestamp: $($State.timestamp)"
    $output += "Command: $($State.command)"
    
    if ($State.manifest) {
        $manifestName = if ($State.manifest.path) { Split-Path -Leaf $State.manifest.path } else { "unknown" }
        $output += "Manifest: $manifestName"
        if ($State.manifest.path) { $output += "  Path: $($State.manifest.path)" }
        if ($State.manifest.hash) { $output += "  Hash: $($State.manifest.hash)" }
    }
    
    $output += ""
    $output += "Summary:"
    
    # Summary counts
    $success = if ($State.summary.success) { $State.summary.success } else { 0 }
    $skipped = if ($State.summary.skipped) { $State.summary.skipped } else { 0 }
    $failed = if ($State.summary.failed) { $State.summary.failed } else { 0 }
    
    $output += "  Succeeded: $success"
    $output += "  Skipped:   $skipped"
    $output += "  Failed:    $failed"
    
    if ($ShowDetails -and $State.actions) {
        # Failed actions (first 10)
        $failedActions = @($State.actions | Where-Object { $_.status -eq 'failed' })
        if ($failedActions.Count -gt 0) {
            $output += ""
            $output += "Failed Actions (first 10):"
            $displayCount = [Math]::Min($failedActions.Count, 10)
            for ($i = 0; $i -lt $displayCount; $i++) {
                $action = $failedActions[$i]
                $id = if ($action.action.id) { $action.action.id } else { $action.action.ref }
                $reason = if ($action.message) { $action.message } else { "unknown" }
                $output += "  - $id : $reason"
            }
            if ($failedActions.Count -gt 10) {
                $output += "  ... and $($failedActions.Count - 10) more"
            }
        }
        
        # Installs performed (first 10)
        $installs = @($State.actions | Where-Object { 
            $_.status -in @('success', 'dry-run') -and $_.action.status -eq 'install'
        })
        if ($installs.Count -gt 0) {
            $output += ""
            $label = if ($State.dryRun) { "Would Install (first 10):" } else { "Installed (first 10):" }
            $output += $label
            $displayCount = [Math]::Min($installs.Count, 10)
            for ($i = 0; $i -lt $displayCount; $i++) {
                $action = $installs[$i]
                $ref = if ($action.action.ref) { $action.action.ref } else { $action.action.id }
                $output += "  - $ref"
            }
            if ($installs.Count -gt 10) {
                $output += "  ... and $($installs.Count - 10) more"
            }
        }
    }
    
    # Version info
    $output += ""
    $output += "Version Info:"
    $output += "  Endstate: $(Get-ReportVersion)"
    $gitSha = Get-ReportGitSha
    if ($gitSha) {
        $output += "  Git SHA: $gitSha"
    }
    
    # File paths
    $output += ""
    $output += "Files:"
    if ($State._filePath) {
        $output += "  State: $($State._filePath)"
    }
    
    # Try to find corresponding log file
    if ($State.runId) {
        $logDir = Join-Path (Split-Path (Split-Path $State._filePath)) "logs"
        $possibleLogs = @(
            (Join-Path $logDir "apply-$($State.runId).log"),
            (Join-Path $logDir "apply-from-plan-$($State.runId).log"),
            (Join-Path $logDir "$($State.runId).log")
        )
        foreach ($logPath in $possibleLogs) {
            if (Test-Path $logPath) {
                $output += "  Log: $logPath"
                break
            }
        }
    }
    
    return $output -join "`n"
}

function Format-ReportCompact {
    <#
    .SYNOPSIS
        Format a run for compact list view (one line per run).
    .PARAMETER State
        The state object to format.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [object]$State
    )
    
    $timestamp = if ($State.timestamp) { $State.timestamp } else { "unknown" }
    $runId = if ($State.runId) { $State.runId } else { "unknown" }
    $manifestName = if ($State.manifest.path) { Split-Path -Leaf $State.manifest.path } else { "unknown" }
    
    $success = if ($State.summary.success) { $State.summary.success } else { 0 }
    $skipped = if ($State.summary.skipped) { $State.summary.skipped } else { 0 }
    $failed = if ($State.summary.failed) { $State.summary.failed } else { 0 }
    
    $dryRunTag = if ($State.dryRun) { "[DRY]" } else { "" }
    
    return "$timestamp | $runId | $manifestName | $success/$skipped/$failed $dryRunTag"
}

function Format-ReportJson {
    <#
    .SYNOPSIS
        Format report(s) as JSON for machine-readable output using standard envelope.
    .PARAMETER States
        Array of state objects.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [array]$States
    )
    
    # Import json-output module for envelope
    . "$PSScriptRoot\json-output.ps1"
    
    $reports = @()
    foreach ($state in $States) {
        $report = [ordered]@{
            runId = $state.runId
            timestamp = $state.timestamp
            command = $state.command
            dryRun = $state.dryRun
            manifest = [ordered]@{
                name = if ($state.manifest.path) { Split-Path -Leaf $state.manifest.path } else { $null }
                path = $state.manifest.path
                hash = $state.manifest.hash
            }
            summary = [ordered]@{
                success = if ($state.summary.success) { $state.summary.success } else { 0 }
                skipped = if ($state.summary.skipped) { $state.summary.skipped } else { 0 }
                failed = if ($state.summary.failed) { $state.summary.failed } else { 0 }
            }
            stateFile = $state._filePath
        }
        $reports += $report
    }
    
    $data = [ordered]@{
        reports = $reports
    }
    
    $envelope = New-JsonEnvelope -Command "report" -Success $true -Data $data
    return ConvertTo-JsonOutput -Envelope $envelope
}

function Write-ReportHuman {
    <#
    .SYNOPSIS
        Write human-readable report to console with colors.
    .PARAMETER States
        Array of state objects.
    .PARAMETER Compact
        Use compact one-line-per-run format.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [array]$States,
        
        [Parameter(Mandatory = $false)]
        [switch]$Compact
    )
    
    if ($Compact) {
        Write-Host ""
        Write-Host "Recent Runs:" -ForegroundColor Cyan
        Write-Host "timestamp | runId | manifest | success/skipped/failed" -ForegroundColor DarkGray
        Write-Host ("-" * 70) -ForegroundColor DarkGray
        
        foreach ($state in $States) {
            $line = Format-ReportCompact -State $state
            $color = if ($state.summary.failed -gt 0) { "Yellow" } else { "White" }
            Write-Host $line -ForegroundColor $color
        }
        Write-Host ""
    } else {
        foreach ($state in $States) {
            $output = Format-ReportHuman -State $state -ShowDetails
            
            # Print with colors
            $lines = $output -split "`n"
            foreach ($line in $lines) {
                if ($line -match "^Run:") {
                    Write-Host $line -ForegroundColor Cyan
                } elseif ($line -match "^\s+Succeeded:") {
                    Write-Host $line -ForegroundColor Green
                } elseif ($line -match "^\s+Failed:" -and $line -notmatch ": 0$") {
                    Write-Host $line -ForegroundColor Red
                } elseif ($line -match "^Failed Actions") {
                    Write-Host $line -ForegroundColor Yellow
                } elseif ($line -match "^\s+-.*:") {
                    Write-Host $line -ForegroundColor DarkGray
                } else {
                    Write-Host $line
                }
            }
        }
    }
}

# Functions exported: Get-StateFiles, Read-StateFile, Get-ProvisioningReport, Format-ReportHuman, Format-ReportCompact, Format-ReportJson, Write-ReportHuman
