# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Append/line merge restorer for Provisioning.

.DESCRIPTION
    Appends lines from source text file into target if not already present.
    - Idempotent: only adds missing lines
    - Dedupe: removes duplicate lines (default true)
    - Newline handling: auto-detects or uses system default
#>

. "$PSScriptRoot\helpers.ps1"

function Get-NormalizedLines {
    <#
    .SYNOPSIS
        Split content into lines and normalize.
    .DESCRIPTION
        Returns array of lines, trimming trailing whitespace from each.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [AllowNull()]
        [AllowEmptyString()]
        [string]$Content
    )
    
    if (-not $Content) {
        return @()
    }
    
    # Split on any newline style
    $lines = $Content -split "`r?`n"
    
    # Trim trailing whitespace from each line but preserve leading
    # Filter out empty lines at the end (trailing newlines)
    $trimmed = @($lines | ForEach-Object { $_.TrimEnd() })
    
    # Remove trailing empty lines
    while ($trimmed.Count -gt 0 -and $trimmed[-1] -eq "") {
        $trimmed = $trimmed[0..($trimmed.Count - 2)]
    }
    
    return $trimmed
}

function Merge-AppendLines {
    <#
    .SYNOPSIS
        Merge source lines into target, adding only missing lines.
    .DESCRIPTION
        - Adds lines from source that are not in target
        - Optionally dedupes the result
        - Maintains deterministic order
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$TargetLines,
        
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$SourceLines,
        
        [Parameter(Mandatory = $false)]
        [bool]$Dedupe = $true
    )
    
    $result = @()
    $seen = @{}
    
    # Add all target lines first
    foreach ($line in $TargetLines) {
        if ($Dedupe) {
            if (-not $seen.ContainsKey($line)) {
                $seen[$line] = $true
                $result += $line
            }
        } else {
            $result += $line
        }
    }
    
    # Add source lines not already present
    foreach ($line in $SourceLines) {
        if (-not $seen.ContainsKey($line)) {
            $seen[$line] = $true
            $result += $line
        }
    }
    
    return $result
}

function Get-NewlineStyle {
    <#
    .SYNOPSIS
        Detect the newline style used in content.
    .DESCRIPTION
        Returns "`r`n" for Windows, "`n" for Unix.
        Defaults to system preference if no newlines found.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [AllowNull()]
        [AllowEmptyString()]
        [string]$Content
    )
    
    if (-not $Content) {
        # Default to system newline
        return [Environment]::NewLine
    }
    
    # Count CRLF vs LF
    $crlfCount = ([regex]::Matches($Content, "`r`n")).Count
    $lfOnlyCount = ([regex]::Matches($Content, "(?<!\r)`n")).Count
    
    if ($crlfCount -gt $lfOnlyCount) {
        return "`r`n"
    } elseif ($lfOnlyCount -gt 0) {
        return "`n"
    }
    
    # Default to system newline
    return [Environment]::NewLine
}

function Invoke-AppendRestore {
    <#
    .SYNOPSIS
        Append lines from source into target file.
    .DESCRIPTION
        Adds lines from source that are not already in target.
        Idempotent: re-running produces same result.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Target,
        
        [Parameter(Mandatory = $false)]
        [bool]$Backup = $true,
        
        [Parameter(Mandatory = $false)]
        [bool]$Dedupe = $true,
        
        [Parameter(Mandatory = $false)]
        [ValidateSet("auto", "crlf", "lf")]
        [string]$Newline = "auto",
        
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
        Warnings = @()
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
        # Read source lines
        $sourceContent = Read-TextFileUtf8 -Path $expandedSource
        $sourceLines = Get-NormalizedLines -Content $sourceContent
        
        # Read target lines if exists
        $targetContent = ""
        $targetLines = @()
        if (Test-Path $expandedTarget) {
            $targetContent = Read-TextFileUtf8 -Path $expandedTarget
            $targetLines = Get-NormalizedLines -Content $targetContent
        }
        
        # Merge lines
        $mergedLines = Merge-AppendLines -TargetLines $targetLines -SourceLines $sourceLines -Dedupe $Dedupe
        
        # Determine newline style
        $nl = switch ($Newline) {
            "crlf" { "`r`n" }
            "lf" { "`n" }
            default { 
                if (Test-Path $expandedTarget) {
                    Get-NewlineStyle -Content $targetContent
                } else {
                    [Environment]::NewLine
                }
            }
        }
        
        # Build merged content
        $mergedContent = ($mergedLines -join $nl)
        if ($mergedLines.Count -gt 0) {
            $mergedContent += $nl
        }
        
        # Check if already up-to-date
        # Compare merged lines with existing lines (content-based, not byte-based)
        if (Test-Path $expandedTarget) {
            $existingLines = Get-NormalizedLines -Content $targetContent
            
            # Check if merged result equals existing content
            $mergedSet = @{}
            foreach ($line in $mergedLines) { $mergedSet[$line] = $true }
            $existingSet = @{}
            foreach ($line in $existingLines) { $existingSet[$line] = $true }
            
            # Same lines in same order = up to date
            $isSame = $true
            if ($mergedLines.Count -ne $existingLines.Count) {
                $isSame = $false
            } else {
                for ($i = 0; $i -lt $mergedLines.Count; $i++) {
                    if ($mergedLines[$i] -ne $existingLines[$i]) {
                        $isSame = $false
                        break
                    }
                }
            }
            
            if ($isSame) {
                $result.Success = $true
                $result.Skipped = $true
                $result.Message = "already up to date"
                return $result
            }
        }
        
        # Dry-run mode
        if ($DryRun) {
            $result.Success = $true
            $result.Message = "Would append lines from $expandedSource -> $expandedTarget"
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
        
        # Write merged content atomically
        Write-TextFileUtf8Atomic -Path $expandedTarget -Content $mergedContent
        
        $result.Success = $true
        $result.Message = "Appended lines successfully"
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

# Functions exported: Invoke-AppendRestore, Get-NormalizedLines, Merge-AppendLines
