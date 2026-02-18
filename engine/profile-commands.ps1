# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    CLI profile management commands.

.DESCRIPTION
    Functions for creating, modifying, and inspecting overlay profiles.
    All mutations use Read-ManifestRaw + modify + Write-Manifest (never Read-Manifest for writes).
    Only bare .jsonc profiles are mutable — zip and folder profiles are read-only.
#>

# Import dependencies
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\bundle.ps1"

function Assert-BareProfile {
    <#
    .SYNOPSIS
        Validate that a resolved profile is a bare .jsonc file (mutable).
    .DESCRIPTION
        Throws if the profile resolves to zip or folder format.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$ProfileResult,
        
        [Parameter(Mandatory = $true)]
        [string]$Name
    )
    
    if (-not $ProfileResult.Found) {
        throw "Profile not found: $Name"
    }
    
    if ($ProfileResult.Format -ne "bare") {
        throw "Profile '$Name' is a $($ProfileResult.Format) profile and cannot be modified. Only bare .jsonc profiles are mutable."
    }
}

function New-ProfileOverlay {
    <#
    .SYNOPSIS
        Create a new overlay profile manifest.
    .PARAMETER Name
        Profile name (used as filename).
    .PARAMETER From
        Optional base profile name to include.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .PARAMETER Json
        If true, return result for JSON envelope output.
    .OUTPUTS
        The created file path.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $false)]
        [string]$From,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir,
        
        [Parameter(Mandatory = $false)]
        [switch]$Json
    )
    
    $outputPath = Join-Path $ProfilesDir "$Name.jsonc"
    
    if (Test-Path $outputPath) {
        throw "Profile already exists: $outputPath"
    }
    
    if ($From) {
        $manifest = @{
            version = 1
            name = $Name
            includes = @($From)
            exclude = @()
            excludeConfigs = @()
            apps = @()
        }
    } else {
        $manifest = @{
            version = 1
            name = $Name
            apps = @()
        }
    }
    
    Write-Manifest -Path $outputPath -Manifest $manifest | Out-Null
    
    return $outputPath
}

function Add-ProfileExclusion {
    <#
    .SYNOPSIS
        Append winget IDs to a profile's exclude array.
    .PARAMETER Name
        Profile name.
    .PARAMETER Ids
        Winget IDs to add to the exclude list.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .OUTPUTS
        Count of newly added entries.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $true)]
        [string[]]$Ids,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir
    )
    
    $profileResult = Resolve-ProfilePath -ProfileName $Name -ProfilesDir $ProfilesDir
    Assert-BareProfile -ProfileResult $profileResult -Name $Name
    
    $manifest = Read-ManifestRaw -Path $profileResult.Path
    if (-not $manifest) {
        throw "Failed to read profile: $Name"
    }
    
    if (-not $manifest.ContainsKey('exclude') -or $null -eq $manifest.exclude) {
        $manifest.exclude = @()
    }
    
    $added = 0
    foreach ($id in $Ids) {
        if ($id -notin $manifest.exclude) {
            $manifest.exclude = @($manifest.exclude) + @($id)
            $added++
        }
    }
    
    Write-Manifest -Path $profileResult.Path -Manifest $manifest | Out-Null
    
    return $added
}

function Add-ProfileExcludeConfig {
    <#
    .SYNOPSIS
        Append config module IDs to a profile's excludeConfigs array.
    .PARAMETER Name
        Profile name.
    .PARAMETER Ids
        Config module IDs to add to the excludeConfigs list.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .OUTPUTS
        Count of newly added entries.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $true)]
        [string[]]$Ids,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir
    )
    
    $profileResult = Resolve-ProfilePath -ProfileName $Name -ProfilesDir $ProfilesDir
    Assert-BareProfile -ProfileResult $profileResult -Name $Name
    
    $manifest = Read-ManifestRaw -Path $profileResult.Path
    if (-not $manifest) {
        throw "Failed to read profile: $Name"
    }
    
    if (-not $manifest.ContainsKey('excludeConfigs') -or $null -eq $manifest.excludeConfigs) {
        $manifest.excludeConfigs = @()
    }
    
    $added = 0
    foreach ($id in $Ids) {
        if ($id -notin $manifest.excludeConfigs) {
            $manifest.excludeConfigs = @($manifest.excludeConfigs) + @($id)
            $added++
        }
    }
    
    Write-Manifest -Path $profileResult.Path -Manifest $manifest | Out-Null
    
    return $added
}

function Add-ProfileApp {
    <#
    .SYNOPSIS
        Append app entries to a profile's apps array.
    .PARAMETER Name
        Profile name.
    .PARAMETER Ids
        Winget IDs to add as app entries.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .OUTPUTS
        Count of newly added entries.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $true)]
        [string[]]$Ids,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir
    )
    
    $profileResult = Resolve-ProfilePath -ProfileName $Name -ProfilesDir $ProfilesDir
    Assert-BareProfile -ProfileResult $profileResult -Name $Name
    
    $manifest = Read-ManifestRaw -Path $profileResult.Path
    if (-not $manifest) {
        throw "Failed to read profile: $Name"
    }
    
    if (-not $manifest.ContainsKey('apps') -or $null -eq $manifest.apps) {
        $manifest.apps = @()
    }
    
    $added = 0
    foreach ($id in $Ids) {
        # Skip if any existing app has refs.windows matching the ID
        $exists = $false
        foreach ($app in $manifest.apps) {
            if ($app.refs -and $app.refs.windows -eq $id) {
                $exists = $true
                break
            }
        }
        
        if (-not $exists) {
            $newApp = @{
                id = $id
                refs = @{
                    windows = $id
                }
            }
            $manifest.apps = @($manifest.apps) + @($newApp)
            $added++
        }
    }
    
    Write-Manifest -Path $profileResult.Path -Manifest $manifest | Out-Null
    
    return $added
}

function Get-ProfileSummary {
    <#
    .SYNOPSIS
        Read and summarize a profile.
    .PARAMETER Name
        Profile name.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .PARAMETER Json
        If true, return structured data for JSON envelope.
    .OUTPUTS
        Summary object or formatted text.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir,
        
        [Parameter(Mandatory = $false)]
        [switch]$Json
    )
    
    $profileResult = Resolve-ProfilePath -ProfileName $Name -ProfilesDir $ProfilesDir
    if (-not $profileResult.Found) {
        throw "Profile not found: $Name"
    }
    
    # Read raw manifest for overlay fields
    $rawManifest = Read-ManifestRaw -Path $profileResult.Path
    if (-not $rawManifest) {
        throw "Failed to read profile: $Name"
    }
    
    # Read resolved manifest for net app count
    $resolvedManifest = Read-Manifest -Path $profileResult.Path
    
    $base = $null
    $baseAppCount = 0
    if ($rawManifest.includes -and $rawManifest.includes.Count -gt 0) {
        $base = $rawManifest.includes[0]
        # Count apps from resolved minus local apps
        $localAppCount = if ($rawManifest.apps) { @($rawManifest.apps).Count } else { 0 }
        $netAppCount = if ($resolvedManifest.apps) { @($resolvedManifest.apps).Count } else { 0 }
        $baseAppCount = $netAppCount - $localAppCount
        if ($baseAppCount -lt 0) { $baseAppCount = 0 }
    }
    
    $excludedCount = if ($rawManifest.exclude) { @($rawManifest.exclude).Count } else { 0 }
    $excludedConfigsCount = if ($rawManifest.excludeConfigs) { @($rawManifest.excludeConfigs).Count } else { 0 }
    $addedCount = if ($rawManifest.apps) { @($rawManifest.apps).Count } else { 0 }
    $netAppCount = if ($resolvedManifest.apps) { @($resolvedManifest.apps).Count } else { 0 }
    
    $summary = @{
        name = $Name
        format = $profileResult.Format
        base = $base
        baseAppCount = $baseAppCount
        excludedCount = $excludedCount
        excludedConfigsCount = $excludedConfigsCount
        addedCount = $addedCount
        netAppCount = $netAppCount
    }
    
    if ($Json) {
        return $summary
    }
    
    # Human-readable output
    Write-Host ""
    Write-Host "Profile: $Name" -ForegroundColor Cyan
    Write-Host "  Format:            $($profileResult.Format)" -ForegroundColor Gray
    if ($base) {
        Write-Host "  Base:              $base" -ForegroundColor Gray
        Write-Host "  Base apps:         $baseAppCount" -ForegroundColor Gray
    }
    Write-Host "  Excluded apps:     $excludedCount" -ForegroundColor Gray
    Write-Host "  Excluded configs:  $excludedConfigsCount" -ForegroundColor Gray
    Write-Host "  Added apps:        $addedCount" -ForegroundColor Gray
    Write-Host "  Net app count:     $netAppCount" -ForegroundColor Green
    Write-Host ""
    
    return $summary
}

function Get-ProfileList {
    <#
    .SYNOPSIS
        List all profiles in the profiles directory.
    .PARAMETER ProfilesDir
        Directory where profiles are stored.
    .PARAMETER Json
        If true, return structured data for JSON envelope.
    .OUTPUTS
        Array of profile summary objects.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir,
        
        [Parameter(Mandatory = $false)]
        [switch]$Json
    )
    
    $profiles = @()
    
    if (-not (Test-Path $ProfilesDir)) {
        if ($Json) {
            return $profiles
        }
        Write-Host "No profiles directory found: $ProfilesDir" -ForegroundColor Yellow
        return $profiles
    }
    
    # Scan for zip bundles
    $zips = Get-ChildItem -Path $ProfilesDir -Filter "*.zip" -File -ErrorAction SilentlyContinue
    foreach ($zip in $zips) {
        $name = [System.IO.Path]::GetFileNameWithoutExtension($zip.Name)
        $profiles += @{
            name = $name
            type = "bundle"
            format = "zip"
            path = $zip.FullName
        }
    }
    
    # Scan for folder profiles (contain manifest.jsonc)
    $folders = Get-ChildItem -Path $ProfilesDir -Directory -ErrorAction SilentlyContinue
    foreach ($folder in $folders) {
        $folderManifest = Join-Path $folder.FullName "manifest.jsonc"
        if (Test-Path $folderManifest) {
            $name = $folder.Name
            # Skip if already found as zip
            if ($profiles | Where-Object { $_.name -eq $name }) { continue }
            $profiles += @{
                name = $name
                type = "folder"
                format = "folder"
                path = $folderManifest
            }
        }
    }
    
    # Scan for bare .jsonc profiles
    $jsoncs = Get-ChildItem -Path $ProfilesDir -Filter "*.jsonc" -File -ErrorAction SilentlyContinue
    foreach ($jsonc in $jsoncs) {
        $name = [System.IO.Path]::GetFileNameWithoutExtension($jsonc.Name)
        # Skip if already found as zip or folder
        if ($profiles | Where-Object { $_.name -eq $name }) { continue }
        
        # Read manifest to determine type
        try {
            $manifest = Read-ManifestRaw -Path $jsonc.FullName
            $type = if ($manifest.includes -and $manifest.includes.Count -gt 0) { "overlay" } else { "bare" }
            $appCount = if ($manifest.apps) { @($manifest.apps).Count } else { 0 }
        } catch {
            $type = "bare"
            $appCount = 0
        }
        
        $profiles += @{
            name = $name
            type = $type
            format = "bare"
            appCount = $appCount
            path = $jsonc.FullName
        }
    }
    
    # Sort by name
    $profiles = @($profiles | Sort-Object { $_.name })
    
    if ($Json) {
        return $profiles
    }
    
    # Human-readable output
    if ($profiles.Count -eq 0) {
        Write-Host "No profiles found in: $ProfilesDir" -ForegroundColor Yellow
        return $profiles
    }
    
    Write-Host ""
    Write-Host "Profiles:" -ForegroundColor Cyan
    Write-Host ""
    
    foreach ($p in $profiles) {
        $typeLabel = switch ($p.type) {
            "overlay" { "[overlay]" }
            "bundle"  { "[bundle]" }
            "folder"  { "[folder]" }
            default   { "[bare]" }
        }
        $countLabel = if ($null -ne $p.appCount) { " ($($p.appCount) apps)" } else { "" }
        Write-Host "  $($p.name) $typeLabel$countLabel" -ForegroundColor White
    }
    
    Write-Host ""
    Write-Host "Total: $($profiles.Count) profiles" -ForegroundColor Gray
    Write-Host ""
    
    return $profiles
}
