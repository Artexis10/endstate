# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Copy restorer for Provisioning.

.DESCRIPTION
    Restores configuration files by copying from source to target,
    with backup-before-overwrite safety and up-to-date detection.
#>

# Known sensitive path segments that trigger warnings
$script:SensitivePathSegments = @(
    '.ssh', '.aws', '.azure', '.gnupg', '.gpg',
    'credentials', 'secrets', 'tokens',
    '.kube', '.docker', 'id_rsa', 'id_ed25519', 'id_ecdsa'
)

function Test-SharingViolation {
    <#
    .SYNOPSIS
        Check if an exception is a sharing violation (file locked by another process).
    .DESCRIPTION
        Checks HRESULT codes for sharing violations:
        - 0x80070020 (ERROR_SHARING_VIOLATION) - file is being used by another process
        - 0x80070021 (ERROR_LOCK_VIOLATION) - file is locked
    #>
    param([System.Exception]$Exception)
    
    # Check HResult for sharing violation codes
    $hresult = $Exception.HResult
    if ($hresult -eq 0x80070020 -or $hresult -eq 0x80070021) {
        return $true
    }
    
    # Also check inner exception
    if ($Exception.InnerException) {
        $innerHResult = $Exception.InnerException.HResult
        if ($innerHResult -eq 0x80070020 -or $innerHResult -eq 0x80070021) {
            return $true
        }
    }
    
    return $false
}

function Copy-ItemWithLockedFileHandling {
    <#
    .SYNOPSIS
        Copy a file or directory, skipping locked files with warnings.
    .DESCRIPTION
        Copies files/directories recursively. When a file is locked:
        - Adds a warning to the result
        - Continues copying other files
        - Does NOT fail the entire operation
    .OUTPUTS
        Hashtable with Success, SkippedFiles, Warnings
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Destination,
        
        [Parameter(Mandatory = $false)]
        [string[]]$ExcludePatterns = @()
    )
    
    $result = @{
        Success = $true
        SkippedFiles = @()
        Warnings = @()
        CopiedCount = 0
    }
    
    $sourceItem = Get-Item $Source
    
    if ($sourceItem.PSIsContainer) {
        # Directory copy with locked file handling
        if (-not (Test-Path $Destination)) {
            New-Item -ItemType Directory -Path $Destination -Force | Out-Null
        }
        
        $sourceFiles = Get-ChildItem -Path $Source -Recurse -Force
        
        foreach ($item in $sourceFiles) {
            $relativePath = $item.FullName.Substring($Source.Length).TrimStart('\', '/')
            $destPath = Join-Path $Destination $relativePath
            
            # Check exclude patterns
            $excluded = $false
            foreach ($pattern in $ExcludePatterns) {
                if ($relativePath -like $pattern -or $item.FullName -like $pattern) {
                    $excluded = $true
                    break
                }
            }
            
            if ($excluded) {
                continue
            }
            
            if ($item.PSIsContainer) {
                if (-not (Test-Path $destPath)) {
                    New-Item -ItemType Directory -Path $destPath -Force | Out-Null
                }
            } else {
                # File - try to copy, handle locked files
                $destDir = Split-Path -Parent $destPath
                if (-not (Test-Path $destDir)) {
                    New-Item -ItemType Directory -Path $destDir -Force | Out-Null
                }
                
                try {
                    Copy-Item -Path $item.FullName -Destination $destPath -Force -ErrorAction Stop
                    $result.CopiedCount++
                } catch {
                    # Only skip for sharing violations (file locked by another process)
                    # Do NOT skip for generic access denied - those are real errors
                    if (Test-SharingViolation -Exception $_.Exception) {
                        $result.SkippedFiles += $relativePath
                        $result.Warnings += "WARN: Skipped locked file (sharing violation): $relativePath"
                    } else {
                        # Re-throw for non-sharing-violation errors - these are real failures
                        throw
                    }
                }
            }
        }
    } else {
        # Single file copy
        try {
            $destDir = Split-Path -Parent $Destination
            if ($destDir -and -not (Test-Path $destDir)) {
                New-Item -ItemType Directory -Path $destDir -Force | Out-Null
            }
            Copy-Item -Path $Source -Destination $Destination -Force -ErrorAction Stop
            $result.CopiedCount++
        } catch {
            # Only skip for sharing violations (file locked by another process)
            # Do NOT skip for generic access denied - those are real errors
            if (Test-SharingViolation -Exception $_.Exception) {
                $result.SkippedFiles += $Source
                $result.Warnings += "WARN: Skipped locked file (sharing violation): $Source"
            } else {
                $result.Success = $false
                $result.Warnings += "ERROR: Failed to copy: $($_.Exception.Message)"
            }
        }
    }
    
    return $result
}

function Test-RestoreSensitivePath {
    <#
    .SYNOPSIS
        Check if a path contains sensitive segments.
    #>
    param([string]$Path)
    
    $normalizedPath = $Path.ToLower() -replace '\\', '/'
    foreach ($segment in $script:SensitivePathSegments) {
        if ($normalizedPath -match [regex]::Escape($segment.ToLower())) {
            return $true
        }
    }
    return $false
}

function Test-RestoreUpToDate {
    <#
    .SYNOPSIS
        Check if target matches source (up-to-date detection).
    .DESCRIPTION
        For files: compares size and last write time.
        For directories: shallow comparison (file count + newest mtime).
    #>
    param(
        [string]$Source,
        [string]$Target
    )
    
    if (-not (Test-Path $Target)) {
        return $false
    }
    
    $sourceItem = Get-Item $Source
    $targetItem = Get-Item $Target
    
    if ($sourceItem.PSIsContainer -ne $targetItem.PSIsContainer) {
        return $false
    }
    
    if ($sourceItem.PSIsContainer) {
        # Directory: shallow comparison
        $sourceFiles = @(Get-ChildItem -Path $Source -Recurse -File)
        $targetFiles = @(Get-ChildItem -Path $Target -Recurse -File)
        
        if ($sourceFiles.Count -ne $targetFiles.Count) {
            return $false
        }
        
        if ($sourceFiles.Count -eq 0) {
            return $true
        }
        
        $sourceNewest = ($sourceFiles | Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime
        $targetNewest = ($targetFiles | Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime
        
        return [Math]::Abs(($sourceNewest - $targetNewest).TotalSeconds) -lt 2
    } else {
        # File: size + mtime comparison
        if ($sourceItem.Length -ne $targetItem.Length) {
            return $false
        }
        return [Math]::Abs(($sourceItem.LastWriteTime - $targetItem.LastWriteTime).TotalSeconds) -lt 2
    }
}

function Invoke-CopyRestore {
    <#
    .SYNOPSIS
        Copy a file or directory to target, backing up existing content.
    .DESCRIPTION
        Supports up-to-date detection, backup-first safety, and sensitive path warnings.
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
    
    # Expand environment variables in paths
    $expandedSource = [Environment]::ExpandEnvironmentVariables($Source)
    $expandedTarget = [Environment]::ExpandEnvironmentVariables($Target)
    
    # Handle ~ for home directory (cross-platform)
    $homeDir = if ($env:HOME) { $env:HOME } else { $env:USERPROFILE }
    if ($expandedSource.StartsWith("~")) {
        $expandedSource = $expandedSource -replace "^~", $homeDir
    }
    if ($expandedTarget.StartsWith("~")) {
        $expandedTarget = $expandedTarget -replace "^~", $homeDir
    }
    
    # Handle relative paths
    if ($ManifestDir -and ($expandedSource.StartsWith("./") -or $expandedSource.StartsWith("../"))) {
        $expandedSource = Join-Path $ManifestDir $expandedSource
        $expandedSource = [System.IO.Path]::GetFullPath($expandedSource)
    }
    
    # Check for sensitive paths and add warnings
    if (Test-RestoreSensitivePath -Path $expandedSource) {
        $result.Warnings += "Source path contains sensitive segment: $expandedSource"
    }
    if (Test-RestoreSensitivePath -Path $expandedTarget) {
        $result.Warnings += "Target path contains sensitive segment: $expandedTarget"
    }
    
    # Check source exists
    if (-not (Test-Path $expandedSource)) {
        $result.Error = "Source not found: $expandedSource"
        return $result
    }
    
    # Check if up-to-date
    if (Test-RestoreUpToDate -Source $expandedSource -Target $expandedTarget) {
        $result.Success = $true
        $result.Skipped = $true
        $result.Message = "already up to date"
        return $result
    }
    
    # Dry-run mode
    if ($DryRun) {
        $result.Success = $true
        $result.Message = "Would copy $expandedSource -> $expandedTarget"
        return $result
    }
    
    try {
        # Backup existing target if it exists
        if ($Backup -and (Test-Path $expandedTarget)) {
            $backupRunId = if ($RunId) { $RunId } else { Get-Date -Format 'yyyyMMdd-HHmmss' }
            $backupRoot = Join-Path $PSScriptRoot "..\state\backups\$backupRunId"
            
            # Preserve path structure in backup
            $normalizedTarget = $expandedTarget -replace ':', ''
            $normalizedTarget = $normalizedTarget -replace '^[/\\]+', ''
            $backupPath = Join-Path $backupRoot $normalizedTarget
            $backupDir = Split-Path -Parent $backupPath
            
            if (-not (Test-Path $backupDir)) {
                New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
            }
            
            if (Test-Path $expandedTarget -PathType Container) {
                Copy-Item -Path $expandedTarget -Destination $backupPath -Recurse -Force
            } else {
                Copy-Item -Path $expandedTarget -Destination $backupPath -Force
            }
            
            $result.BackupPath = $backupPath
        }
        
        # Ensure target directory exists
        $targetDir = Split-Path -Parent $expandedTarget
        if ($targetDir -and -not (Test-Path $targetDir)) {
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
        }
        
        # Copy source to target using locked file handling
        if (Test-Path $expandedSource -PathType Container) {
            # For directories, use locked-file-safe copy
            # Don't remove existing - let the copy function handle overwrites
            $copyResult = Copy-ItemWithLockedFileHandling -Source $expandedSource -Destination $expandedTarget -ExcludePatterns @()
            
            $result.Warnings += $copyResult.Warnings
            
            if ($copyResult.SkippedFiles.Count -gt 0) {
                $result.Message = "Restored with $($copyResult.SkippedFiles.Count) skipped locked file(s)"
            } else {
                $result.Message = "Restored successfully"
            }
            $result.Success = $copyResult.Success
        } else {
            # Single file copy with locked file handling
            $copyResult = Copy-ItemWithLockedFileHandling -Source $expandedSource -Destination $expandedTarget
            
            $result.Warnings += $copyResult.Warnings
            
            if ($copyResult.SkippedFiles.Count -gt 0) {
                $result.Message = "Skipped locked file"
                $result.Success = $true  # Still success - we handled it gracefully
            } else {
                $result.Message = "Restored successfully"
                $result.Success = $copyResult.Success
            }
        }
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

function Test-CopyRestorePrerequisites {
    <#
    .SYNOPSIS
        Check if a copy restore can be performed.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Target
    )
    
    $result = @{
        CanRestore = $false
        SourceExists = $false
        TargetExists = $false
        TargetWritable = $false
        Issues = @()
    }
    
    # Expand paths
    $expandedSource = [Environment]::ExpandEnvironmentVariables($Source)
    $expandedTarget = [Environment]::ExpandEnvironmentVariables($Target)
    
    if ($expandedSource.StartsWith("~")) {
        $expandedSource = $expandedSource -replace "^~", $env:USERPROFILE
    }
    if ($expandedTarget.StartsWith("~")) {
        $expandedTarget = $expandedTarget -replace "^~", $env:USERPROFILE
    }
    
    # Check source
    if (Test-Path $expandedSource) {
        $result.SourceExists = $true
    } else {
        $result.Issues += "Source does not exist: $expandedSource"
    }
    
    # Check target
    if (Test-Path $expandedTarget) {
        $result.TargetExists = $true
    }
    
    # Check target directory is writable
    $targetDir = Split-Path -Parent $expandedTarget
    if (Test-Path $targetDir) {
        try {
            $testFile = Join-Path $targetDir ".provisioning-write-test"
            [System.IO.File]::WriteAllText($testFile, "test")
            Remove-Item $testFile -Force
            $result.TargetWritable = $true
        } catch {
            $result.Issues += "Target directory not writable: $targetDir"
        }
    } else {
        # Directory doesn't exist, check if we can create it
        try {
            $parentDir = Split-Path -Parent $targetDir
            if (Test-Path $parentDir) {
                $result.TargetWritable = $true
            } else {
                $result.Issues += "Cannot create target directory: $targetDir"
            }
        } catch {
            $result.Issues += "Cannot determine target writability"
        }
    }
    
    $result.CanRestore = $result.SourceExists -and $result.TargetWritable
    
    return $result
}

# Functions exported: Invoke-CopyRestore, Test-CopyRestorePrerequisites
