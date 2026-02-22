# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    JSON merge restorer for Provisioning.

.DESCRIPTION
    Deep-merges JSON/JSONC source into target file with:
    - Deep-merge for objects
    - Array handling: replace (default) or union
    - Scalar overwrite
    - Deterministic output (sorted keys, 2-space indent)
    - Backup-first safety
#>

. "$PSScriptRoot\helpers.ps1"

function Merge-JsonDeep {
    <#
    .SYNOPSIS
        Deep-merge two hashtables/objects.
    .DESCRIPTION
        - Objects: recursively merge
        - Arrays: replace or union based on strategy
        - Scalars: source overwrites target
    #>
    param(
        [Parameter(Mandatory = $false)]
        $Target,
        
        [Parameter(Mandatory = $false)]
        $Source,
        
        [Parameter(Mandatory = $false)]
        [ValidateSet("replace", "union")]
        [string]$ArrayStrategy = "replace"
    )
    
    # If source is null, keep target
    if ($null -eq $Source) {
        return $Target
    }
    
    # If target is null, use source
    if ($null -eq $Target) {
        return $Source
    }
    
    # Both are hashtables: deep merge
    if ($Target -is [hashtable] -and $Source -is [hashtable]) {
        $result = @{}
        
        # Copy all target keys first
        foreach ($key in $Target.Keys) {
            $result[$key] = $Target[$key]
        }
        
        # Merge/overwrite with source keys
        foreach ($key in $Source.Keys) {
            if ($result.ContainsKey($key)) {
                $result[$key] = Merge-JsonDeep -Target $result[$key] -Source $Source[$key] -ArrayStrategy $ArrayStrategy
            } else {
                $result[$key] = $Source[$key]
            }
        }
        
        return $result
    }
    
    # Both are arrays
    if ($Target -is [array] -and $Source -is [array]) {
        if ($ArrayStrategy -eq "union") {
            # Union: combine unique elements, maintain deterministic order
            $seen = @{}
            $union = @()
            
            # Add target elements first
            foreach ($item in $Target) {
                $key = ConvertTo-JsonSorted -Object $item -Compress
                if (-not $seen.ContainsKey($key)) {
                    $seen[$key] = $true
                    $union += $item
                }
            }
            
            # Add source elements not already present
            foreach ($item in $Source) {
                $key = ConvertTo-JsonSorted -Object $item -Compress
                if (-not $seen.ContainsKey($key)) {
                    $seen[$key] = $true
                    $union += $item
                }
            }
            
            return $union
        } else {
            # Replace: source array replaces target
            return $Source
        }
    }
    
    # Scalars or type mismatch: source wins
    return $Source
}

function ConvertTo-JsonSorted {
    <#
    .SYNOPSIS
        Convert object to JSON with sorted keys for deterministic output.
    #>
    param(
        [Parameter(Mandatory = $false)]
        $Object,
        
        [Parameter(Mandatory = $false)]
        [switch]$Compress,
        
        [Parameter(Mandatory = $false)]
        [int]$Depth = 100
    )
    
    $sorted = ConvertTo-SortedObject -Object $Object
    
    if ($Compress) {
        return $sorted | ConvertTo-Json -Depth $Depth -Compress
    } else {
        # Pretty print with 2-space indent
        $json = $sorted | ConvertTo-Json -Depth $Depth
        # PowerShell uses 4-space indent by default, convert to 2-space
        $lines = $json -split "`n"
        $result = @()
        foreach ($line in $lines) {
            if ($line -match '^(\s+)(.*)$') {
                $spaces = $Matches[1]
                $content = $Matches[2]
                $newSpaces = ' ' * ([Math]::Floor($spaces.Length / 2))
                $result += "$newSpaces$content"
            } else {
                $result += $line
            }
        }
        return ($result -join "`n")
    }
}

function ConvertTo-SortedObject {
    <#
    .SYNOPSIS
        Recursively sort hashtable keys for deterministic JSON output.
    #>
    param(
        [Parameter(Mandatory = $false)]
        $Object
    )
    
    if ($null -eq $Object) {
        return $null
    }
    
    if ($Object -is [hashtable]) {
        $sorted = [ordered]@{}
        foreach ($key in $Object.Keys | Sort-Object) {
            $sorted[$key] = ConvertTo-SortedObject -Object $Object[$key]
        }
        return $sorted
    }
    
    if ($Object -is [System.Collections.IEnumerable] -and $Object -isnot [string] -and $Object -isnot [hashtable]) {
        $arr = @()
        foreach ($item in $Object) {
            $arr += ConvertTo-SortedObject -Object $item
        }
        return $arr
    }
    
    if ($Object -is [PSCustomObject]) {
        $sorted = [ordered]@{}
        foreach ($prop in $Object.PSObject.Properties.Name | Sort-Object) {
            $sorted[$prop] = ConvertTo-SortedObject -Object $Object.PSObject.Properties[$prop].Value
        }
        return $sorted
    }
    
    return $Object
}

function ConvertTo-HashtableRecursive {
    <#
    .SYNOPSIS
        Recursively convert PSCustomObject to hashtable (PS 5.1 compat).
    #>
    param($InputObject)
    if ($null -eq $InputObject) { return $null }
    if ($InputObject -is [hashtable]) {
        $hash = @{}
        foreach ($key in $InputObject.Keys) {
            $hash[$key] = ConvertTo-HashtableRecursive -InputObject $InputObject[$key]
        }
        return $hash
    }
    if ($InputObject -is [System.Collections.IEnumerable] -and $InputObject -isnot [string]) {
        $arr = @()
        foreach ($item in $InputObject) {
            $arr += ConvertTo-HashtableRecursive -InputObject $item
        }
        return $arr
    }
    if ($InputObject -is [PSCustomObject]) {
        $hash = @{}
        foreach ($prop in $InputObject.PSObject.Properties) {
            $hash[$prop.Name] = ConvertTo-HashtableRecursive -InputObject $prop.Value
        }
        return $hash
    }
    return $InputObject
}

function ConvertFrom-JsoncContent {
    <#
    .SYNOPSIS
        Parse JSONC (JSON with comments) content.
    .DESCRIPTION
        Strips single-line (//) and multi-line comments before parsing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Content
    )
    
    # Remove single-line and multi-line comments
    $inString = $false
    $escaped = $false
    $result = [System.Text.StringBuilder]::new()
    $i = 0
    
    while ($i -lt $Content.Length) {
        $char = $Content[$i]
        $nextChar = if ($i + 1 -lt $Content.Length) { $Content[$i + 1] } else { $null }
        
        if ($escaped) {
            [void]$result.Append($char)
            $escaped = $false
            $i++
            continue
        }
        
        if ($char -eq '\' -and $inString) {
            [void]$result.Append($char)
            $escaped = $true
            $i++
            continue
        }
        
        if ($char -eq '"' -and -not $escaped) {
            $inString = -not $inString
            [void]$result.Append($char)
            $i++
            continue
        }
        
        if (-not $inString) {
            # Single-line comment
            if ($char -eq '/' -and $nextChar -eq '/') {
                while ($i -lt $Content.Length -and $Content[$i] -ne "`n") {
                    $i++
                }
                continue
            }
            
            # Multi-line comment
            if ($char -eq '/' -and $nextChar -eq '*') {
                $i += 2
                while ($i -lt $Content.Length - 1) {
                    if ($Content[$i] -eq '*' -and $Content[$i + 1] -eq '/') {
                        $i += 2
                        break
                    }
                    $i++
                }
                continue
            }
        }
        
        [void]$result.Append($char)
        $i++
    }
    
    $cleanJson = $result.ToString()
    
    try {
        if ($PSVersionTable.PSVersion.Major -ge 6) {
            return $cleanJson | ConvertFrom-Json -AsHashtable
        } else {
            $obj = $cleanJson | ConvertFrom-Json
            return (ConvertTo-HashtableRecursive -InputObject $obj)
        }
    } catch {
        throw "Failed to parse JSONC: $($_.Exception.Message)"
    }
}

function Invoke-JsonMergeRestore {
    <#
    .SYNOPSIS
        Merge JSON source into target file.
    .DESCRIPTION
        Deep-merges source JSON into target, creating target if missing.
        Supports JSONC (JSON with comments) for source files.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Target,
        
        [Parameter(Mandatory = $false)]
        [bool]$Backup = $true,
        
        [Parameter(Mandatory = $false)]
        [ValidateSet("replace", "union")]
        [string]$ArrayStrategy = "replace",
        
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
        # Read source JSON/JSONC
        $sourceContent = Read-TextFileUtf8 -Path $expandedSource
        $sourceData = ConvertFrom-JsoncContent -Content $sourceContent
        
        # Read target JSON if exists, otherwise empty object
        $targetData = @{}
        if (Test-Path $expandedTarget) {
            $targetContent = Read-TextFileUtf8 -Path $expandedTarget
            if ($targetContent -and $targetContent.Trim()) {
                $targetData = ConvertFrom-JsoncContent -Content $targetContent
            }
        }
        
        # Perform deep merge
        $mergedData = Merge-JsonDeep -Target $targetData -Source $sourceData -ArrayStrategy $ArrayStrategy
        
        # Convert to deterministic JSON
        $mergedJson = ConvertTo-JsonSorted -Object $mergedData
        
        # Check if already up-to-date
        if (Test-Path $expandedTarget) {
            $existingContent = Read-TextFileUtf8 -Path $expandedTarget
            $existingData = $null
            try {
                $existingData = ConvertFrom-JsoncContent -Content $existingContent
            } catch {
                # Target exists but isn't valid JSON - will be overwritten
            }
            
            if ($null -ne $existingData) {
                $existingJson = ConvertTo-JsonSorted -Object $existingData
                if ($existingJson -eq $mergedJson) {
                    $result.Success = $true
                    $result.Skipped = $true
                    $result.Message = "already up to date"
                    return $result
                }
            }
        }
        
        # Dry-run mode
        if ($DryRun) {
            $result.Success = $true
            $result.Message = "Would merge JSON $expandedSource -> $expandedTarget"
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
        
        # Write merged JSON atomically
        Write-TextFileUtf8Atomic -Path $expandedTarget -Content $mergedJson
        
        $result.Success = $true
        $result.Message = "Merged JSON successfully"
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

# Functions exported: Invoke-JsonMergeRestore, Merge-JsonDeep, ConvertTo-JsonSorted
