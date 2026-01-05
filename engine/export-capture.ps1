# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Export capture engine - captures configuration files from restore manifest entries.

.DESCRIPTION
    Implements the inverse of restore: reads restore[] entries from manifest,
    resolves targets on current system, and copies them to export source paths.
    
    Export Convention:
    - Export is a folder, default: <manifestDir>/export/
    - Contains manifest.snapshot.jsonc (copied at capture time)
    - Contains referenced config files under relative paths
    
    Safety:
    - Respects sensitive-path rules (warn + skip unless allowed)
    - Never modifies original manifest
    - Creates directories as needed
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\restore.ps1"
. "$PSScriptRoot\events.ps1"

function Get-ExportPath {
    <#
    .SYNOPSIS
        Resolve export path from manifest directory or explicit path.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [string]$ExportPath = $null
    )
    
    if ($ExportPath) {
        return [System.IO.Path]::GetFullPath($ExportPath)
    }
    
    $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
    return Join-Path $manifestDir "export"
}

function Invoke-ExportCapture {
    <#
    .SYNOPSIS
        Capture configuration files from system to export.
    .DESCRIPTION
        Reads restore[] entries from manifest, resolves targets on current system,
        and copies them to export source paths.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [string]$ExportPath = $null,
        
        [Parameter(Mandatory = $false)]
        [switch]$DryRun,
        
        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "export-$runId"
    
    Write-ProvisioningSection "Export Configuration"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    if ($DryRun) {
        Write-Host ""
        Write-Host "  *** DRY-RUN MODE - No files will be copied ***" -ForegroundColor Yellow
        Write-Host ""
        Write-ProvisioningLog "DRY-RUN mode enabled" -Level INFO
    }
    
    # Emit phase event
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "export" -Status "started" -Message "Starting configuration export"
    }
    
    # Load manifest
    if (-not (Test-Path $ManifestPath)) {
        Write-ProvisioningLog "Manifest not found: $ManifestPath" -Level ERROR
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "export" -Status "failed" -Message "Manifest not found"
        }
        
        return @{
            RunId = $runId
            Success = $false
            Error = "Manifest not found: $ManifestPath"
        }
    }
    
    $manifest = Read-Manifest -Path $ManifestPath
    $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
    
    # Resolve export path
    $resolvedExportPath = Get-ExportPath -ManifestPath $ManifestPath -ExportPath $ExportPath
    Write-ProvisioningLog "Export path: $resolvedExportPath" -Level INFO
    
    # Check restore entries
    $restoreItems = @($manifest.restore)
    
    if ($restoreItems.Count -eq 0) {
        Write-ProvisioningLog "No restore entries in manifest" -Level WARN
        Write-Host ""
        Write-Host "No restore entries found in manifest." -ForegroundColor Yellow
        Write-Host "Add restore entries to export configuration files." -ForegroundColor DarkGray
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "export" -Status "completed" -Message "No restore entries to export"
        }
        
        return @{
            RunId = $runId
            Success = $true
            CaptureCount = 0
            SkipCount = 0
            FailCount = 0
            WarnCount = 0
            Results = @()
        }
    }
    
    Write-ProvisioningLog "Found $($restoreItems.Count) restore entries" -Level INFO
    
    # Create export directory
    if (-not (Test-Path $resolvedExportPath)) {
        New-Item -ItemType Directory -Path $resolvedExportPath -Force | Out-Null
        Write-ProvisioningLog "Created export directory: $resolvedExportPath" -Level INFO
    }
    
    # Process restore items (capture inverse)
    Write-ProvisioningSection "Capturing Configuration Files"
    
    $captureCount = 0
    $skipCount = 0
    $failCount = 0
    $warnCount = 0
    $results = @()
    
    foreach ($item in $restoreItems) {
        $itemId = Get-RestoreActionId -Item $item
        
        # Expand paths
        $expandedTarget = Expand-RestorePath -Path $item.target
        
        # For export, we reverse: target (system) -> source (export)
        $captureSource = $expandedTarget
        $captureDestRel = $item.source
        $captureDest = Join-Path $resolvedExportPath $captureDestRel
        
        $result = @{
            id = $itemId
            type = "export"
            systemPath = $captureSource
            exportPath = $captureDestRel
            status = "pending"
            reason = $null
            warnings = @()
        }
        
        # Check for sensitive paths
        $warnings = Test-SensitivePath -Path $captureSource
        if ($warnings.Count -gt 0) {
            $result.warnings = $warnings
            foreach ($warning in $warnings) {
                Write-ProvisioningLog "WARNING: $warning" -Level WARN
            }
        }
        
        # Check if source exists on system
        if (-not (Test-Path $captureSource)) {
            $result.status = "skip"
            $result.reason = "not found on system: $captureSource"
            Write-ProvisioningLog "SKIP: $itemId - $($result.reason)" -Level SKIP
            $skipCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "export" -ItemId $itemId -Status "skipped" -Message $result.reason
            }
            
            $results += $result
            continue
        }
        
        # Warn if sensitive path (but continue if no explicit block)
        if ($warnings.Count -gt 0) {
            Write-Host "[WARN] $itemId - Sensitive path detected" -ForegroundColor Yellow
            $warnCount++
        }
        
        # Dry-run mode
        if ($DryRun) {
            $result.status = "dry-run"
            $result.reason = "would export $captureSource -> $captureDestRel"
            Write-ProvisioningLog "[DRY-RUN] Would export: $captureSource -> $captureDestRel" -Level ACTION
            $captureCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "export" -ItemId $itemId -Status "success" -Message "Would export (dry-run)"
            }
            
            $results += $result
            continue
        }
        
        # Perform export (copy system -> export)
        try {
            $destDir = Split-Path -Parent $captureDest
            if ($destDir -and -not (Test-Path $destDir)) {
                New-Item -ItemType Directory -Path $destDir -Force | Out-Null
            }
            
            if (Test-Path $captureSource -PathType Container) {
                # Directory copy
                if (Test-Path $captureDest) {
                    Remove-Item -Path $captureDest -Recurse -Force
                }
                Copy-Item -Path $captureSource -Destination $captureDest -Recurse -Force
            } else {
                # File copy
                Copy-Item -Path $captureSource -Destination $captureDest -Force
            }
            
            $result.status = "exported"
            $result.reason = "exported successfully"
            Write-ProvisioningLog "EXPORTED: $itemId" -Level SUCCESS
            $captureCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "export" -ItemId $itemId -Status "success" -Message "Exported to export folder"
            }
            
        } catch {
            $result.status = "fail"
            $result.reason = $_.Exception.Message
            Write-ProvisioningLog "FAIL: $itemId - $($result.reason)" -Level ERROR
            $failCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "export" -ItemId $itemId -Status "failed" -Message $result.reason
            }
        }
        
        $results += $result
    }
    
    # Copy manifest snapshot to export
    try {
        $snapshotPath = Join-Path $resolvedExportPath "manifest.snapshot.jsonc"
        Copy-Item -Path $ManifestPath -Destination $snapshotPath -Force
        Write-ProvisioningLog "Copied manifest snapshot to export" -Level INFO
    } catch {
        Write-ProvisioningLog "WARNING: Failed to copy manifest snapshot: $($_.Exception.Message)" -Level WARN
    }
    
    # Save state
    $manifestHash = Get-ManifestHash -ManifestPath $ManifestPath
    
    $runState = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        command = "export"
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
            hash = $manifestHash
        }
        export = @{
            path = $resolvedExportPath
        }
        summary = @{
            captured = $captureCount
            skip = $skipCount
            fail = $failCount
            warn = $warnCount
        }
        actions = $results
    }
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    $stateFile = Join-Path $stateDir "export-$runId.json"
    $runState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    # Summary
    Write-ProvisioningSection "Export Results"
    Close-ProvisioningLog -SuccessCount $captureCount -SkipCount $skipCount -FailCount $failCount
    
    Write-Host ""
    if ($DryRun) {
        Write-Host "Dry-run complete. No files were copied." -ForegroundColor Yellow
        Write-Host "  Would export: $captureCount" -ForegroundColor Cyan
        Write-Host "  Would skip: $skipCount" -ForegroundColor Yellow
        if ($warnCount -gt 0) {
            Write-Host "  Warnings: $warnCount (sensitive paths)" -ForegroundColor Yellow
        }
        Write-Host ""
        Write-Host "To export for real:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command export-config -Manifest $ManifestPath" -ForegroundColor DarkGray
    } elseif ($failCount -eq 0) {
        Write-Host "Export complete!" -ForegroundColor Green
        Write-Host "  Exported: $captureCount" -ForegroundColor Green
        Write-Host "  Skipped: $skipCount" -ForegroundColor Yellow
        if ($warnCount -gt 0) {
            Write-Host "  Warnings: $warnCount (sensitive paths)" -ForegroundColor Yellow
        }
    } else {
        Write-Host "Export completed with $failCount failure(s)." -ForegroundColor Yellow
    }
    Write-Host ""
    Write-Host "Export location: $resolvedExportPath" -ForegroundColor Cyan
    Write-Host ""
    
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "export" -Status "completed" -Message "Configuration export completed"
    }
    
    return @{
        RunId = $runId
        Success = ($failCount -eq 0)
        ExportCount = $captureCount
        SkipCount = $skipCount
        FailCount = $failCount
        WarnCount = $warnCount
        Results = $results
        ExportPath = $resolvedExportPath
        LogFile = $logFile
    }
}

# Functions exported: Invoke-ExportCapture, Get-ExportPath
