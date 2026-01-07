# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning planner - generates deterministic execution plans.

.DESCRIPTION
    Reads a manifest and resolves it into executable actions,
    detecting already-installed apps and computing minimal diff.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\..\drivers\driver.ps1"

function Invoke-Plan {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath
    )
    
    $runId = Get-RunId
    Initialize-ProvisioningLog -RunId "plan-$runId" | Out-Null
    
    Write-ProvisioningSection "Provisioning Plan"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    # Read manifest
    if (-not (Test-Path $ManifestPath)) {
        Write-ProvisioningLog "Manifest not found: $ManifestPath" -Level ERROR
        return $null
    }
    
    $manifest = Read-Manifest -Path $ManifestPath
    Write-ProvisioningLog "Manifest loaded: $($manifest.name)" -Level SUCCESS
    
    # Get current state
    Write-ProvisioningSection "Analyzing Current State"
    $installedApps = Invoke-DriverGetInstalledPackages
    Write-ProvisioningLog "Found $($installedApps.Count) installed packages" -Level INFO
    
    # Build plan
    Write-ProvisioningSection "Computing Actions"
    $plan = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
            hash = (Get-ManifestHash -ManifestPath $ManifestPath)
        }
        actions = @()
        summary = @{
            install = 0
            skip = 0
            restore = 0
            verify = 0
        }
    }
    
    # Process apps
    $driverName = Get-ActiveDriverName
    foreach ($app in $manifest.apps) {
        $windowsRef = $app.refs.windows
        if (-not $windowsRef) {
            Write-ProvisioningLog "App '$($app.id)' has no Windows ref, skipping" -Level WARN
            continue
        }
        
        $isInstalled = $installedApps -contains $windowsRef
        
        $action = @{
            type = "app"
            id = $app.id
            ref = $windowsRef
            driver = $driverName
        }
        
        if ($isInstalled) {
            $action.status = "skip"
            $action.reason = "already installed"
            $plan.summary.skip++
            Write-ProvisioningLog "$windowsRef - SKIP (already installed)" -Level SKIP
        } else {
            $action.status = "install"
            $plan.summary.install++
            Write-ProvisioningLog "$windowsRef - INSTALL" -Level ACTION
        }
        
        $plan.actions += $action
    }
    
    # Process restore items
    if ($manifest.restore -and $manifest.restore.Count -gt 0) {
        foreach ($item in $manifest.restore) {
            $action = @{
                type = "restore"
                restoreType = $item.type
                source = $item.source
                target = $item.target
                backup = if ($item.backup) { $true } else { $false }
                status = "restore"
            }
            $plan.actions += $action
            $plan.summary.restore++
            Write-ProvisioningLog "RESTORE: $($item.source) -> $($item.target)" -Level ACTION
        }
    }
    
    # Process verify items
    if ($manifest.verify -and $manifest.verify.Count -gt 0) {
        foreach ($item in $manifest.verify) {
            $action = @{
                type = "verify"
                verifyType = $item.type
                status = "verify"
            }
            if ($item.path) { $action.path = $item.path }
            if ($item.command) { $action.command = $item.command }
            
            $plan.actions += $action
            $plan.summary.verify++
            Write-ProvisioningLog "VERIFY: $($item.type)" -Level ACTION
        }
    }
    
    # Save plan
    $plansDir = Join-Path $PSScriptRoot "..\plans"
    if (-not (Test-Path $plansDir)) {
        New-Item -ItemType Directory -Path $plansDir -Force | Out-Null
    }
    
    $planFile = Join-Path $plansDir "$runId.json"
    $plan | ConvertTo-Json -Depth 10 | Out-File -FilePath $planFile -Encoding UTF8
    Write-ProvisioningLog "Plan saved: $planFile" -Level INFO
    
    # Print summary
    Write-ProvisioningSection "Plan Summary"
    Write-Host ""
    Write-Host "  Apps to install: " -NoNewline
    Write-Host "$($plan.summary.install)" -ForegroundColor $(if ($plan.summary.install -gt 0) { "Cyan" } else { "DarkGray" })
    Write-Host "  Apps to skip:    " -NoNewline
    Write-Host "$($plan.summary.skip)" -ForegroundColor DarkGray
    Write-Host "  Configs to restore: " -NoNewline
    Write-Host "$($plan.summary.restore)" -ForegroundColor $(if ($plan.summary.restore -gt 0) { "Cyan" } else { "DarkGray" })
    Write-Host "  Verifications:   " -NoNewline
    Write-Host "$($plan.summary.verify)" -ForegroundColor $(if ($plan.summary.verify -gt 0) { "Cyan" } else { "DarkGray" })
    Write-Host ""
    
    if ($plan.summary.install -eq 0 -and $plan.summary.restore -eq 0) {
        Write-Host "  Nothing to do - system matches manifest!" -ForegroundColor Green
    } else {
        Write-Host "  To apply this plan:" -ForegroundColor Yellow
        Write-Host "    .\cli.ps1 -Command apply -Manifest `"$ManifestPath`" -DryRun  # Preview"
        Write-Host "    .\cli.ps1 -Command apply -Manifest `"$ManifestPath`"          # Execute"
    }
    Write-Host ""
    
    Close-ProvisioningLog -SuccessCount 0 -SkipCount $plan.summary.skip -FailCount 0
    
    return $plan
}

function Get-InstalledAppsFromWinget {
    Write-ProvisioningLog "Querying winget for installed packages..." -Level INFO
    
    try {
        # Get list of installed packages
        $output = & winget list --accept-source-agreements 2>&1
        
        $installedIds = @()
        $inTable = $false
        
        foreach ($line in $output) {
            $lineStr = $line.ToString()
            
            # Skip header lines
            if ($lineStr -match '^-+$') {
                $inTable = $true
                continue
            }
            
            if (-not $inTable) { continue }
            if ([string]::IsNullOrWhiteSpace($lineStr)) { continue }
            
            # Parse the line - winget list output is space-separated columns
            # Format: Name  Id  Version  Available  Source
            # We need to extract the Id column
            $parts = $lineStr -split '\s{2,}'
            if ($parts.Count -ge 2) {
                $id = $parts[1].Trim()
                if ($id -and $id -ne "Id" -and $id -notmatch '^-+$') {
                    $installedIds += $id
                }
            }
        }
        
        return $installedIds
        
    } catch {
        Write-ProvisioningLog "Error querying winget: $_" -Level ERROR
        return @()
    }
}

function New-PlanFromManifest {
    <#
    .SYNOPSIS
        Pure function to generate a plan from a parsed manifest.
    .DESCRIPTION
        Creates a deterministic plan given a manifest and list of installed apps.
        Does not perform I/O or call external processes - suitable for unit testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest,
        
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $true)]
        [string]$ManifestHash,
        
        [Parameter(Mandatory = $true)]
        [string]$RunId,
        
        [Parameter(Mandatory = $true)]
        [string]$Timestamp,
        
        [Parameter(Mandatory = $false)]
        [array]$InstalledApps = @()
    )
    
    $plan = @{
        runId = $RunId
        timestamp = $Timestamp
        manifest = @{
            path = $ManifestPath
            name = $Manifest.name
            hash = $ManifestHash
        }
        actions = @()
        summary = @{
            install = 0
            skip = 0
            restore = 0
            verify = 0
        }
    }
    
    # Process apps
    $driverName = Get-ActiveDriverName
    foreach ($app in $Manifest.apps) {
        $windowsRef = $app.refs.windows
        if (-not $windowsRef) {
            continue
        }
        
        $isInstalled = $InstalledApps -contains $windowsRef
        
        $action = @{
            type = "app"
            id = $app.id
            ref = $windowsRef
            driver = $driverName
        }
        
        if ($isInstalled) {
            $action.status = "skip"
            $action.reason = "already installed"
            $plan.summary.skip++
        } else {
            $action.status = "install"
            $plan.summary.install++
        }
        
        $plan.actions += $action
    }
    
    # Process restore items
    if ($Manifest.restore -and $Manifest.restore.Count -gt 0) {
        foreach ($item in $Manifest.restore) {
            $action = @{
                type = "restore"
                restoreType = $item.type
                source = $item.source
                target = $item.target
                backup = if ($item.backup) { $true } else { $false }
                status = "restore"
            }
            $plan.actions += $action
            $plan.summary.restore++
        }
    }
    
    # Process verify items
    if ($Manifest.verify -and $Manifest.verify.Count -gt 0) {
        foreach ($item in $Manifest.verify) {
            $action = @{
                type = "verify"
                verifyType = $item.type
                status = "verify"
            }
            if ($item.path) { $action.path = $item.path }
            if ($item.command) { $action.command = $item.command }
            
            $plan.actions += $action
            $plan.summary.verify++
        }
    }
    
    return $plan
}

function ConvertTo-ReportJson {
    <#
    .SYNOPSIS
        Convert a plan to a stable JSON report format.
    .DESCRIPTION
        Serializes plan to JSON with deterministic key ordering for stable output.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Plan
    )
    
    # Create ordered structure for deterministic output
    $ordered = [ordered]@{
        runId = $Plan.runId
        timestamp = $Plan.timestamp
        manifest = [ordered]@{
            path = $Plan.manifest.path
            name = $Plan.manifest.name
            hash = $Plan.manifest.hash
        }
        summary = [ordered]@{
            install = $Plan.summary.install
            skip = $Plan.summary.skip
            restore = $Plan.summary.restore
            verify = $Plan.summary.verify
        }
        actions = @()
    }
    
    foreach ($action in $Plan.actions) {
        $orderedAction = [ordered]@{
            type = $action.type
            driver = $action.driver
            id = $action.id
            ref = $action.ref
            status = $action.status
        }
        
        # Add optional fields in consistent order
        if ($action.reason) { $orderedAction.reason = $action.reason }
        if ($action.restoreType) { $orderedAction.restoreType = $action.restoreType }
        if ($action.source) { $orderedAction.source = $action.source }
        if ($action.target) { $orderedAction.target = $action.target }
        if ($null -ne $action.backup) { $orderedAction.backup = $action.backup }
        if ($action.verifyType) { $orderedAction.verifyType = $action.verifyType }
        if ($action.path) { $orderedAction.path = $action.path }
        if ($action.command) { $orderedAction.command = $action.command }
        
        $ordered.actions += $orderedAction
    }
    
    return $ordered | ConvertTo-Json -Depth 10
}

# Functions exported: Invoke-Plan, Get-InstalledAppsFromWinget, New-PlanFromManifest, ConvertTo-ReportJson
