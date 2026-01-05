# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Export revert engine - reverts the last restore operation by restoring backups.

.DESCRIPTION
    Provides explicit revert functionality for restore operations:
    - Finds the most recent restore run with backups
    - Restores backed-up files to their original locations
    - Creates a new backup before reverting (safety layer)
    - Deterministic and reversible
    
    Safety:
    - Only reverts the most recent restore
    - Requires explicit user action
    - Creates backups before reverting
    - No automatic rollback
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\restore.ps1"
. "$PSScriptRoot\events.ps1"

function Get-LastRestoreRun {
    <#
    .SYNOPSIS
        Find the most recent restore run that has backups.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$StateDir = $null
    )
    
    if (-not $StateDir) {
        $StateDir = Join-Path $PSScriptRoot "..\state"
    }
    
    if (-not (Test-Path $StateDir)) {
        return $null
    }
    
    # Find all restore state files
    $restoreStates = Get-ChildItem -Path $StateDir -Filter "restore-*.json" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending
    
    foreach ($stateFile in $restoreStates) {
        try {
            $state = Get-Content -Path $stateFile.FullName -Raw | ConvertFrom-Json
            
            # Check if this restore has backups
            $runId = $state.runId
            $backupDir = Join-Path $StateDir "backups\$runId"
            
            if (Test-Path $backupDir) {
                return @{
                    RunId = $runId
                    StateFile = $stateFile.FullName
                    State = $state
                    BackupDir = $backupDir
                }
            }
        } catch {
            # Skip invalid state files
            continue
        }
    }
    
    return $null
}

function Invoke-ExportRevert {
    <#
    .SYNOPSIS
        Revert the last restore operation by restoring backups.
    .DESCRIPTION
        Finds the most recent restore run with backups and restores them.
        Creates a new backup before reverting for safety.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [switch]$DryRun,
        
        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "revert-$runId"
    
    Write-ProvisioningSection "Revert Last Restore"
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    # Emit phase event
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "revert" -Status "started" -Message "Starting restore revert"
    }
    
    # Find last restore run
    $lastRestore = Get-LastRestoreRun
    
    if (-not $lastRestore) {
        Write-ProvisioningLog "No restore run with backups found" -Level WARN
        Write-Host ""
        Write-Host "No restore operation found to revert." -ForegroundColor Yellow
        Write-Host "Revert only works if a previous restore created backups." -ForegroundColor DarkGray
        Write-Host ""
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "revert" -Status "completed" -Message "No restore to revert"
        }
        
        return @{
            RunId = $runId
            Success = $true
            RevertCount = 0
            SkipCount = 0
            FailCount = 0
            Results = @()
        }
    }
    
    $restoreRunId = $lastRestore.RunId
    $backupDir = $lastRestore.BackupDir
    
    Write-ProvisioningLog "Found restore run to revert: $restoreRunId" -Level INFO
    Write-ProvisioningLog "Backup directory: $backupDir" -Level INFO
    
    Write-Host ""
    Write-Host "Reverting restore run: $restoreRunId" -ForegroundColor Cyan
    Write-Host "Backup location: $backupDir" -ForegroundColor DarkGray
    Write-Host ""
    
    if ($DryRun) {
        Write-Host "  *** DRY-RUN MODE - No changes will be made ***" -ForegroundColor Yellow
        Write-Host ""
    }
    
    # Find all backed-up files
    $backupFiles = Get-ChildItem -Path $backupDir -Recurse -File -ErrorAction SilentlyContinue
    
    if ($backupFiles.Count -eq 0) {
        Write-ProvisioningLog "No backup files found in $backupDir" -Level WARN
        Write-Host "No backup files found to restore." -ForegroundColor Yellow
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "revert" -Status "completed" -Message "No backups to restore"
        }
        
        return @{
            RunId = $runId
            Success = $true
            RevertCount = 0
            SkipCount = 0
            FailCount = 0
            Results = @()
        }
    }
    
    Write-ProvisioningLog "Found $($backupFiles.Count) backup files" -Level INFO
    
    # Process each backup file
    Write-ProvisioningSection "Restoring Backups"
    
    $revertCount = 0
    $skipCount = 0
    $failCount = 0
    $results = @()
    
    foreach ($backupFile in $backupFiles) {
        # Reconstruct original path from backup structure
        $relativePath = $backupFile.FullName.Substring($backupDir.Length).TrimStart('\', '/')
        
        # Reconstruct absolute path
        # Backup structure: backups/<runId>/<drive-letter>/<path>
        # Need to add drive letter back
        $pathParts = $relativePath -split '[/\\]', 2
        if ($pathParts.Count -lt 2) {
            Write-ProvisioningLog "SKIP: Invalid backup path structure: $relativePath" -Level WARN
            $skipCount++
            continue
        }
        
        $driveLetter = $pathParts[0]
        $pathWithoutDrive = $pathParts[1]
        $originalPath = "${driveLetter}:\$pathWithoutDrive"
        
        $result = @{
            id = $relativePath
            type = "revert"
            backupPath = $backupFile.FullName
            originalPath = $originalPath
            status = "pending"
            reason = $null
        }
        
        # Check if backup file exists
        if (-not (Test-Path $backupFile.FullName)) {
            $result.status = "skip"
            $result.reason = "backup file not found"
            Write-ProvisioningLog "SKIP: $relativePath - backup not found" -Level SKIP
            $skipCount++
            $results += $result
            continue
        }
        
        # Dry-run mode
        if ($DryRun) {
            $result.status = "dry-run"
            $result.reason = "would revert $originalPath"
            Write-ProvisioningLog "[DRY-RUN] Would revert: $originalPath" -Level ACTION
            $revertCount++
            $results += $result
            continue
        }
        
        # Create backup of current state before reverting (safety layer)
        if (Test-Path $originalPath) {
            try {
                $revertBackupResult = Backup-RestoreTarget -Target $originalPath -RunId $runId
                if (-not $revertBackupResult.Success) {
                    $result.status = "fail"
                    $result.reason = "failed to backup current state: $($revertBackupResult.Error)"
                    Write-ProvisioningLog "FAIL: $relativePath - $($result.reason)" -Level ERROR
                    $failCount++
                    
                    if ($EventsFormat -eq "jsonl") {
                        Emit-ItemEvent -Phase "revert" -ItemId $relativePath -Status "failed" -Message $result.reason
                    }
                    
                    $results += $result
                    continue
                }
            } catch {
                $result.status = "fail"
                $result.reason = "failed to backup current state: $($_.Exception.Message)"
                Write-ProvisioningLog "FAIL: $relativePath - $($result.reason)" -Level ERROR
                $failCount++
                $results += $result
                continue
            }
        }
        
        # Restore backup to original location
        try {
            $targetDir = Split-Path -Parent $originalPath
            if ($targetDir -and -not (Test-Path $targetDir)) {
                New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            }
            
            Copy-Item -Path $backupFile.FullName -Destination $originalPath -Force
            
            $result.status = "reverted"
            $result.reason = "restored from backup"
            Write-ProvisioningLog "REVERTED: $originalPath" -Level SUCCESS
            $revertCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "revert" -ItemId $relativePath -Status "success" -Message "Reverted successfully"
            }
            
        } catch {
            $result.status = "fail"
            $result.reason = $_.Exception.Message
            Write-ProvisioningLog "FAIL: $relativePath - $($result.reason)" -Level ERROR
            $failCount++
            
            if ($EventsFormat -eq "jsonl") {
                Emit-ItemEvent -Phase "revert" -ItemId $relativePath -Status "failed" -Message $result.reason
            }
        }
        
        $results += $result
    }
    
    # Save state
    $runState = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        command = "revert"
        dryRun = $DryRun.IsPresent
        revertedRestoreRunId = $restoreRunId
        summary = @{
            reverted = $revertCount
            skip = $skipCount
            fail = $failCount
        }
        actions = $results
    }
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    $stateFile = Join-Path $stateDir "revert-$runId.json"
    $runState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    # Summary
    Write-ProvisioningSection "Revert Results"
    Close-ProvisioningLog -SuccessCount $revertCount -SkipCount $skipCount -FailCount $failCount
    
    Write-Host ""
    if ($DryRun) {
        Write-Host "Dry-run complete. No changes were made." -ForegroundColor Yellow
        Write-Host ""
        Write-Host "To revert for real:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command revert"
    } elseif ($failCount -eq 0) {
        Write-Host "Revert complete!" -ForegroundColor Green
        Write-Host "  Reverted: $revertCount files" -ForegroundColor Green
        Write-Host "  Skipped: $skipCount" -ForegroundColor Yellow
    } else {
        Write-Host "Revert completed with $failCount failure(s)." -ForegroundColor Yellow
    }
    Write-Host ""
    
    if ($EventsFormat -eq "jsonl") {
        Emit-PhaseEvent -Phase "revert" -Status "completed" -Message "Restore revert completed"
    }
    
    return @{
        RunId = $runId
        Success = ($failCount -eq 0)
        RevertCount = $revertCount
        SkipCount = $skipCount
        FailCount = $failCount
        Results = $results
        RevertedRestoreRunId = $restoreRunId
        LogFile = $logFile
    }
}

# Functions exported: Invoke-ExportRevert, Get-LastRestoreRun
