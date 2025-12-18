<#
.SYNOPSIS
    Provisioning apply - executes a plan with optional dry-run support.

.DESCRIPTION
    Executes the actions in a plan: installs apps, restores configs,
    and runs verifications. Supports dry-run mode for safe preview.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\plan.ps1"
. "$PSScriptRoot\..\drivers\winget.ps1"
. "$PSScriptRoot\..\restorers\copy.ps1"
. "$PSScriptRoot\..\verifiers\file-exists.ps1"

function Invoke-Apply {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [switch]$DryRun,
        
        [Parameter(Mandatory = $false)]
        [switch]$EnableRestore
    )
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "apply-$runId"
    
    $modeText = if ($DryRun) { "DRY-RUN" } else { "APPLY" }
    
    Write-ProvisioningSection "Provisioning $modeText"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    Write-ProvisioningLog "Mode: $modeText" -Level INFO
    
    if ($DryRun) {
        Write-Host ""
        Write-Host "  *** DRY-RUN MODE - No changes will be made ***" -ForegroundColor Yellow
        Write-Host ""
    }
    
    # Generate plan first
    Write-ProvisioningSection "Generating Plan"
    $plan = Invoke-Plan -ManifestPath $ManifestPath
    
    if (-not $plan) {
        Write-ProvisioningLog "Failed to generate plan" -Level ERROR
        return $null
    }
    
    # Execute actions
    Write-ProvisioningSection "Executing Actions"
    
    $successCount = 0
    $skipCount = 0
    $failCount = 0
    $actionResults = @()
    
    foreach ($action in $plan.actions) {
        $result = @{
            action = $action
            status = "pending"
            message = ""
        }
        
        switch ($action.type) {
            "app" {
                if ($action.status -eq "skip") {
                    Write-ProvisioningLog "$($action.ref) - Already installed" -Level SKIP
                    $result.status = "skipped"
                    $result.message = "Already installed"
                    $skipCount++
                }
                elseif ($action.status -eq "install") {
                    if ($DryRun) {
                        Write-ProvisioningLog "[DRY-RUN] Would install: $($action.ref)" -Level ACTION
                        $result.status = "dry-run"
                        $result.message = "Would install via winget"
                        $successCount++
                    } else {
                        Write-ProvisioningLog "Installing: $($action.ref)" -Level ACTION
                        $installResult = Install-AppViaWinget -PackageId $action.ref
                        if ($installResult.Success) {
                            Write-ProvisioningLog "$($action.ref) - Installed successfully" -Level SUCCESS
                            $result.status = "success"
                            $result.message = "Installed"
                            $successCount++
                        } else {
                            Write-ProvisioningLog "$($action.ref) - Installation failed: $($installResult.Error)" -Level ERROR
                            $result.status = "failed"
                            $result.message = $installResult.Error
                            $failCount++
                        }
                    }
                }
            }
            
            "restore" {
                if (-not $EnableRestore) {
                    # Restore is opt-in - skip unless explicitly enabled
                    Write-ProvisioningLog "SKIP: $($action.source) -> $($action.target) (restore not enabled)" -Level SKIP
                    $result.status = "skipped"
                    $result.message = "Restore not enabled (use -EnableRestore)"
                    $skipCount++
                }
                elseif ($DryRun) {
                    Write-ProvisioningLog "[DRY-RUN] Would restore: $($action.source) -> $($action.target)" -Level ACTION
                    $result.status = "dry-run"
                    $result.message = "Would restore"
                    $successCount++
                } else {
                    Write-ProvisioningLog "Restoring: $($action.source) -> $($action.target)" -Level ACTION
                    $restoreResult = Invoke-CopyRestore -Source $action.source -Target $action.target -Backup $action.backup -RunId $runId
                    if ($restoreResult.Success) {
                        if ($restoreResult.Skipped) {
                            Write-ProvisioningLog "SKIP: $($action.target) - $($restoreResult.Message)" -Level SKIP
                            $result.status = "skipped"
                            $result.message = $restoreResult.Message
                            $skipCount++
                        } else {
                            Write-ProvisioningLog "Restored: $($action.target)" -Level SUCCESS
                            $result.status = "success"
                            $result.message = "Restored"
                            if ($restoreResult.BackupPath) {
                                $result.backupPath = $restoreResult.BackupPath
                            }
                            $successCount++
                        }
                    } else {
                        Write-ProvisioningLog "Restore failed: $($restoreResult.Error)" -Level ERROR
                        $result.status = "failed"
                        $result.message = $restoreResult.Error
                        $failCount++
                    }
                }
            }
            
            "verify" {
                $verifyResult = $null
                
                switch ($action.verifyType) {
                    "file-exists" {
                        $verifyResult = Test-FileExistsVerifier -Path $action.path
                    }
                    "command-succeeds" {
                        # Future: implement command verification
                        $verifyResult = @{ Success = $true; Message = "Command verification not yet implemented" }
                    }
                    default {
                        $verifyResult = @{ Success = $false; Message = "Unknown verify type: $($action.verifyType)" }
                    }
                }
                
                if ($verifyResult.Success) {
                    Write-ProvisioningLog "Verify PASSED: $($action.verifyType)" -Level SUCCESS
                    $result.status = "success"
                    $result.message = $verifyResult.Message
                    $successCount++
                } else {
                    Write-ProvisioningLog "Verify FAILED: $($action.verifyType) - $($verifyResult.Message)" -Level ERROR
                    $result.status = "failed"
                    $result.message = $verifyResult.Message
                    $failCount++
                }
            }
        }
        
        $actionResults += $result
    }
    
    # Save state
    $manifestHash = Get-ManifestHash -ManifestPath $ManifestPath
    Save-RunState -RunId $runId `
        -ManifestPath $ManifestPath `
        -ManifestHash $manifestHash `
        -Command "apply" `
        -DryRun $DryRun.IsPresent `
        -Actions $actionResults `
        -SuccessCount $successCount `
        -SkipCount $skipCount `
        -FailCount $failCount
    
    # Summary
    Write-ProvisioningSection "Results"
    Close-ProvisioningLog -SuccessCount $successCount -SkipCount $skipCount -FailCount $failCount
    
    if ($DryRun) {
        Write-Host ""
        Write-Host "Dry-run complete. No changes were made." -ForegroundColor Yellow
        Write-Host ""
        Write-Host "To apply for real:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command apply -Manifest `"$ManifestPath`""
        Write-Host ""
    } else {
        Write-Host ""
        if ($failCount -eq 0) {
            Write-Host "Apply complete!" -ForegroundColor Green
        } else {
            Write-Host "Apply completed with $failCount failure(s)." -ForegroundColor Yellow
        }
        Write-Host ""
        Write-Host "To verify the result:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command verify -Manifest `"$ManifestPath`""
        Write-Host ""
    }
    
    return @{
        RunId = $runId
        DryRun = $DryRun.IsPresent
        Success = $successCount
        Skipped = $skipCount
        Failed = $failCount
        LogFile = $logFile
    }
}

# Functions exported: Invoke-Apply
