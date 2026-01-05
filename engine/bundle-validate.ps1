# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Bundle validation engine - validates bundle integrity before restore.

.DESCRIPTION
    Validates that a bundle is complete and ready for restore:
    - All restore[].source paths exist in bundle
    - Targets are writable (or require elevation)
    - Snapshot manifest exists (warn if missing)
    - Snapshot mismatch with active manifest (warn)
    
    Fails fast on missing sources.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\restore.ps1"
. "$PSScriptRoot\events.ps1"

function Test-PathWritable {
    <#
    .SYNOPSIS
        Check if a path is writable by current user.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    # If path exists, test write access
    if (Test-Path $Path) {
        try {
            $testFile = Join-Path $Path ".endstate-write-test-$(Get-Random)"
            if (Test-Path $Path -PathType Container) {
                New-Item -ItemType File -Path $testFile -Force | Out-Null
                Remove-Item -Path $testFile -Force
                return $true
            } else {
                # For files, test parent directory
                $parentDir = Split-Path -Parent $Path
                $testFile = Join-Path $parentDir ".endstate-write-test-$(Get-Random)"
                New-Item -ItemType File -Path $testFile -Force | Out-Null
                Remove-Item -Path $testFile -Force
                return $true
            }
        } catch {
            return $false
        }
    } else {
        # Path doesn't exist, test parent directory
        $parentDir = Split-Path -Parent $Path
        if (-not $parentDir) {
            return $false
        }
        
        if (-not (Test-Path $parentDir)) {
            # Parent doesn't exist either, recurse
            return Test-PathWritable -Path $parentDir
        }
        
        try {
            $testFile = Join-Path $parentDir ".endstate-write-test-$(Get-Random)"
            New-Item -ItemType File -Path $testFile -Force | Out-Null
            Remove-Item -Path $testFile -Force
            return $true
        } catch {
            return $false
        }
    }
}

function Invoke-BundleValidate {
    <#
    .SYNOPSIS
        Validate bundle integrity and readiness for restore.
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
    $logFile = Initialize-ProvisioningLog -RunId "validate-$runId"
    
    Write-ProvisioningSection "Bundle Validation"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    # Emit phase event
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "validate-bundle" -Status "started" -Message "Starting bundle validation"
    }
    
    # Load manifest
    if (-not (Test-Path $ManifestPath)) {
        Write-ProvisioningLog "Manifest not found: $ManifestPath" -Level ERROR
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "validate-bundle" -Status "failed" -Message "Manifest not found"
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
    . "$PSScriptRoot\bundle-capture.ps1"
    $resolvedBundlePath = Get-BundlePath -ManifestPath $ManifestPath -BundlePath $BundlePath
    Write-ProvisioningLog "Bundle path: $resolvedBundlePath" -Level INFO
    
    # Check bundle exists
    if (-not (Test-Path $resolvedBundlePath)) {
        Write-ProvisioningLog "Bundle directory not found: $resolvedBundlePath" -Level ERROR
        Write-Host "[ERROR] Bundle directory not found: $resolvedBundlePath" -ForegroundColor Red
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "validate-bundle" -Status "failed" -Message "Bundle directory not found"
        }
        
        return @{
            RunId = $runId
            Success = $false
            Error = "Bundle directory not found: $resolvedBundlePath"
        }
    }
    
    # Check restore entries
    $restoreItems = @($manifest.restore)
    
    if ($restoreItems.Count -eq 0) {
        Write-ProvisioningLog "No restore entries in manifest" -Level WARN
        Write-Host ""
        Write-Host "No restore entries found in manifest." -ForegroundColor Yellow
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "validate-bundle" -Status "completed" -Message "No restore entries to validate"
        }
        
        return @{
            RunId = $runId
            Success = $true
            ValidCount = 0
            WarnCount = 0
            FailCount = 0
            Results = @()
        }
    }
    
    Write-ProvisioningLog "Validating $($restoreItems.Count) restore entries" -Level INFO
    
    # Validate snapshot manifest
    $snapshotPath = Join-Path $resolvedBundlePath "manifest.snapshot.jsonc"
    $snapshotWarnings = @()
    
    if (-not (Test-Path $snapshotPath)) {
        $warning = "Snapshot manifest not found in bundle (manifest.snapshot.jsonc)"
        $snapshotWarnings += $warning
        Write-ProvisioningLog "WARNING: $warning" -Level WARN
    } else {
        # Compare snapshot with active manifest
        try {
            $snapshotManifest = Read-Manifest -Path $snapshotPath
            $activeHash = Get-ManifestHash -ManifestPath $ManifestPath
            $snapshotHash = Get-ManifestHash -ManifestPath $snapshotPath
            
            if ($activeHash -ne $snapshotHash) {
                $warning = "Snapshot manifest differs from active manifest (hashes: snapshot=$snapshotHash, active=$activeHash)"
                $snapshotWarnings += $warning
                Write-ProvisioningLog "WARNING: $warning" -Level WARN
            } else {
                Write-ProvisioningLog "Snapshot manifest matches active manifest" -Level INFO
            }
        } catch {
            $warning = "Failed to compare snapshot manifest: $($_.Exception.Message)"
            $snapshotWarnings += $warning
            Write-ProvisioningLog "WARNING: $warning" -Level WARN
        }
    }
    
    # Validate each restore entry
    Write-ProvisioningSection "Validating Restore Entries"
    
    $validCount = 0
    $warnCount = 0
    $failCount = 0
    $results = @()
    
    foreach ($item in $restoreItems) {
        $itemId = Get-RestoreActionId -Item $item
        
        # Expand paths
        $expandedTarget = Expand-RestorePath -Path $item.target
        $expandedSource = Expand-RestorePath -Path $item.source -BasePath $resolvedBundlePath
        
        $result = @{
            id = $itemId
            type = "validate"
            bundleSource = $item.source
            systemTarget = $expandedTarget
            status = "pending"
            reason = $null
            warnings = @()
        }
        
        # Check source exists in bundle
        if (-not (Test-Path $expandedSource)) {
            $result.status = "fail"
            $result.reason = "source not found in bundle: $expandedSource"
            Write-ProvisioningLog "FAIL: $itemId - $($result.reason)" -Level ERROR
            $failCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "validate-bundle" -ItemId $itemId -Status "failed" -Message $result.reason
            }
            
            $results += $result
            continue
        }
        
        # Check target writability
        $isWritable = Test-PathWritable -Path $expandedTarget
        $isElevated = Test-IsElevated
        $requiresAdmin = if ($item.requiresAdmin) { $item.requiresAdmin } else { $false }
        
        if (-not $isWritable) {
            if ($requiresAdmin -and -not $isElevated) {
                $result.warnings += "Target requires elevation (run as Administrator): $expandedTarget"
                Write-ProvisioningLog "WARN: $itemId - Target requires elevation" -Level WARN
                $warnCount++
            } elseif (-not $requiresAdmin) {
                $result.warnings += "Target may not be writable: $expandedTarget"
                Write-ProvisioningLog "WARN: $itemId - Target may not be writable" -Level WARN
                $warnCount++
            }
        }
        
        # Check for sensitive paths
        $sensitiveWarnings = Test-SensitivePath -Path $expandedTarget
        if ($sensitiveWarnings.Count -gt 0) {
            $result.warnings += $sensitiveWarnings
            foreach ($warning in $sensitiveWarnings) {
                Write-ProvisioningLog "WARN: $warning" -Level WARN
            }
            $warnCount++
        }
        
        # Validation passed
        $result.status = "valid"
        $result.reason = "source exists, target accessible"
        Write-ProvisioningLog "VALID: $itemId" -Level SUCCESS
        $validCount++
        
        if ($EventsFormat -eq "jsonl") {
            $msg = if ($result.warnings.Count -gt 0) { "Valid with warnings" } else { "Valid" }
            Emit-ItemEvent -Phase "validate-bundle" -ItemId $itemId -Status "success" -Message $msg
        }
        
        $results += $result
    }
    
    # Save state
    $manifestHash = Get-ManifestHash -ManifestPath $ManifestPath
    
    $runState = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        command = "validate-bundle"
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
            hash = $manifestHash
        }
        bundle = @{
            path = $resolvedBundlePath
            snapshotWarnings = $snapshotWarnings
        }
        summary = @{
            valid = $validCount
            warn = $warnCount
            fail = $failCount
        }
        actions = $results
    }
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    $stateFile = Join-Path $stateDir "validate-$runId.json"
    $runState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    # Summary
    Write-ProvisioningSection "Validation Results"
    
    if ($snapshotWarnings.Count -gt 0) {
        Write-Host ""
        Write-Host "Snapshot Warnings:" -ForegroundColor Yellow
        foreach ($warning in $snapshotWarnings) {
            Write-Host "  - $warning" -ForegroundColor Yellow
        }
    }
    
    Close-ProvisioningLog -SuccessCount $validCount -SkipCount 0 -FailCount $failCount
    
    Write-Host ""
    if ($failCount -eq 0) {
        Write-Host "Bundle validation passed!" -ForegroundColor Green
        Write-Host "  Valid: $validCount" -ForegroundColor Green
        if ($warnCount -gt 0) {
            Write-Host "  Warnings: $warnCount" -ForegroundColor Yellow
        }
        Write-Host ""
        Write-Host "Bundle is ready for restore." -ForegroundColor Cyan
    } else {
        Write-Host "Bundle validation failed!" -ForegroundColor Red
        Write-Host "  Valid: $validCount" -ForegroundColor Green
        Write-Host "  Failed: $failCount" -ForegroundColor Red
        if ($warnCount -gt 0) {
            Write-Host "  Warnings: $warnCount" -ForegroundColor Yellow
        }
        Write-Host ""
        Write-Host "Fix missing sources before attempting restore." -ForegroundColor Yellow
    }
    Write-Host ""
    
    if ($EventsFormat -eq "jsonl") {
        $status = if ($failCount -eq 0) { "completed" } else { "failed" }
        Emit-PhaseEvent -Phase "validate-bundle" -Status $status -Message "Bundle validation $status"
    }
    
    return @{
        RunId = $runId
        Success = ($failCount -eq 0)
        ValidCount = $validCount
        WarnCount = $warnCount
        FailCount = $failCount
        Results = $results
        SnapshotWarnings = $snapshotWarnings
        BundlePath = $resolvedBundlePath
        LogFile = $logFile
    }
}

# Functions exported: Invoke-BundleValidate, Test-PathWritable
