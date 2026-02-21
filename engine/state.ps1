# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning state management.

.DESCRIPTION
    Records run history, manifest hashes, and action outcomes.
#>

function Get-ManifestHash {
    <#
    .SYNOPSIS
        Compute hash of the raw manifest file on disk.
    .DESCRIPTION
        Returns a truncated SHA256 hash of the manifest file as-is on disk.
        This hash reflects the source file content, NOT the expanded/resolved manifest.
        Use Get-ExpandedManifestHash for a hash that reflects the actual executed state.
    .PARAMETER ManifestPath
        Path to the manifest file.
    .OUTPUTS
        16-character hex string (truncated SHA256), or $null if file not found.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath
    )
    
    if (-not (Test-Path $ManifestPath)) {
        return $null
    }
    
    $content = Get-Content -Path $ManifestPath -Raw
    $normalized = $content -replace "`r`n", "`n"
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalized)
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    $hashBytes = $sha256.ComputeHash($bytes)
    $hashString = [BitConverter]::ToString($hashBytes) -replace '-', ''
    return $hashString.ToLower().Substring(0, 16)
}

function Get-ExpandedManifestHash {
    <#
    .SYNOPSIS
        Compute hash of the fully expanded/resolved manifest.
    .DESCRIPTION
        Loads the manifest, resolves all includes, expands configModules,
        normalizes the structure, then hashes the resulting JSON.
        
        This hash reflects the actual desired state that will be executed,
        including all expanded restore/verify items from config modules.
        
        Use this for drift detection and state comparison where you need
        to know if the effective configuration has changed.
    .PARAMETER ManifestPath
        Path to the manifest file.
    .PARAMETER Manifest
        Optional: pre-loaded expanded manifest hashtable. If provided, ManifestPath is ignored.
    .OUTPUTS
        16-character hex string (truncated SHA256), or $null on error.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [hashtable]$Manifest
    )
    
    try {
        # Load and expand manifest if not provided
        if (-not $Manifest) {
            if (-not $ManifestPath -or -not (Test-Path $ManifestPath)) {
                return $null
            }
            
            # Import manifest module if needed
            $manifestModule = Join-Path $PSScriptRoot "manifest.ps1"
            if (Test-Path $manifestModule) {
                . $manifestModule
            }
            
            $Manifest = Read-Manifest -Path $ManifestPath
        }
        
        # Create a normalized copy for hashing (remove internal/transient fields)
        $hashableManifest = @{}
        foreach ($key in $Manifest.Keys) {
            # Skip internal fields (prefixed with _)
            if (-not $key.StartsWith('_')) {
                $hashableManifest[$key] = $Manifest[$key]
            }
        }
        
        # Sort keys for deterministic JSON output
        $sortedKeys = $hashableManifest.Keys | Sort-Object
        $orderedManifest = [ordered]@{}
        foreach ($key in $sortedKeys) {
            $orderedManifest[$key] = $hashableManifest[$key]
        }
        
        # Convert to JSON and hash
        $json = $orderedManifest | ConvertTo-Json -Depth 20 -Compress
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
        $sha256 = [System.Security.Cryptography.SHA256]::Create()
        $hashBytes = $sha256.ComputeHash($bytes)
        $hashHex = [BitConverter]::ToString($hashBytes) -replace '-', ''
        
        return $hashHex.Substring(0, 16)
    } catch {
        Write-Warning "Failed to compute expanded manifest hash: $($_.Exception.Message)"
        return $null
    }
}

function Save-RunState {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RunId,
        
        [Parameter(Mandatory = $false)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [string]$ManifestHash,
        
        [Parameter(Mandatory = $true)]
        [string]$Command,
        
        [Parameter(Mandatory = $false)]
        [bool]$DryRun = $false,
        
        [Parameter(Mandatory = $false)]
        [array]$Actions = @(),
        
        [Parameter(Mandatory = $false)]
        [int]$SuccessCount = 0,
        
        [Parameter(Mandatory = $false)]
        [int]$SkipCount = 0,
        
        [Parameter(Mandatory = $false)]
        [int]$FailCount = 0
    )
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    
    $stateFile = Join-Path $stateDir "$RunId.json"
    
    $state = @{
        runId = $RunId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        machine = $env:COMPUTERNAME
        user = $env:USERNAME
        command = $Command
        dryRun = $DryRun
        manifest = @{
            path = $ManifestPath
            hash = $ManifestHash
        }
        summary = @{
            success = $SuccessCount
            skipped = $SkipCount
            failed = $FailCount
        }
        actions = $Actions
    }
    
    $tempFile = "$stateFile.tmp"
    $state | ConvertTo-Json -Depth 10 | Out-File -FilePath $tempFile -Encoding UTF8
    Move-Item -Path $tempFile -Destination $stateFile -Force

    return $stateFile
}

function Get-LastRunState {
    $stateDir = Join-Path $PSScriptRoot "..\state"
    
    if (-not (Test-Path $stateDir)) {
        return $null
    }
    
    $stateFiles = Get-ChildItem -Path $stateDir -Filter "*.json" | Sort-Object Name -Descending
    
    if ($stateFiles.Count -eq 0) {
        return $null
    }
    
    $lastState = Get-Content -Path $stateFiles[0].FullName -Raw | ConvertFrom-Json
    return $lastState
}

function Get-RunHistory {
    param(
        [Parameter(Mandatory = $false)]
        [int]$Limit = 10
    )
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    
    if (-not (Test-Path $stateDir)) {
        return @()
    }
    
    $stateFiles = Get-ChildItem -Path $stateDir -Filter "*.json" | Sort-Object Name -Descending | Select-Object -First $Limit
    
    $history = @()
    foreach ($file in $stateFiles) {
        try {
            $state = Get-Content -Path $file.FullName -Raw | ConvertFrom-Json
            $history += $state
        } catch {
            # Skip corrupted state files
        }
    }
    
    return $history
}

# Functions exported: Get-ManifestHash, Get-ExpandedManifestHash, Save-RunState, Get-LastRunState, Get-RunHistory
