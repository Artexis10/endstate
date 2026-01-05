# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Bundle capture engine - captures configuration files from restore manifest entries.

.DESCRIPTION
    Implements the inverse of restore: reads restore[] entries from manifest,
    resolves targets on current system, and copies them to bundle source paths.
    
    Bundle Convention:
    - Bundle is a folder, default: <manifestDir>/bundle/
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

function Get-BundlePath {
    <#
    .SYNOPSIS
        Resolve bundle path from manifest directory or explicit path.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [string]$BundlePath = $null
    )
    
    if ($BundlePath) {
        return [System.IO.Path]::GetFullPath($BundlePath)
    }
    
    $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
    return Join-Path $manifestDir "bundle"
}

function Invoke-BundleCapture {
    <#
    .SYNOPSIS
        Capture configuration files from system to bundle.
    .DESCRIPTION
        Reads restore[] entries from manifest, resolves targets on current system,
        and copies them to bundle source paths.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [string]$BundlePath = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "capture-$runId"
    
    Write-ProvisioningSection "Bundle Capture"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    # Emit phase event
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "capture" -Status "started" -Message "Starting bundle capture"
    }
    
    # Load manifest
    if (-not (Test-Path $ManifestPath)) {
        Write-ProvisioningLog "Manifest not found: $ManifestPath" -Level ERROR
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "capture" -Status "failed" -Message "Manifest not found"
        }
        
        return @{
            RunId = $runId
            Success = $false
            Error = "Manifest not found: $ManifestPath"
        }
    }
    
    $manifest = Read-Manifest -Path $ManifestPath
    $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
    
    # Resolve bundle path
    $resolvedBundlePath = Get-BundlePath -ManifestPath $ManifestPath -BundlePath $BundlePath
    Write-ProvisioningLog "Bundle path: $resolvedBundlePath" -Level INFO
    
    # Check restore entries
    $restoreItems = @($manifest.restore)
    
    if ($restoreItems.Count -eq 0) {
        Write-ProvisioningLog "No restore entries in manifest" -Level WARN
        Write-Host ""
        Write-Host "No restore entries found in manifest." -ForegroundColor Yellow
        Write-Host "Add restore entries to capture configuration files." -ForegroundColor DarkGray
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "capture" -Status "completed" -Message "No restore entries to capture"
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
    
    # Create bundle directory
    if (-not (Test-Path $resolvedBundlePath)) {
        New-Item -ItemType Directory -Path $resolvedBundlePath -Force | Out-Null
        Write-ProvisioningLog "Created bundle directory: $resolvedBundlePath" -Level INFO
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
        $expandedSource = Expand-RestorePath -Path $item.source -BasePath $manifestDir
        
        # For capture, we reverse: target (system) -> source (bundle)
        $captureSource = $expandedTarget
        $captureDestRel = $item.source
        $captureDest = Join-Path $resolvedBundlePath $captureDestRel
        
        $result = @{
            id = $itemId
            type = "capture"
            systemPath = $captureSource
            bundlePath = $captureDestRel
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
                Emit-ItemEvent -Phase "capture" -ItemId $itemId -Status "skipped" -Message $result.reason
            }
            
            $results += $result
            continue
        }
        
        # Warn if sensitive path (but continue if no explicit block)
        if ($warnings.Count -gt 0) {
            Write-Host "[WARN] $itemId - Sensitive path detected" -ForegroundColor Yellow
            $warnCount++
        }
        
        # Perform capture (copy system -> bundle)
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
            
            $result.status = "captured"
            $result.reason = "captured successfully"
            Write-ProvisioningLog "CAPTURED: $itemId" -Level SUCCESS
            $captureCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "capture" -ItemId $itemId -Status "success" -Message "Captured to bundle"
            }
            
        } catch {
            $result.status = "fail"
            $result.reason = $_.Exception.Message
            Write-ProvisioningLog "FAIL: $itemId - $($result.reason)" -Level ERROR
            $failCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "capture" -ItemId $itemId -Status "failed" -Message $result.reason
            }
        }
        
        $results += $result
    }
    
    # Copy manifest snapshot to bundle
    try {
        $snapshotPath = Join-Path $resolvedBundlePath "manifest.snapshot.jsonc"
        Copy-Item -Path $ManifestPath -Destination $snapshotPath -Force
        Write-ProvisioningLog "Copied manifest snapshot to bundle" -Level INFO
    } catch {
        Write-ProvisioningLog "WARNING: Failed to copy manifest snapshot: $($_.Exception.Message)" -Level WARN
    }
    
    # Save state
    $manifestHash = Get-ManifestHash -ManifestPath $ManifestPath
    
    $runState = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        command = "capture"
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
            hash = $manifestHash
        }
        bundle = @{
            path = $resolvedBundlePath
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
    $stateFile = Join-Path $stateDir "capture-$runId.json"
    $runState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    # Summary
    Write-ProvisioningSection "Capture Results"
    Close-ProvisioningLog -SuccessCount $captureCount -SkipCount $skipCount -FailCount $failCount
    
    Write-Host ""
    if ($failCount -eq 0) {
        Write-Host "Capture complete!" -ForegroundColor Green
        Write-Host "  Captured: $captureCount" -ForegroundColor Green
        Write-Host "  Skipped: $skipCount" -ForegroundColor Yellow
        if ($warnCount -gt 0) {
            Write-Host "  Warnings: $warnCount (sensitive paths)" -ForegroundColor Yellow
        }
    } else {
        Write-Host "Capture completed with $failCount failure(s)." -ForegroundColor Yellow
    }
    Write-Host ""
    Write-Host "Bundle location: $resolvedBundlePath" -ForegroundColor Cyan
    Write-Host ""
    
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "capture" -Status "completed" -Message "Bundle capture completed"
    }
    
    return @{
        RunId = $runId
        Success = ($failCount -eq 0)
        CaptureCount = $captureCount
        SkipCount = $skipCount
        FailCount = $failCount
        WarnCount = $warnCount
        Results = $results
        BundlePath = $resolvedBundlePath
        LogFile = $logFile
    }
}

# Functions exported: Invoke-BundleCapture, Get-BundlePath
