# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning verify - runs verifiers only without modifying state.

.DESCRIPTION
    Reads a manifest and runs all verification steps to check
    if the current machine state matches the desired state.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\external.ps1"
. "$PSScriptRoot\..\drivers\driver.ps1"
. "$PSScriptRoot\events.ps1"
. "$PSScriptRoot\..\verifiers\file-exists.ps1"
. "$PSScriptRoot\..\verifiers\command-exists.ps1"
. "$PSScriptRoot\..\verifiers\registry-key-exists.ps1"

function Invoke-Verify {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [switch]$OutputJson,
        
        [Parameter(Mandatory = $false)]
        [string]$EventsFormat = ""
    )
    
    $runId = Get-RunId
    
    # Enable streaming events if requested
    if ($EventsFormat -eq "jsonl") {
        Enable-StreamingEvents -RunId "verify-$runId"
    }
    Initialize-ProvisioningLog -RunId "verify-$runId" | Out-Null
    
    Write-ProvisioningSection "Provisioning Verify"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    
    # Read manifest
    if (-not (Test-Path $ManifestPath)) {
        Write-ProvisioningLog "Manifest not found: $ManifestPath" -Level ERROR
        return $null
    }
    
    $manifest = Read-Manifest -Path $ManifestPath
    Write-ProvisioningLog "Manifest loaded: $($manifest.name)" -Level SUCCESS
    
    # Get installed apps
    Write-ProvisioningSection "Verifying Applications"
    Write-PhaseEvent -Phase "verify"
    $installedApps = Invoke-DriverGetInstalledPackages
    $driverName = Get-ActiveDriverName
    
    $passCount = 0
    $failCount = 0
    $results = @()
    
    foreach ($app in $manifest.apps) {
        $windowsRef = $app.refs.windows
        if (-not $windowsRef) { continue }
        
        $isInstalled = $installedApps -contains $windowsRef
        
        $result = @{
            type = "app"
            id = $app.id
            ref = $windowsRef
        }
        
        if ($isInstalled) {
            Write-ProvisioningLog "$windowsRef - INSTALLED" -Level SUCCESS
            Write-ItemEvent -Id $windowsRef -Driver $driverName -Status "present" -Message "Verified installed"
            $result.status = "pass"
            $passCount++
        } else {
            Write-ProvisioningLog "$windowsRef - NOT INSTALLED" -Level ERROR
            Write-ItemEvent -Id $windowsRef -Driver $driverName -Status "failed" -Reason "missing" -Message "Missing - not installed"
            $result.status = "fail"
            $result.reason = "missing"
            $failCount++
        }
        
        $results += $result
    }
    
    # Run explicit verify items
    if ($manifest.verify -and $manifest.verify.Count -gt 0) {
        Write-ProvisioningSection "Running Verifiers"
        
        foreach ($item in $manifest.verify) {
            $result = @{
                type = "verify"
                verifyType = $item.type
            }
            
            $verifyResult = $null
            
            switch ($item.type) {
                "file-exists" {
                    $result.path = $item.path
                    $verifyResult = Test-FileExistsVerifier -Path $item.path
                }
                "command-exists" {
                    $result.command = $item.command
                    $verifyResult = Test-CommandExistsVerifier -Command $item.command
                }
                "registry-key-exists" {
                    $result.path = $item.path
                    $result.name = $item.name
                    $verifyResult = Test-RegistryKeyExistsVerifier -Path $item.path -Name $item.name
                }
                default {
                    $verifyResult = @{ Success = $false; Message = "Unknown verify type: $($item.type)" }
                }
            }
            
            if ($verifyResult.Success) {
                Write-ProvisioningLog "PASS: $($item.type) - $($verifyResult.Message)" -Level SUCCESS
                $result.status = "pass"
                $passCount++
            } else {
                Write-ProvisioningLog "FAIL: $($item.type) - $($verifyResult.Message)" -Level ERROR
                $result.status = "fail"
                $failCount++
            }
            
            $results += $result
        }
    }
    
    # Summary
    Write-ProvisioningSection "Verification Results"
    Write-SummaryEvent -Phase "verify" -Total ($passCount + $failCount) -Success $passCount -Skipped 0 -Failed $failCount
    Close-ProvisioningLog -SuccessCount $passCount -SkipCount 0 -FailCount $failCount
    
    # Save verification state
    $verifyState = @{
        runId = $runId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
        }
        summary = @{
            pass = $passCount
            fail = $failCount
            total = $passCount + $failCount
        }
        verification = $results
    }
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    $stateFile = [System.IO.Path]::GetFullPath((Join-Path $stateDir "verify-$runId.json"))
    $verifyState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    if ($OutputJson) {
        # Output JSON envelope
        . "$PSScriptRoot\json-output.ps1"
        
        $logsDir = Join-Path $PSScriptRoot "..\logs"
        $logFile = [System.IO.Path]::GetFullPath((Join-Path $logsDir "verify-$runId.log"))
        
        $data = [ordered]@{
            manifest = [ordered]@{
                path = $ManifestPath
                name = $manifest.name
            }
            summary = [ordered]@{
                total = $passCount + $failCount
                pass = $passCount
                fail = $failCount
            }
            results = @($results | ForEach-Object {
                $item = [ordered]@{
                    type = $_.type
                    status = $_.status
                }
                if ($_.verifyType) { $item.verifyType = $_.verifyType }
                if ($_.id) { $item.id = $_.id }
                if ($_.ref) { $item.ref = $_.ref }
                if ($_.path) { $item.path = $_.path }
                if ($_.command) { $item.command = $_.command }
                if ($_.message) { $item.message = $_.message }
                $item
            })
            runId = $runId
            stateFile = $stateFile
            logFile = $logFile
        }
        
        # Add eventsFile if events are enabled
        if ($EventsFormat -eq "jsonl") {
            $eventsFile = [System.IO.Path]::GetFullPath((Join-Path $logsDir "verify-$runId.events.jsonl"))
            $data['eventsFile'] = $eventsFile
        }
        
        # Create error object if there are failures
        $verifyError = $null
        if ($failCount -gt 0) {
            # Collect failed items for error message
            $failedApps = @($results | Where-Object { $_.type -eq "app" -and $_.status -eq "fail" } | ForEach-Object { $_.ref })
            $failedVerifiers = @($results | Where-Object { $_.type -eq "verify" -and $_.status -eq "fail" })
            
            $messageParts = @()
            if ($failedApps.Count -gt 0) {
                $messageParts += "Missing apps: $($failedApps -join ', ')"
            }
            if ($failedVerifiers.Count -gt 0) {
                $messageParts += "$($failedVerifiers.Count) verification(s) failed"
            }
            
            $verifyError = New-JsonError `
                -Code (Get-ErrorCode -Name "VERIFY_FAILED") `
                -Message ($messageParts -join "; ") `
                -Detail @{
                    missingApps = $failedApps
                    failedVerifierCount = $failedVerifiers.Count
                    manifestPath = $ManifestPath
                } `
                -Remediation "Run 'endstate apply --manifest `"$ManifestPath`"' to install missing apps"
        }
        
        $envelope = New-JsonEnvelope -Command "verify" -RunId $runId -Success ($failCount -eq 0) -Data $data -Error $verifyError
        Write-JsonOutput -Envelope $envelope
    } else {
        Write-Host ""
        if ($failCount -eq 0) {
            Write-Host "All verifications passed!" -ForegroundColor Green
        } else {
            Write-Host "$failCount verification(s) failed." -ForegroundColor Yellow
            Write-Host ""
            Write-Host "To fix missing items:" -ForegroundColor Yellow
            Write-Host "  .\cli.ps1 -Command apply -Manifest `"$ManifestPath`""
        }
        Write-Host ""
    }
    
    return @{
        RunId = $runId
        Pass = $passCount
        Fail = $failCount
        Results = $results
    }
}

function Invoke-VerifyItem {
    <#
    .SYNOPSIS
        Pure function to run a single verification item.
    .DESCRIPTION
        Runs a verifier and returns structured result. Suitable for unit testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Item
    )
    
    $result = @{
        type = "verify"
        verifyType = $Item.type
        status = "fail"
        message = ""
    }
    
    $verifyResult = $null
    
    switch ($Item.type) {
        "file-exists" {
            $result.path = $Item.path
            $verifyResult = Test-FileExistsVerifier -Path $Item.path
        }
        "command-exists" {
            $result.command = $Item.command
            $verifyResult = Test-CommandExistsVerifier -Command $Item.command
        }
        "registry-key-exists" {
            $result.path = $Item.path
            $result.name = $Item.name
            $verifyResult = Test-RegistryKeyExistsVerifier -Path $Item.path -Name $Item.name
        }
        default {
            $verifyResult = @{ Success = $false; Message = "Unknown verify type: $($Item.type)" }
        }
    }
    
    if ($verifyResult.Success) {
        $result.status = "pass"
    } else {
        $result.status = "fail"
    }
    $result.message = $verifyResult.Message
    
    return $result
}

# Functions exported: Invoke-Verify, Invoke-VerifyItem
