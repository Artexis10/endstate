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

function Get-LastRestoreJournal {
    <#
    .SYNOPSIS
        Find the most recent restore journal.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$LogsDir = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$ManifestPath = $null
    )
    
    if (-not $LogsDir) {
        $LogsDir = Join-Path $PSScriptRoot "..\logs"
    }
    
    if (-not (Test-Path $LogsDir)) {
        return $null
    }
    
    # Find all restore journal files
    $journalFiles = Get-ChildItem -Path $LogsDir -Filter "restore-journal-*.json" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending
    
    foreach ($journalFile in $journalFiles) {
        try {
            $journal = Get-Content -Path $journalFile.FullName -Raw | ConvertFrom-Json
            
            # If manifest path specified, try to match it
            if ($ManifestPath) {
                $normalizedManifest = [System.IO.Path]::GetFullPath($ManifestPath)
                $normalizedJournalManifest = [System.IO.Path]::GetFullPath($journal.manifestPath)
                
                if ($normalizedManifest -eq $normalizedJournalManifest) {
                    return @{
                        JournalFile = $journalFile.FullName
                        Journal = $journal
                    }
                }
            } else {
                # Return most recent journal
                return @{
                    JournalFile = $journalFile.FullName
                    Journal = $journal
                }
            }
        } catch {
            # Skip invalid journal files
            continue
        }
    }
    
    return $null
}

function Invoke-ExportRevert {
    <#
    .SYNOPSIS
        Revert the last restore operation using journal.
    .DESCRIPTION
        Finds the most recent restore journal and reverts changes:
        - Restores backed-up files to their original locations
        - Deletes targets that were created by restore (targetExistedBefore=false)
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
    
    # Find last restore journal
    $lastJournal = Get-LastRestoreJournal
    
    if (-not $lastJournal) {
        Write-ProvisioningLog "No restore journal found" -Level WARN
        Write-Host ""
        Write-Host "No restore operation found to revert." -ForegroundColor Yellow
        Write-Host "Revert requires a restore journal from a previous restore." -ForegroundColor DarkGray
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
    
    $journal = $lastJournal.Journal
    $journalFile = $lastJournal.JournalFile
    $restoreRunId = $journal.runId
    
    Write-ProvisioningLog "Found restore journal: $journalFile" -Level INFO
    Write-ProvisioningLog "Restore run ID: $restoreRunId" -Level INFO
    
    Write-Host ""
    Write-Host "Reverting restore run: $restoreRunId" -ForegroundColor Cyan
    Write-Host "Journal: $journalFile" -ForegroundColor DarkGray
    Write-Host ""
    
    if ($DryRun) {
        Write-Host "  *** DRY-RUN MODE - No changes will be made ***" -ForegroundColor Yellow
        Write-Host ""
    }
    
    # Get journal entries
    $journalEntries = @($journal.entries)
    
    if ($journalEntries.Count -eq 0) {
        Write-ProvisioningLog "No entries in journal" -Level WARN
        Write-Host "No entries found in journal." -ForegroundColor Yellow
        
        if ($EventsFormat -eq "jsonl") {
            Emit-PhaseEvent -Phase "revert" -Status "completed" -Message "No entries to revert"
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
    
    Write-ProvisioningLog "Processing $($journalEntries.Count) journal entries" -Level INFO
    
    # Process journal entries in REVERSE order
    Write-ProvisioningSection "Reverting Restore Operations"
    
    $revertCount = 0
    $skipCount = 0
    $failCount = 0
    $results = @()
    
    # Reverse the entries to undo in reverse order
    [array]::Reverse($journalEntries)
    
    foreach ($entry in $journalEntries) {
        $entryId = "$($entry.source) -> $($entry.target)"
        $targetPath = $entry.targetPath
        
        $result = @{
            id = $entryId
            type = "revert"
            targetPath = $targetPath
            status = "pending"
            reason = $null
        }
        
        # Only revert entries that were actually restored
        if ($entry.action -ne "restored") {
            $result.status = "skip"
            $result.reason = "entry was not restored (action: $($entry.action))"
            Write-ProvisioningLog "SKIP: $entryId - $($result.reason)" -Level SKIP
            $skipCount++
            $results += $result
            continue
        }
        
        # Dry-run mode
        if ($DryRun) {
            if ($entry.backupCreated -and $entry.backupPath) {
                $result.status = "dry-run"
                $result.reason = "would restore backup: $($entry.backupPath) -> $targetPath"
                Write-ProvisioningLog "[DRY-RUN] Would restore backup: $targetPath" -Level ACTION
            } elseif (-not $entry.targetExistedBefore) {
                $result.status = "dry-run"
                $result.reason = "would delete created target: $targetPath"
                Write-ProvisioningLog "[DRY-RUN] Would delete: $targetPath" -Level ACTION
            } else {
                $result.status = "skip"
                $result.reason = "no backup and target existed before (nothing to revert)"
                Write-ProvisioningLog "[DRY-RUN] SKIP: $entryId - $($result.reason)" -Level SKIP
                $skipCount++
                $results += $result
                continue
            }
            $revertCount++
            $results += $result
            continue
        }
        
        # CASE 1: Backup exists - restore it
        if ($entry.backupCreated -and $entry.backupPath -and (Test-Path $entry.backupPath)) {
            try {
                # Create safety backup of current state
                if (Test-Path $targetPath) {
                    $safetyBackup = Backup-RestoreTarget -Target $targetPath -RunId $runId
                    if (-not $safetyBackup.Success) {
                        $result.status = "fail"
                        $result.reason = "failed to create safety backup: $($safetyBackup.Error)"
                        Write-ProvisioningLog "FAIL: $entryId - $($result.reason)" -Level ERROR
                        $failCount++
                        $results += $result
                        continue
                    }
                }
                
                # Restore backup
                $targetDir = Split-Path -Parent $targetPath
                if ($targetDir -and -not (Test-Path $targetDir)) {
                    New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
                }
                
                if (Test-Path $entry.backupPath -PathType Container) {
                    if (Test-Path $targetPath) {
                        Remove-Item -Path $targetPath -Recurse -Force
                    }
                    Copy-Item -Path $entry.backupPath -Destination $targetPath -Recurse -Force
                } else {
                    Copy-Item -Path $entry.backupPath -Destination $targetPath -Force
                }
                
                $result.status = "reverted"
                $result.reason = "restored from backup"
                Write-ProvisioningLog "REVERTED (backup): $targetPath" -Level SUCCESS
                $revertCount++
                
                if ($EventsFormat -eq "jsonl") {
                    Emit-ItemEvent -Phase "revert" -ItemId $entryId -Status "success" -Message "Restored from backup"
                }
            } catch {
                $result.status = "fail"
                $result.reason = $_.Exception.Message
                Write-ProvisioningLog "FAIL: $entryId - $($result.reason)" -Level ERROR
                $failCount++
                
                if ($EventsFormat -eq "jsonl") {
                    Emit-ItemEvent -Phase "revert" -ItemId $entryId -Status "failed" -Message $result.reason
                }
            }
            
            $results += $result
            continue
        }
        
        # CASE 2: Target was created by restore (didn't exist before) - delete it
        if (-not $entry.targetExistedBefore) {
            if (-not (Test-Path $targetPath)) {
                $result.status = "skip"
                $result.reason = "target no longer exists"
                Write-ProvisioningLog "SKIP: $entryId - target already deleted" -Level SKIP
                $skipCount++
                $results += $result
                continue
            }
            
            try {
                # Create safety backup before deleting
                $safetyBackup = Backup-RestoreTarget -Target $targetPath -RunId $runId
                if (-not $safetyBackup.Success) {
                    $result.status = "fail"
                    $result.reason = "failed to create safety backup before deletion: $($safetyBackup.Error)"
                    Write-ProvisioningLog "FAIL: $entryId - $($result.reason)" -Level ERROR
                    $failCount++
                    $results += $result
                    continue
                }
                
                # Delete the target
                if (Test-Path $targetPath -PathType Container) {
                    Remove-Item -Path $targetPath -Recurse -Force
                } else {
                    Remove-Item -Path $targetPath -Force
                }
                
                $result.status = "reverted"
                $result.reason = "deleted created target"
                Write-ProvisioningLog "REVERTED (deleted): $targetPath" -Level SUCCESS
                $revertCount++
                
                if ($EventsFormat -eq "jsonl") {
                    Emit-ItemEvent -Phase "revert" -ItemId $entryId -Status "success" -Message "Deleted created target"
                }
            } catch {
                $result.status = "fail"
                $result.reason = $_.Exception.Message
                Write-ProvisioningLog "FAIL: $entryId - $($result.reason)" -Level ERROR
                $failCount++
                
                if ($EventsFormat -eq "jsonl") {
                    Emit-ItemEvent -Phase "revert" -ItemId $entryId -Status "failed" -Message $result.reason
                }
            }
            
            $results += $result
            continue
        }
        
        # CASE 3: No backup and target existed before - nothing to revert
        $result.status = "skip"
        $result.reason = "no backup available and target existed before restore"
        Write-ProvisioningLog "SKIP: $entryId - $($result.reason)" -Level SKIP
        $skipCount++
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

# Functions exported: Invoke-ExportRevert, Get-LastRestoreJournal
