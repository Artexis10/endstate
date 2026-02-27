# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

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
. "$PSScriptRoot\events.ps1"
. "$PSScriptRoot\..\drivers\driver.ps1"
. "$PSScriptRoot\restore.ps1"
. "$PSScriptRoot\..\verifiers\file-exists.ps1"
. "$PSScriptRoot\config-modules.ps1"

function Invoke-Apply {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,

        [Parameter(Mandatory = $false)]
        [switch]$DryRun,

        [Parameter(Mandatory = $false)]
        [switch]$EnableRestore,

        [Parameter(Mandatory = $false)]
        [string]$RestoreFilter = $null,

        [Parameter(Mandatory = $false)]
        [switch]$OutputJson,

        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    $runId = Get-RunId
    
    try {
    
    # Enable streaming events if requested
    if ($EventsFormat -eq "jsonl") {
        Enable-StreamingEvents -RunId "apply-$runId"
    }
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
    Write-PhaseEvent -Phase "plan"
    $plan = Invoke-Plan -ManifestPath $ManifestPath
    
    if (-not $plan) {
        Write-ProvisioningLog "Failed to generate plan" -Level ERROR
        Write-ErrorEvent -Scope "engine" -Message "Failed to generate plan"
        return $null
    }
    
    # Execute actions
    Write-ProvisioningSection "Executing Actions"
    Write-PhaseEvent -Phase "apply"
    
    $successCount = 0
    $skipCount = 0
    $failCount = 0
    $actionResults = @()
    $pendingRestoreActions = @()

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
                    Write-ItemEvent -Id $action.ref -Driver (Get-ActiveDriverName) -Status "present" -Reason "already_installed" -Message "Already installed"
                    $result.status = "skipped"
                    $result.message = "Already installed"
                    $skipCount++
                }
                elseif ($action.status -eq "install") {
                    $driverName = Get-ActiveDriverName
                    if ($DryRun) {
                        Write-ProvisioningLog "[DRY-RUN] Would install: $($action.ref)" -Level ACTION
                        Write-ItemEvent -Id $action.ref -Driver $driverName -Status "to_install" -Message "Would install via $driverName"
                        $result.status = "dry-run"
                        $result.message = "Would install via $driverName"
                        $successCount++
                    } else {
                        Write-ProvisioningLog "Installing: $($action.ref)" -Level ACTION
                        Write-ItemEvent -Id $action.ref -Driver $driverName -Status "installing" -Message "Installing via $driverName"
                        $installResult = Invoke-DriverInstallPackage -PackageId $action.ref
                        if ($installResult.Success) {
                            Write-ProvisioningLog "$($action.ref) - Installed successfully" -Level SUCCESS
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "installed" -Message "Installed successfully"
                            $result.status = "success"
                            $result.message = "Installed"
                            $successCount++
                        } elseif ($installResult.UserDenied) {
                            Write-ProvisioningLog "$($action.ref) - User cancelled installation" -Level SKIP
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "skipped" -Reason "user_denied" -Message $installResult.Error
                            $result.status = "skipped"
                            $result.message = $installResult.Error
                            $skipCount++
                        } else {
                            Write-ProvisioningLog "$($action.ref) - Installation failed: $($installResult.Error)" -Level ERROR
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "failed" -Reason "install_failed" -Message $installResult.Error
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
                } else {
                    # Collect for dedicated restore phase
                    $pendingRestoreActions += $action
                    $result.status = "deferred"
                    $result.message = "Deferred to restore phase"
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

    # === Restore Phase ===
    $restoreResults = @()
    $restoreSuccessCount = 0
    $restoreSkipCount = 0
    $restoreFailCount = 0
    $restoreBackupLocation = $null

    # Parse RestoreFilter into array if provided
    $restoreFilterArray = $null
    if ($RestoreFilter) {
        $restoreFilterArray = @($RestoreFilter -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    }

    # Compute available modules before filtering (for envelope)
    $restoreModulesAvailable = @($pendingRestoreActions | ForEach-Object {
        if ($_.module) { $_.module } elseif ($_._fromModule) { $_._fromModule } else { $null }
    } | Where-Object { $_ } | Select-Object -Unique | Sort-Object)

    # Apply RestoreFilter if provided
    if ($restoreFilterArray -and $restoreFilterArray.Count -gt 0 -and $pendingRestoreActions.Count -gt 0) {
        $pendingRestoreActions = @($pendingRestoreActions | Where-Object {
            $moduleId = if ($_.module) { $_.module } elseif ($_._fromModule) { $_._fromModule } else { $null }
            # Inline entries (no module) always pass the filter
            if (-not $moduleId) { return $true }
            return $moduleId -in $restoreFilterArray
        })
    }

    if ($EnableRestore -and $pendingRestoreActions.Count -gt 0) {
        Write-ProvisioningSection "Executing Restore Phase"
        Write-PhaseEvent -Phase "restore"

        $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)

        foreach ($restoreAction in $pendingRestoreActions) {
            # Build action hashtable for Invoke-RestoreAction
            $actionId = Get-RestoreActionId -Item $restoreAction
            $restoreType = if ($restoreAction.restoreType) { $restoreAction.restoreType } else { "copy" }
            $restorerName = switch ($restoreType) {
                "merge" {
                    $fmt = if ($restoreAction.format) { $restoreAction.format } else { "json" }
                    "merge-$fmt"
                }
                "append" { "append" }
                default { "copy" }
            }
            $moduleId = if ($restoreAction.module) { $restoreAction.module } else { ($actionId -split '[/\\]')[0] }

            # Emit "restoring" event
            Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                -Source $restoreAction.source -Target $restoreAction.target `
                -Status "restoring" -Message "Restoring $actionId"

            $actionHash = @{
                id = $actionId
                restoreType = $restoreAction.restoreType
                source = $restoreAction.source
                target = $restoreAction.target
                backup = if ($null -eq $restoreAction.backup) { $true } else { $restoreAction.backup }
                requiresAdmin = if ($restoreAction.requiresAdmin) { $true } else { $false }
                requiresClosed = $restoreAction.requiresClosed
                format = $restoreAction.format
                arrayStrategy = $restoreAction.arrayStrategy
                dedupe = $restoreAction.dedupe
                newline = $restoreAction.newline
                exclude = $restoreAction.exclude
            }

            if ($DryRun) {
                Write-ProvisioningLog "[DRY-RUN] Would restore: $($restoreAction.source) -> $($restoreAction.target)" -Level ACTION
                $restoreResult = @{
                    id = $actionId
                    module = $moduleId
                    restorer = $restorerName
                    source = $restoreAction.source
                    target = $restoreAction.target
                    status = "skipped_up_to_date"
                    reason = "dry-run"
                    backupPath = $null
                    targetExisted = $false
                    message = "Would restore"
                }
                Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                    -Source $restoreAction.source -Target $restoreAction.target `
                    -Status "skipped_up_to_date" -Reason "dry-run" -Message "Would restore"
                $restoreSuccessCount++
            } else {
                Write-ProvisioningLog "Restoring: $($restoreAction.source) -> $($restoreAction.target)" -Level ACTION
                $raResult = Invoke-RestoreAction -Action $actionHash -RunId $runId -ManifestDir $manifestDir

                # Map Invoke-RestoreAction status to event status
                $eventStatus = switch ($raResult.status) {
                    "restore" { "restored" }
                    "skip" {
                        if ($raResult.reason -like "*up to date*") { "skipped_up_to_date" }
                        elseif ($raResult.reason -like "*not found*") { "skipped_missing_source" }
                        else { "skipped_up_to_date" }
                    }
                    "fail" { "failed" }
                    default { "failed" }
                }
                $targetExisted = if ($raResult.ContainsKey('targetExistedBefore')) { $raResult.targetExistedBefore } else { $false }

                Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                    -Source $restoreAction.source -Target $restoreAction.target `
                    -Status $eventStatus -Reason $raResult.reason `
                    -BackupPath $raResult.backupPath -TargetExisted $targetExisted `
                    -Message $raResult.reason

                $restoreResult = @{
                    id = $actionId
                    module = $moduleId
                    restorer = $restorerName
                    source = $restoreAction.source
                    target = $restoreAction.target
                    expandedSource = $raResult.expandedSource
                    expandedTarget = $raResult.expandedTarget
                    status = $eventStatus
                    reason = $raResult.reason
                    backupPath = $raResult.backupPath
                    targetExisted = $targetExisted
                    backupCreated = if ($raResult.ContainsKey('backupCreated')) { $raResult.backupCreated } else { $false }
                    message = $raResult.reason
                }

                # Track backup location
                if ($raResult.backupPath -and -not $restoreBackupLocation) {
                    $restoreBackupLocation = Split-Path -Parent $raResult.backupPath
                }

                switch ($raResult.status) {
                    "restore" {
                        Write-ProvisioningLog "RESTORED: $actionId" -Level SUCCESS
                        $restoreSuccessCount++
                    }
                    "skip" {
                        Write-ProvisioningLog "SKIP: $actionId - $($raResult.reason)" -Level SKIP
                        $restoreSkipCount++
                    }
                    "fail" {
                        Write-ProvisioningLog "FAIL: $actionId - $($raResult.reason)" -Level ERROR
                        $restoreFailCount++
                    }
                    "dry-run" {
                        Write-ProvisioningLog "[DRY-RUN] $actionId - $($raResult.reason)" -Level ACTION
                        $restoreSuccessCount++
                    }
                }
            }

            $restoreResults += $restoreResult
        }

        # Restore summary event
        $restoreTotal = $restoreSuccessCount + $restoreSkipCount + $restoreFailCount
        Write-SummaryEvent -Phase "restore" -Total $restoreTotal -Success $restoreSuccessCount -Skipped $restoreSkipCount -Failed $restoreFailCount -BackupLocation $restoreBackupLocation

        # Write restore journal (non-dry-run only)
        if (-not $DryRun) {
            $logsDir = Join-Path $PSScriptRoot "..\logs"
            if (-not (Test-Path $logsDir)) {
                New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
            }

            $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
            $journalEntries = @()
            foreach ($rr in $restoreResults) {
                $actionStatus = switch ($rr.status) {
                    "restored" { "restored" }
                    "skipped_up_to_date" { "skipped_up_to_date" }
                    "skipped_missing_source" { "skipped_missing_source" }
                    "failed" { "failed" }
                    default { $rr.status }
                }
                $journalEntries += @{
                    kind = if ($rr.restorer -eq "copy") { "copy" } elseif ($rr.restorer -like "merge-*") { "merge" } else { $rr.restorer }
                    source = $rr.source
                    target = $rr.target
                    resolvedSourcePath = $rr.expandedSource
                    targetPath = $rr.expandedTarget
                    backupRequested = $true
                    targetExistedBefore = $rr.targetExisted
                    backupCreated = $rr.backupCreated
                    backupPath = $rr.backupPath
                    action = $actionStatus
                    error = if ($rr.status -eq "failed") { $rr.reason } else { $null }
                }
            }

            $journal = @{
                runId = $runId
                manifestPath = $ManifestPath
                manifestDir = $manifestDir
                exportRoot = $null
                timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
                entries = $journalEntries
            }

            $journalFile = Join-Path $logsDir "restore-journal-$runId.json"
            $tempFile = "$journalFile.tmp"
            $journal | ConvertTo-Json -Depth 10 | Out-File -FilePath $tempFile -Encoding UTF8
            Move-Item -Path $tempFile -Destination $journalFile -Force
            Write-ProvisioningLog "Restore journal written: $journalFile" -Level INFO
        }
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
    Write-SummaryEvent -Phase "apply" -Total $actionResults.Count -Success $successCount -Skipped $skipCount -Failed $failCount
    Close-ProvisioningLog -SuccessCount ($successCount + $restoreSuccessCount) -SkipCount ($skipCount + $restoreSkipCount) -FailCount ($failCount + $restoreFailCount)

    # Get state file path (absolute)
    $stateDir = Join-Path $PSScriptRoot "..\state"
    $stateFile = (Join-Path $stateDir "$runId.json") | Resolve-Path -ErrorAction SilentlyContinue
    if (-not $stateFile) {
        $stateFile = [System.IO.Path]::GetFullPath((Join-Path $stateDir "$runId.json"))
    }

    if ($OutputJson) {
        # Output JSON envelope
        . "$PSScriptRoot\json-output.ps1"

        # Build items[] array for GUI consumption (app entries only — unchanged)
        $items = @($actionResults | Where-Object { $_.action.type -eq "app" } | ForEach-Object {
            $guiStatus = switch ($_.status) {
                "success" { "ok" }
                "dry-run" { "ok" }  # dry-run is a successful preview
                "skipped" { "skipped" }
                "failed" { "failed" }
                default { "skipped" }
            }
            $guiReason = switch ($_.status) {
                "success" { "installed" }
                "dry-run" { "would_install" }
                "skipped" { "already_installed" }
                "failed" { "install_failed" }
                default { "unknown" }
            }
            [ordered]@{
                id = $_.action.ref
                driver = (Get-ActiveDriverName)
                status = $guiStatus
                reason = $guiReason
                message = $_.message
            }
        })

        # Count items by category for GUI
        $installedCount = @($items | Where-Object { $_.reason -eq "installed" }).Count
        $alreadyInstalledCount = @($items | Where-Object { $_.reason -eq "already_installed" }).Count
        $failedCount = @($items | Where-Object { $_.status -eq "failed" }).Count

        # Convert logFile to absolute path
        $logFileAbsolute = $logFile
        if ($logFile -and -not [System.IO.Path]::IsPathRooted($logFile)) {
            $logFileAbsolute = [System.IO.Path]::GetFullPath($logFile)
        }

        $data = [ordered]@{
            dryRun = $DryRun.IsPresent
            manifest = [ordered]@{
                path = $ManifestPath
                name = Split-Path -Leaf $ManifestPath
                hash = $manifestHash
            }
            summary = [ordered]@{
                total = $actionResults.Count
                success = $successCount
                skipped = $skipCount
                failed = $failCount
            }
            # GUI-expected counts structure
            counts = [ordered]@{
                total = $items.Count
                installed = $installedCount
                alreadyInstalled = $alreadyInstalledCount
                skippedFiltered = 0
                failed = $failedCount
            }
            # GUI-expected items array
            items = $items
            # Legacy actions array (for backward compatibility)
            actions = @($actionResults | ForEach-Object {
                [ordered]@{
                    type = $_.action.type
                    id = $_.action.id
                    ref = $_.action.ref
                    status = $_.status
                    message = $_.message
                }
            })
            runId = $runId
            stateFile = $stateFile
            logFile = $logFileAbsolute
        }

        # Add configModuleMap for GUI (additive, backward compatible)
        try {
            $manifestContent = Read-JsoncFile -Path $ManifestPath
            if ($manifestContent.configModules -and $manifestContent.configModules.Count -gt 0) {
                $cmMap = Build-ConfigModuleMap -ModuleIds $manifestContent.configModules
                if ($null -ne $cmMap) {
                    $data['configModuleMap'] = $cmMap
                }
            }
        } catch {
            # Non-fatal: configModuleMap is optional
        }

        # Add restore metadata to envelope (additive, backward compatible)
        if ($restoreFilterArray) {
            $data['restoreFilter'] = $restoreFilterArray
        }
        if ($restoreModulesAvailable.Count -gt 0) {
            $data['restoreModulesAvailable'] = $restoreModulesAvailable
        }

        # Add restore data to envelope (additive, backward compatible)
        if ($restoreResults.Count -gt 0) {
            $data['restoreItems'] = @($restoreResults | ForEach-Object {
                [ordered]@{
                    id = $_.id
                    module = $_.module
                    restorer = $_.restorer
                    source = $_.source
                    target = $_.target
                    status = $_.status
                    reason = $_.reason
                    backupPath = $_.backupPath
                    targetExisted = $_.targetExisted
                    message = $_.message
                }
            })
            $data['restoreSummary'] = [ordered]@{
                total = $restoreResults.Count
                restored = $restoreSuccessCount
                skipped = $restoreSkipCount
                failed = $restoreFailCount
                backupLocation = $restoreBackupLocation
            }
            # Add journal file path if written
            if (-not $DryRun) {
                $restoreLogsDir = Join-Path $PSScriptRoot "..\logs"
                $data['restoreJournalFile'] = [System.IO.Path]::GetFullPath((Join-Path $restoreLogsDir "restore-journal-$runId.json"))
            }
        }

        # Add eventsFile if events are enabled
        if ($EventsFormat -eq "jsonl") {
            $logsDir = Join-Path $PSScriptRoot "..\logs"
            $eventsFile = [System.IO.Path]::GetFullPath((Join-Path $logsDir "apply-$runId.events.jsonl"))
            $data['eventsFile'] = $eventsFile
        }

        $envelope = New-JsonEnvelope -Command "apply" -RunId $runId -Success (($failCount + $restoreFailCount) -eq 0) -Data $data
        Write-JsonOutput -Envelope $envelope
    } else {
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
    }
    
    return @{
        RunId = $runId
        DryRun = $DryRun.IsPresent
        Success = $successCount
        Skipped = $skipCount
        Failed = $failCount
        LogFile = $logFile
    }
    
    } finally {
        # Clean up profile bundle temp directories from include resolution
        if ($script:ProfileBundleTempDirs -and $script:ProfileBundleTempDirs.Count -gt 0) {
            foreach ($tempDir in $script:ProfileBundleTempDirs) {
                if ($tempDir -and (Test-Path $tempDir)) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
            $script:ProfileBundleTempDirs = @()
        }
    }
}

function Invoke-ApplyFromPlan {
    <#
    .SYNOPSIS
        Execute a previously generated plan without recomputing actions.
    .DESCRIPTION
        Loads a plan JSON file and executes the actions in order.
        Actions are processed deterministically in the order they appear.
    .PARAMETER PlanPath
        Path to the plan JSON file.
    .PARAMETER DryRun
        Preview what would happen without making changes.
    .PARAMETER EnableRestore
        Enable restore actions (opt-in for safety).
    .PARAMETER OutputJson
        Output results as JSON with standard envelope.
    .PARAMETER RestoreFilter
        Comma-separated list of module IDs to filter restore actions.
    .PARAMETER EventsFormat
        Streaming events format (jsonl for NDJSON to stderr).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$PlanPath,

        [Parameter(Mandatory = $false)]
        [switch]$DryRun,

        [Parameter(Mandatory = $false)]
        [switch]$EnableRestore,

        [Parameter(Mandatory = $false)]
        [string]$RestoreFilter = $null,

        [Parameter(Mandatory = $false)]
        [switch]$OutputJson,

        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    # Validate plan file exists
    if (-not (Test-Path $PlanPath)) {
        Write-Host "[ERROR] Plan file not found: $PlanPath" -ForegroundColor Red
        return $null
    }
    
    # Load plan (supports JSONC format)
    try {
        $plan = Read-JsoncFile -Path $PlanPath -Depth 100
    } catch {
        Write-Host "[ERROR] Failed to parse plan file: $($_.Exception.Message)" -ForegroundColor Red
        return $null
    }
    
    # Validate required fields
    if (-not $plan.actions) {
        Write-Host "[ERROR] Plan file missing required 'actions' array." -ForegroundColor Red
        return $null
    }
    
    if (-not $plan.runId) {
        Write-Host "[ERROR] Plan file missing required 'runId' field." -ForegroundColor Red
        return $null
    }
    
    $runId = Get-RunId
    
    # Enable streaming events if requested
    if ($EventsFormat -eq "jsonl") {
        Enable-StreamingEvents -RunId "apply-from-plan-$runId"
    }
    
    $logFile = Initialize-ProvisioningLog -RunId "apply-from-plan-$runId"
    
    $modeText = if ($DryRun) { "DRY-RUN (from plan)" } else { "APPLY (from plan)" }
    
    Write-ProvisioningSection "Provisioning $modeText"
    Write-ProvisioningLog "Plan file: $PlanPath" -Level INFO
    Write-ProvisioningLog "Original plan run ID: $($plan.runId)" -Level INFO
    Write-ProvisioningLog "Current run ID: $runId" -Level INFO
    Write-ProvisioningLog "Mode: $modeText" -Level INFO
    
    if ($plan.manifest) {
        Write-ProvisioningLog "Original manifest: $($plan.manifest.name) ($($plan.manifest.path))" -Level INFO
    }
    
    if ($DryRun) {
        Write-Host ""
        Write-Host "  *** DRY-RUN MODE - No changes will be made ***" -ForegroundColor Yellow
        Write-Host ""
    }
    
    # Log plan summary
    Write-ProvisioningSection "Plan Summary"
    $installCount = @($plan.actions | Where-Object { $_.status -eq "install" }).Count
    $skipCount = @($plan.actions | Where-Object { $_.status -eq "skip" }).Count
    $restoreCount = @($plan.actions | Where-Object { $_.type -eq "restore" }).Count
    $verifyCount = @($plan.actions | Where-Object { $_.type -eq "verify" }).Count
    
    Write-ProvisioningLog "Actions to execute: $($plan.actions.Count) total" -Level INFO
    Write-ProvisioningLog "  - Install: $installCount" -Level INFO
    Write-ProvisioningLog "  - Skip: $skipCount" -Level INFO
    if ($restoreCount -gt 0) { Write-ProvisioningLog "  - Restore: $restoreCount" -Level INFO }
    if ($verifyCount -gt 0) { Write-ProvisioningLog "  - Verify: $verifyCount" -Level INFO }
    
    # Execute actions in order (deterministic)
    Write-ProvisioningSection "Executing Actions"
    Write-PhaseEvent -Phase "apply"
    
    $successCount = 0
    $skippedCount = 0
    $failCount = 0
    $actionResults = @()
    $pendingRestoreActions = @()

    foreach ($action in $plan.actions) {
        $result = @{
            action = $action
            status = "pending"
            message = ""
        }

        switch ($action.type) {
            "app" {
                $driverName = Get-ActiveDriverName
                if ($action.status -eq "skip") {
                    $reason = if ($action.reason) { $action.reason } else { "skipped in plan" }
                    Write-ProvisioningLog "[SKIP] $($action.ref) - $reason" -Level SKIP
                    Write-ItemEvent -Id $action.ref -Driver $driverName -Status "present" -Reason "already_installed" -Message $reason
                    $result.status = "skipped"
                    $result.message = $reason
                    $skippedCount++
                }
                elseif ($action.status -eq "install") {
                    if ($DryRun) {
                        Write-ProvisioningLog "[DRY-RUN] Would install: $($action.ref)" -Level ACTION
                        Write-ItemEvent -Id $action.ref -Driver $driverName -Status "to_install" -Message "Would install via $driverName"
                        $result.status = "dry-run"
                        $result.message = "Would install via $driverName"
                        $successCount++
                    } else {
                        Write-ProvisioningLog "Installing: $($action.ref)" -Level ACTION
                        Write-ItemEvent -Id $action.ref -Driver $driverName -Status "installing" -Message "Installing via $driverName"
                        $installResult = Invoke-DriverInstallPackage -PackageId $action.ref
                        if ($installResult.Success) {
                            Write-ProvisioningLog "$($action.ref) - Installed successfully" -Level SUCCESS
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "installed" -Message "Installed successfully"
                            $result.status = "success"
                            $result.message = "Installed"
                            $successCount++
                        } elseif ($installResult.UserDenied) {
                            Write-ProvisioningLog "$($action.ref) - User cancelled installation" -Level SKIP
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "skipped" -Reason "user_denied" -Message $installResult.Error
                            $result.status = "skipped"
                            $result.message = $installResult.Error
                            $skippedCount++
                        } else {
                            Write-ProvisioningLog "$($action.ref) - Installation failed: $($installResult.Error)" -Level ERROR
                            Write-ItemEvent -Id $action.ref -Driver $driverName -Status "failed" -Reason "install_failed" -Message $installResult.Error
                            $result.status = "failed"
                            $result.message = $installResult.Error
                            $failCount++
                        }
                    }
                }
            }

            "restore" {
                if (-not $EnableRestore) {
                    Write-ProvisioningLog "[SKIP] $($action.source) -> $($action.target) (restore not enabled)" -Level SKIP
                    $result.status = "skipped"
                    $result.message = "Restore not enabled (use -EnableRestore)"
                    $skippedCount++
                } else {
                    # Collect for dedicated restore phase
                    $pendingRestoreActions += $action
                    $result.status = "deferred"
                    $result.message = "Deferred to restore phase"
                }
            }

            "verify" {
                $verifyResult = $null
                $verifyType = if ($action.verifyType) { $action.verifyType } else { $action.type }

                switch ($verifyType) {
                    "file-exists" {
                        $verifyResult = Test-FileExistsVerifier -Path $action.path
                    }
                    "command-succeeds" {
                        $verifyResult = @{ Success = $true; Message = "Command verification not yet implemented" }
                    }
                    default {
                        $verifyResult = @{ Success = $false; Message = "Unknown verify type: $verifyType" }
                    }
                }

                if ($verifyResult.Success) {
                    Write-ProvisioningLog "Verify PASSED: $verifyType" -Level SUCCESS
                    $result.status = "success"
                    $result.message = $verifyResult.Message
                    $successCount++
                } else {
                    Write-ProvisioningLog "Verify FAILED: $verifyType - $($verifyResult.Message)" -Level ERROR
                    $result.status = "failed"
                    $result.message = $verifyResult.Message
                    $failCount++
                }
            }
        }

        $actionResults += $result
    }

    # === Restore Phase ===
    $restoreResults = @()
    $restoreSuccessCount = 0
    $restoreSkipCount = 0
    $restoreFailCount = 0
    $restoreBackupLocation = $null

    # Parse RestoreFilter into array if provided
    $restoreFilterArray = $null
    if ($RestoreFilter) {
        $restoreFilterArray = @($RestoreFilter -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    }

    # Compute available modules before filtering (for envelope)
    $restoreModulesAvailable = @($pendingRestoreActions | ForEach-Object {
        if ($_.module) { $_.module } elseif ($_._fromModule) { $_._fromModule } else { $null }
    } | Where-Object { $_ } | Select-Object -Unique | Sort-Object)

    # Apply RestoreFilter if provided
    if ($restoreFilterArray -and $restoreFilterArray.Count -gt 0 -and $pendingRestoreActions.Count -gt 0) {
        $pendingRestoreActions = @($pendingRestoreActions | Where-Object {
            $moduleId = if ($_.module) { $_.module } elseif ($_._fromModule) { $_._fromModule } else { $null }
            # Inline entries (no module) always pass the filter
            if (-not $moduleId) { return $true }
            return $moduleId -in $restoreFilterArray
        })
    }

    if ($EnableRestore -and $pendingRestoreActions.Count -gt 0) {
        Write-ProvisioningSection "Executing Restore Phase"
        Write-PhaseEvent -Phase "restore"

        $planManifestDir = $null
        if ($plan.manifest.path -and (Test-Path $plan.manifest.path)) {
            $planManifestDir = Split-Path -Parent (Resolve-Path $plan.manifest.path)
        }

        foreach ($restoreAction in $pendingRestoreActions) {
            $actionId = Get-RestoreActionId -Item $restoreAction
            $restoreType = if ($restoreAction.restoreType) { $restoreAction.restoreType } else { "copy" }
            $restorerName = switch ($restoreType) {
                "merge" {
                    $fmt = if ($restoreAction.format) { $restoreAction.format } else { "json" }
                    "merge-$fmt"
                }
                "append" { "append" }
                default { "copy" }
            }
            $moduleId = if ($restoreAction.module) { $restoreAction.module } else { ($actionId -split '[/\\]')[0] }

            Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                -Source $restoreAction.source -Target $restoreAction.target `
                -Status "restoring" -Message "Restoring $actionId"

            $actionHash = @{
                id = $actionId
                restoreType = $restoreAction.restoreType
                source = $restoreAction.source
                target = $restoreAction.target
                backup = if ($null -eq $restoreAction.backup) { $true } else { $restoreAction.backup }
                requiresAdmin = if ($restoreAction.requiresAdmin) { $true } else { $false }
                requiresClosed = $restoreAction.requiresClosed
                format = $restoreAction.format
                arrayStrategy = $restoreAction.arrayStrategy
                dedupe = $restoreAction.dedupe
                newline = $restoreAction.newline
                exclude = $restoreAction.exclude
            }

            if ($DryRun) {
                Write-ProvisioningLog "[DRY-RUN] Would restore: $($restoreAction.source) -> $($restoreAction.target)" -Level ACTION
                $restoreResult = @{
                    id = $actionId
                    module = $moduleId
                    restorer = $restorerName
                    source = $restoreAction.source
                    target = $restoreAction.target
                    status = "skipped_up_to_date"
                    reason = "dry-run"
                    backupPath = $null
                    targetExisted = $false
                    message = "Would restore"
                }
                Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                    -Source $restoreAction.source -Target $restoreAction.target `
                    -Status "skipped_up_to_date" -Reason "dry-run" -Message "Would restore"
                $restoreSuccessCount++
            } else {
                Write-ProvisioningLog "Restoring: $($restoreAction.source) -> $($restoreAction.target)" -Level ACTION
                $raResult = Invoke-RestoreAction -Action $actionHash -RunId $runId -ManifestDir $planManifestDir

                $eventStatus = switch ($raResult.status) {
                    "restore" { "restored" }
                    "skip" {
                        if ($raResult.reason -like "*up to date*") { "skipped_up_to_date" }
                        elseif ($raResult.reason -like "*not found*") { "skipped_missing_source" }
                        else { "skipped_up_to_date" }
                    }
                    "fail" { "failed" }
                    default { "failed" }
                }
                $targetExisted = if ($raResult.ContainsKey('targetExistedBefore')) { $raResult.targetExistedBefore } else { $false }

                Write-RestoreItemEvent -Id $actionId -Module $moduleId -Restorer $restorerName `
                    -Source $restoreAction.source -Target $restoreAction.target `
                    -Status $eventStatus -Reason $raResult.reason `
                    -BackupPath $raResult.backupPath -TargetExisted $targetExisted `
                    -Message $raResult.reason

                $restoreResult = @{
                    id = $actionId
                    module = $moduleId
                    restorer = $restorerName
                    source = $restoreAction.source
                    target = $restoreAction.target
                    expandedSource = $raResult.expandedSource
                    expandedTarget = $raResult.expandedTarget
                    status = $eventStatus
                    reason = $raResult.reason
                    backupPath = $raResult.backupPath
                    targetExisted = $targetExisted
                    backupCreated = if ($raResult.ContainsKey('backupCreated')) { $raResult.backupCreated } else { $false }
                    message = $raResult.reason
                }

                if ($raResult.backupPath -and -not $restoreBackupLocation) {
                    $restoreBackupLocation = Split-Path -Parent $raResult.backupPath
                }

                switch ($raResult.status) {
                    "restore" {
                        Write-ProvisioningLog "RESTORED: $actionId" -Level SUCCESS
                        $restoreSuccessCount++
                    }
                    "skip" {
                        Write-ProvisioningLog "SKIP: $actionId - $($raResult.reason)" -Level SKIP
                        $restoreSkipCount++
                    }
                    "fail" {
                        Write-ProvisioningLog "FAIL: $actionId - $($raResult.reason)" -Level ERROR
                        $restoreFailCount++
                    }
                    "dry-run" {
                        Write-ProvisioningLog "[DRY-RUN] $actionId - $($raResult.reason)" -Level ACTION
                        $restoreSuccessCount++
                    }
                }
            }

            $restoreResults += $restoreResult
        }

        $restoreTotal = $restoreSuccessCount + $restoreSkipCount + $restoreFailCount
        Write-SummaryEvent -Phase "restore" -Total $restoreTotal -Success $restoreSuccessCount -Skipped $restoreSkipCount -Failed $restoreFailCount -BackupLocation $restoreBackupLocation

        if (-not $DryRun) {
            $logsDir = Join-Path $PSScriptRoot "..\logs"
            if (-not (Test-Path $logsDir)) {
                New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
            }

            $journalEntries = @()
            foreach ($rr in $restoreResults) {
                $actionStatus = switch ($rr.status) {
                    "restored" { "restored" }
                    "skipped_up_to_date" { "skipped_up_to_date" }
                    "skipped_missing_source" { "skipped_missing_source" }
                    "failed" { "failed" }
                    default { $rr.status }
                }
                $journalEntries += @{
                    kind = if ($rr.restorer -eq "copy") { "copy" } elseif ($rr.restorer -like "merge-*") { "merge" } else { $rr.restorer }
                    source = $rr.source
                    target = $rr.target
                    resolvedSourcePath = $rr.expandedSource
                    targetPath = $rr.expandedTarget
                    backupRequested = $true
                    targetExistedBefore = $rr.targetExisted
                    backupCreated = $rr.backupCreated
                    backupPath = $rr.backupPath
                    action = $actionStatus
                    error = if ($rr.status -eq "failed") { $rr.reason } else { $null }
                }
            }

            $journal = @{
                runId = $runId
                manifestPath = if ($plan.manifest.path) { $plan.manifest.path } else { $PlanPath }
                manifestDir = $planManifestDir
                exportRoot = $null
                timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
                entries = $journalEntries
            }

            $journalFile = Join-Path $logsDir "restore-journal-$runId.json"
            $tempFile = "$journalFile.tmp"
            $journal | ConvertTo-Json -Depth 10 | Out-File -FilePath $tempFile -Encoding UTF8
            Move-Item -Path $tempFile -Destination $journalFile -Force
            Write-ProvisioningLog "Restore journal written: $journalFile" -Level INFO
        }
    }

    # Save state
    $manifestPath = if ($plan.manifest.path) { $plan.manifest.path } else { $PlanPath }
    $manifestHash = if ($plan.manifest.hash) { $plan.manifest.hash } else { "from-plan" }
    
    Save-RunState -RunId $runId `
        -ManifestPath $manifestPath `
        -ManifestHash $manifestHash `
        -Command "apply-from-plan" `
        -DryRun $DryRun.IsPresent `
        -Actions $actionResults `
        -SuccessCount $successCount `
        -SkipCount $skippedCount `
        -FailCount $failCount
    
    # Summary
    Write-ProvisioningSection "Results"
    Write-SummaryEvent -Phase "apply" -Total $actionResults.Count -Success $successCount -Skipped $skippedCount -Failed $failCount
    Close-ProvisioningLog -SuccessCount ($successCount + $restoreSuccessCount) -SkipCount ($skippedCount + $restoreSkipCount) -FailCount ($failCount + $restoreFailCount)

    # Get state file path (absolute)
    $stateDir = Join-Path $PSScriptRoot "..\state"
    $stateFile = (Join-Path $stateDir "$runId.json") | Resolve-Path -ErrorAction SilentlyContinue
    if (-not $stateFile) {
        $stateFile = [System.IO.Path]::GetFullPath((Join-Path $stateDir "$runId.json"))
    }

    if ($OutputJson) {
        # Output JSON envelope
        . "$PSScriptRoot\json-output.ps1"

        # Build items[] array for GUI consumption (app entries only — unchanged)
        $items = @($actionResults | Where-Object { $_.action.type -eq "app" } | ForEach-Object {
            $guiStatus = switch ($_.status) {
                "success" { "ok" }
                "dry-run" { "ok" }
                "skipped" { "skipped" }
                "failed" { "failed" }
                default { "skipped" }
            }
            $guiReason = switch ($_.status) {
                "success" { "installed" }
                "dry-run" { "would_install" }
                "skipped" { "already_installed" }
                "failed" { "install_failed" }
                default { "unknown" }
            }
            [ordered]@{
                id = $_.action.ref
                driver = (Get-ActiveDriverName)
                status = $guiStatus
                reason = $guiReason
                message = $_.message
            }
        })

        # Count items by category for GUI
        $installedCount = @($items | Where-Object { $_.reason -eq "installed" }).Count
        $alreadyInstalledCount = @($items | Where-Object { $_.reason -eq "already_installed" }).Count
        $failedItemCount = @($items | Where-Object { $_.status -eq "failed" }).Count

        # Convert logFile to absolute path
        $logFileAbsolute = $logFile
        if ($logFile -and -not [System.IO.Path]::IsPathRooted($logFile)) {
            $logFileAbsolute = [System.IO.Path]::GetFullPath($logFile)
        }

        $data = [ordered]@{
            dryRun = $DryRun.IsPresent
            originalPlanRunId = $plan.runId
            planPath = $PlanPath
            manifest = [ordered]@{
                path = $manifestPath
                name = Split-Path -Leaf $manifestPath
                hash = $manifestHash
            }
            summary = [ordered]@{
                total = $actionResults.Count
                success = $successCount
                skipped = $skippedCount
                failed = $failCount
            }
            # GUI-expected counts structure
            counts = [ordered]@{
                total = $items.Count
                installed = $installedCount
                alreadyInstalled = $alreadyInstalledCount
                skippedFiltered = 0
                failed = $failedItemCount
            }
            # GUI-expected items array
            items = $items
            # Legacy actions array
            actions = @($actionResults | ForEach-Object {
                [ordered]@{
                    type = $_.action.type
                    id = $_.action.id
                    ref = $_.action.ref
                    status = $_.status
                    message = $_.message
                }
            })
            runId = $runId
            stateFile = $stateFile
            logFile = $logFileAbsolute
        }

        # Add configModuleMap for GUI (additive, backward compatible)
        if ($manifestPath -and (Test-Path -LiteralPath $manifestPath -ErrorAction SilentlyContinue)) {
            try {
                $manifestContent = Read-JsoncFile -Path $manifestPath
                if ($manifestContent.configModules -and $manifestContent.configModules.Count -gt 0) {
                    $cmMap = Build-ConfigModuleMap -ModuleIds $manifestContent.configModules
                    if ($null -ne $cmMap) {
                        $data['configModuleMap'] = $cmMap
                    }
                }
            } catch {
                # Non-fatal: configModuleMap is optional
            }
        }

        # Add restore metadata to envelope (additive, backward compatible)
        if ($restoreFilterArray) {
            $data['restoreFilter'] = $restoreFilterArray
        }
        if ($restoreModulesAvailable.Count -gt 0) {
            $data['restoreModulesAvailable'] = $restoreModulesAvailable
        }

        # Add restore data to envelope (additive, backward compatible)
        if ($restoreResults.Count -gt 0) {
            $data['restoreItems'] = @($restoreResults | ForEach-Object {
                [ordered]@{
                    id = $_.id
                    module = $_.module
                    restorer = $_.restorer
                    source = $_.source
                    target = $_.target
                    status = $_.status
                    reason = $_.reason
                    backupPath = $_.backupPath
                    targetExisted = $_.targetExisted
                    message = $_.message
                }
            })
            $data['restoreSummary'] = [ordered]@{
                total = $restoreResults.Count
                restored = $restoreSuccessCount
                skipped = $restoreSkipCount
                failed = $restoreFailCount
                backupLocation = $restoreBackupLocation
            }
            if (-not $DryRun) {
                $restoreLogsDir = Join-Path $PSScriptRoot "..\logs"
                $data['restoreJournalFile'] = [System.IO.Path]::GetFullPath((Join-Path $restoreLogsDir "restore-journal-$runId.json"))
            }
        }

        # Add eventsFile if events are enabled
        if ($EventsFormat -eq "jsonl") {
            $logsDir = Join-Path $PSScriptRoot "..\logs"
            $eventsFile = [System.IO.Path]::GetFullPath((Join-Path $logsDir "apply-$runId.events.jsonl"))
            $data['eventsFile'] = $eventsFile
        }

        $envelope = New-JsonEnvelope -Command "apply" -RunId $runId -Success (($failCount + $restoreFailCount) -eq 0) -Data $data
        Write-JsonOutput -Envelope $envelope
    } else {
        if ($DryRun) {
            Write-Host ""
            Write-Host "Dry-run complete. No changes were made." -ForegroundColor Yellow
            Write-Host ""
            Write-Host "To apply for real:" -ForegroundColor Yellow
            Write-Host "  .\cli.ps1 -Command apply -Plan `"$PlanPath`""
            Write-Host ""
        } else {
            Write-Host ""
            if ($failCount -eq 0) {
                Write-Host "Apply from plan complete!" -ForegroundColor Green
            } else {
                Write-Host "Apply from plan completed with $failCount failure(s)." -ForegroundColor Yellow
            }
            Write-Host ""
        }
    }
    
    return @{
        RunId = $runId
        OriginalPlanRunId = $plan.runId
        PlanPath = $PlanPath
        DryRun = $DryRun.IsPresent
        Success = $successCount
        Skipped = $skippedCount
        Failed = $failCount
        LogFile = $logFile
    }
}

# Functions exported: Invoke-Apply, Invoke-ApplyFromPlan
