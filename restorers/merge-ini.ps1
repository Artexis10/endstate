# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    INI merge restorer for Provisioning.

.DESCRIPTION
    Merges INI source into target file with:
    - Section-aware parsing
    - Key overwrite/add within sections
    - Preserves existing keys not in source
    - Note: Comments are NOT preserved (acceptable for v1)
#>

. "$PSScriptRoot\helpers.ps1"

function ConvertFrom-IniContent {
    <#
    .SYNOPSIS
        Parse INI content into a hashtable structure.
    .DESCRIPTION
        Returns @{ SectionName = @{ Key = Value } }
        Global keys (before any section) go under "" empty string key.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$Content
    )
    
    $result = [ordered]@{}
    $currentSection = ""
    $result[$currentSection] = [ordered]@{}
    
    if (-not $Content) {
        return $result
    }
    
    $lines = $Content -split "`r?`n"
    
    foreach ($line in $lines) {
        $trimmed = $line.Trim()
        
        # Skip empty lines and comments
        if (-not $trimmed -or $trimmed.StartsWith(";") -or $trimmed.StartsWith("#")) {
            continue
        }
        
        # Section header [SectionName]
        if ($trimmed -match '^\[([^\]]+)\]$') {
            $currentSection = $Matches[1]
            if (-not $result.Contains($currentSection)) {
                $result[$currentSection] = [ordered]@{}
            }
            continue
        }
        
        # Key=Value or Key = Value
        if ($trimmed -match '^([^=]+)=(.*)$') {
            $key = $Matches[1].Trim()
            $value = $Matches[2].Trim()
            $result[$currentSection][$key] = $value
        }
    }
    
    return $result
}

function ConvertTo-IniContent {
    <#
    .SYNOPSIS
        Convert hashtable structure back to INI format.
    .DESCRIPTION
        Outputs deterministic INI with sorted sections and keys.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $IniData
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    # Get sorted section names, but put global (empty) section first
    $sections = @($IniData.Keys) | Sort-Object | Where-Object { $_ -ne "" }
    $hasGlobal = $IniData.Contains("") -and $IniData[""].Count -gt 0
    
    # Output global keys first (no section header)
    if ($hasGlobal) {
        $globalSection = $IniData[""]
        foreach ($key in $globalSection.Keys | Sort-Object) {
            $val = $globalSection[$key]
            [void]$sb.AppendLine("$key=$val")
        }
        if ($sections.Count -gt 0) {
            [void]$sb.AppendLine("")
        }
    }
    
    # Output each section
    $sectionIndex = 0
    foreach ($section in $sections) {
        $sectionIndex++
        [void]$sb.AppendLine("[$section]")
        
        $sectionData = $IniData[$section]
        foreach ($key in $sectionData.Keys | Sort-Object) {
            $val = $sectionData[$key]
            [void]$sb.AppendLine("$key=$val")
        }
        
        # Add blank line between sections (but not after last)
        if ($sectionIndex -lt $sections.Count) {
            [void]$sb.AppendLine("")
        }
    }
    
    return $sb.ToString().TrimEnd("`r`n")
}

function Merge-IniData {
    <#
    .SYNOPSIS
        Merge source INI data into target INI data.
    .DESCRIPTION
        - Keys from source overwrite/add into target
        - Existing keys in target not in source are preserved
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Target,
        
        [Parameter(Mandatory = $true)]
        $Source
    )
    
    $result = [ordered]@{}
    
    # Copy all target sections and keys first
    foreach ($section in $Target.Keys) {
        $result[$section] = [ordered]@{}
        foreach ($key in $Target[$section].Keys) {
            $result[$section][$key] = $Target[$section][$key]
        }
    }
    
    # Merge source sections and keys
    foreach ($section in $Source.Keys) {
        if (-not $result.Contains($section)) {
            $result[$section] = [ordered]@{}
        }
        foreach ($key in $Source[$section].Keys) {
            $result[$section][$key] = $Source[$section][$key]
        }
    }
    
    return $result
}

function Invoke-IniMergeRestore {
    <#
    .SYNOPSIS
        Merge INI source into target file.
    .DESCRIPTION
        Merges source INI into target, creating target if missing.
        Comments are NOT preserved in v1.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Target,
        
        [Parameter(Mandatory = $false)]
        [bool]$Backup = $true,
        
        [Parameter(Mandatory = $false)]
        [string]$RunId = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$ManifestDir = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$ExportPath = $null,
        
        [Parameter(Mandatory = $false)]
        [switch]$DryRun
    )
    
    $result = @{
        Success = $false
        Skipped = $false
        BackupPath = $null
        Message = $null
        Error = $null
        Warnings = @("INI merge does not preserve comments")
    }
    
    # Expand paths with Model B support
    $expandedSource = $null
    if ($ExportPath -and (Test-Path $ExportPath)) {
        $exportSource = Expand-RestorePathHelper -Path $Source -BasePath $ExportPath
        if (Test-Path $exportSource) {
            $expandedSource = $exportSource
        }
    }
    if (-not $expandedSource) {
        $expandedSource = Expand-RestorePathHelper -Path $Source -BasePath $ManifestDir
    }
    $expandedTarget = Expand-RestorePathHelper -Path $Target
    
    # Check source exists
    if (-not (Test-Path $expandedSource)) {
        $result.Error = "Source not found: $expandedSource"
        return $result
    }
    
    try {
        # Read source INI
        $sourceContent = Read-TextFileUtf8 -Path $expandedSource
        $sourceData = ConvertFrom-IniContent -Content $sourceContent
        
        # Read target INI if exists, otherwise empty
        $targetData = [ordered]@{ "" = [ordered]@{} }
        if (Test-Path $expandedTarget) {
            $targetContent = Read-TextFileUtf8 -Path $expandedTarget
            if ($targetContent) {
                $targetData = ConvertFrom-IniContent -Content $targetContent
            }
        }
        
        # Perform merge
        $mergedData = Merge-IniData -Target $targetData -Source $sourceData
        
        # Convert to INI content
        $mergedIni = ConvertTo-IniContent -IniData $mergedData
        
        # Check if already up-to-date
        if (Test-Path $expandedTarget) {
            $existingContent = Read-TextFileUtf8 -Path $expandedTarget
            $existingData = ConvertFrom-IniContent -Content $existingContent
            $existingIni = ConvertTo-IniContent -IniData $existingData
            
            if ($existingIni -eq $mergedIni) {
                $result.Success = $true
                $result.Skipped = $true
                $result.Message = "already up to date"
                return $result
            }
        }
        
        # Dry-run mode
        if ($DryRun) {
            $result.Success = $true
            $result.Message = "Would merge INI $expandedSource -> $expandedTarget"
            return $result
        }
        
        # Backup existing target
        if ($Backup -and (Test-Path $expandedTarget)) {
            $backupRunId = if ($RunId) { $RunId } else { Get-Date -Format 'yyyyMMdd-HHmmss' }
            $backupResult = Invoke-RestoreBackup -Target $expandedTarget -RunId $backupRunId
            if (-not $backupResult.Success) {
                $result.Error = "Backup failed: $($backupResult.Error)"
                return $result
            }
            $result.BackupPath = $backupResult.BackupPath
        }
        
        # Write merged INI atomically
        Write-TextFileUtf8Atomic -Path $expandedTarget -Content $mergedIni
        
        $result.Success = $true
        $result.Message = "Merged INI successfully"
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

# Functions exported: Invoke-IniMergeRestore, ConvertFrom-IniContent, ConvertTo-IniContent, Merge-IniData
