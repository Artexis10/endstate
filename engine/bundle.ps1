# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Zip bundle packaging for capture profiles.

.DESCRIPTION
    Provides config module matching, config file collection, metadata generation,
    and atomic zip creation for capture bundles.
    
    A capture bundle is a self-contained zip containing:
    - manifest.jsonc (app list)
    - metadata.json (capture metadata)
    - configs/<module-id>/<files> (config payloads, optional)
#>

# Import dependencies
. "$PSScriptRoot\config-modules.ps1"

function Get-MatchedConfigModulesForApps {
    <#
    .SYNOPSIS
        Match captured apps against config module catalog by winget ID.
    .DESCRIPTION
        For each app in the captured manifest, checks if any config module
        matches via its matches.winget array. Returns modules that have
        capture sections defined.
    .PARAMETER Apps
        Array of app objects from capture (each has .refs.windows).
    .OUTPUTS
        Array of matched module objects from the catalog.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [AllowEmptyCollection()]
        [array]$Apps = @()
    )
    
    $catalog = Get-ConfigModuleCatalog
    
    if ($catalog.Count -eq 0 -or $Apps.Count -eq 0) {
        return @()
    }
    
    # Collect all winget IDs from captured apps
    $wingetIds = @($Apps | ForEach-Object {
        if ($_.refs -and $_.refs.windows) { $_.refs.windows }
    } | Where-Object { $_ })
    
    $matched = @()
    
    foreach ($moduleId in $catalog.Keys) {
        $module = $catalog[$moduleId]
        
        # Only consider modules with capture sections
        if (-not $module.capture -or -not $module.capture.files) {
            continue
        }
        
        # Check winget ID matches
        if ($module.matches -and $module.matches.winget) {
            $isMatch = $false
            foreach ($wingetPattern in $module.matches.winget) {
                foreach ($installedId in $wingetIds) {
                    if ($installedId -eq $wingetPattern -or $installedId -like $wingetPattern) {
                        $isMatch = $true
                        break
                    }
                }
                if ($isMatch) { break }
            }
            
            if ($isMatch) {
                $matched += $module
            }
        }
    }
    
    # Sort deterministically by module ID
    $matched = @($matched | Sort-Object -Property { $_.id })
    
    return $matched
}

function Invoke-CollectConfigFiles {
    <#
    .SYNOPSIS
        Collect config files from matched modules into a staging directory.
    .DESCRIPTION
        For each matched module, copies capture.files from system paths to
        the staging directory under configs/<module-id>/.
        Respects capture.excludeGlobs and sensitive.files exclusions.
    .PARAMETER Modules
        Array of matched config module objects (from Get-MatchedConfigModulesForApps).
    .PARAMETER StagingDir
        Path to the staging directory where configs/ will be created.
    .OUTPUTS
        Hashtable with:
        - included: array of module IDs successfully captured
        - skipped: array of module IDs skipped (no files found)
        - errors: array of error description strings
        - filesCopied: count of files copied
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [array]$Modules,
        
        [Parameter(Mandatory = $true)]
        [string]$StagingDir
    )
    
    $result = @{
        included = @()
        skipped = @()
        errors = @()
        filesCopied = 0
    }
    
    if ($Modules.Count -eq 0) {
        return $result
    }
    
    foreach ($module in $Modules) {
        $moduleId = $module.id
        # Strip "apps." prefix for directory name if present
        $moduleDirName = if ($moduleId -match '^apps\.(.+)$') { $Matches[1] } else { $moduleId }
        
        $excludeGlobs = if ($module.capture.excludeGlobs) { @($module.capture.excludeGlobs) } else { @() }
        
        # Build sensitive file list (expanded paths)
        $sensitiveFiles = @()
        if ($module.sensitive -and $module.sensitive.files) {
            $sensitiveFiles = @($module.sensitive.files | ForEach-Object {
                $expanded = Expand-ConfigPath -Path $_
                # Normalize path separators for comparison
                $expanded -replace '/', '\'
            })
        }
        
        $moduleFilesCopied = 0
        $moduleErrors = @()
        
        foreach ($fileEntry in $module.capture.files) {
            $sourcePath = Expand-ConfigPath -Path $fileEntry.source
            $isOptional = if ($fileEntry.ContainsKey('optional')) { $fileEntry.optional } else { $false }
            
            # Determine destination filename from dest field
            $destFileName = Split-Path -Leaf $fileEntry.dest
            # Build dest path under configs/<moduleDirName>/
            $destPath = Join-Path $StagingDir "configs\$moduleDirName\$destFileName"
            
            # Check if source matches exclude globs
            if (Test-PathMatchesExcludeGlobs -Path $sourcePath -ExcludeGlobs $excludeGlobs) {
                continue
            }
            
            # Check if source is a sensitive file
            $normalizedSource = $sourcePath -replace '/', '\'
            $isSensitive = $false
            foreach ($sf in $sensitiveFiles) {
                # Support wildcard patterns in sensitive files
                if ($normalizedSource -like $sf -or $normalizedSource -eq $sf) {
                    $isSensitive = $true
                    break
                }
            }
            if ($isSensitive) {
                continue
            }
            
            # Check if source exists
            if (-not (Test-Path $sourcePath)) {
                if (-not $isOptional) {
                    $moduleErrors += "Missing required file: $sourcePath (module: $moduleDirName)"
                }
                continue
            }
            
            # Ensure destination directory exists
            $destDir = Split-Path -Parent $destPath
            if ($destDir -and -not (Test-Path $destDir)) {
                New-Item -ItemType Directory -Path $destDir -Force | Out-Null
            }
            
            # Copy file or directory
            try {
                if (Test-Path $sourcePath -PathType Container) {
                    # Source is a directory — clean existing dest to prevent nesting
                    if (Test-Path $destPath) { Remove-Item $destPath -Recurse -Force }
                    Copy-Item -Path $sourcePath -Destination $destPath -Recurse -Force
                    # Count actual files copied (not the directory itself)
                    $copiedFiles = @(Get-ChildItem -Path $destPath -Recurse -File -ErrorAction SilentlyContinue)
                    $moduleFilesCopied += $copiedFiles.Count
                } else {
                    Copy-Item -Path $sourcePath -Destination $destPath -Force
                    $moduleFilesCopied++
                }
            } catch {
                $moduleErrors += "Failed to copy $sourcePath`: $($_.Exception.Message)"
            }
        }
        
        if ($moduleFilesCopied -gt 0) {
            $result.included += $moduleDirName
            $result.filesCopied += $moduleFilesCopied
        } else {
            $result.skipped += $moduleDirName
        }
        
        if ($moduleErrors.Count -gt 0) {
            $result.errors += $moduleErrors
        }
    }
    
    return $result
}

function New-CaptureMetadata {
    <#
    .SYNOPSIS
        Generate metadata.json content for a capture bundle.
    .PARAMETER ConfigsIncluded
        Array of module IDs that were successfully captured.
    .PARAMETER ConfigsSkipped
        Array of module IDs that were skipped.
    .PARAMETER CaptureWarnings
        Array of warning strings from the capture process.
    .OUTPUTS
        Hashtable representing the metadata.json content.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string[]]$ConfigsIncluded = @(),
        
        [Parameter(Mandatory = $false)]
        [string[]]$ConfigsSkipped = @(),
        
        [Parameter(Mandatory = $false)]
        [string[]]$CaptureWarnings = @()
    )
    
    # Read version from VERSION.txt if available
    $endstateVersion = "0.1.0"
    if ($PSScriptRoot) {
        $versionFile = Join-Path $PSScriptRoot "..\VERSION.txt"
        if (Test-Path $versionFile) {
            $endstateVersion = (Get-Content $versionFile -Raw).Trim()
        }
    }
    
    return [ordered]@{
        schemaVersion = "1.0"
        capturedAt = (Get-Date).ToUniversalTime().ToString("o")
        machineName = $env:COMPUTERNAME
        endstateVersion = $endstateVersion
        configModulesIncluded = @($ConfigsIncluded)
        configModulesSkipped = @($ConfigsSkipped)
        captureWarnings = @($CaptureWarnings)
    }
}

function New-CaptureBundle {
    <#
    .SYNOPSIS
        Create a zip bundle from a capture result.
    .DESCRIPTION
        Stages manifest, config payloads, and metadata in a temp directory,
        then creates a zip atomically (write to temp, move to final location).
    .PARAMETER ManifestPath
        Path to the generated manifest.jsonc file.
    .PARAMETER OutputZipPath
        Final output path for the zip file.
    .PARAMETER Apps
        Array of captured app objects (for config module matching).
    .PARAMETER CaptureWarnings
        Array of warning strings from the capture process.
    .OUTPUTS
        Hashtable with:
        - Success: boolean
        - OutputPath: path to the zip file
        - ConfigsIncluded: array of module IDs bundled
        - ConfigsSkipped: array of module IDs skipped
        - ConfigsCaptureErrors: array of error strings
        - Metadata: the metadata hashtable
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $true)]
        [string]$OutputZipPath,
        
        [Parameter(Mandatory = $false)]
        [AllowEmptyCollection()]
        [array]$Apps = @(),
        
        [Parameter(Mandatory = $false)]
        [string[]]$CaptureWarnings = @()
    )
    
    $result = @{
        Success = $false
        OutputPath = $OutputZipPath
        ConfigsIncluded = @()
        ConfigsSkipped = @()
        ConfigsCaptureErrors = @()
        Metadata = $null
    }
    
    # Create staging directory
    $stagingDir = Join-Path $env:TEMP "endstate-bundle-$([guid]::NewGuid().ToString('N').Substring(0,8))"
    
    try {
        New-Item -ItemType Directory -Path $stagingDir -Force | Out-Null
        
        # Stage 1: Copy manifest
        $stagedManifest = Join-Path $stagingDir "manifest.jsonc"
        Copy-Item -Path $ManifestPath -Destination $stagedManifest -Force
        
        # Stage 2: Match and collect config files
        $matchedModules = @(Get-MatchedConfigModulesForApps -Apps $Apps)
        if ($matchedModules.Count -eq 0) {
            $configResult = @{ included = @(); skipped = @(); errors = @(); filesCopied = 0 }
        } else {
            $configResult = Invoke-CollectConfigFiles -Modules $matchedModules -StagingDir $stagingDir
        }
        
        $result.ConfigsIncluded = @($configResult.included)
        $result.ConfigsSkipped = @($configResult.skipped)
        $result.ConfigsCaptureErrors = @($configResult.errors)
        
        # Stage 2b: Inject restore entries from included modules into staged manifest
        if ($configResult.included.Count -gt 0) {
            $includedSet = @{}
            foreach ($inc in $configResult.included) { $includedSet[$inc] = $true }
            
            $restoreEntries = @()
            foreach ($module in $matchedModules) {
                $moduleDirName = if ($module.id -match '^apps\.(.+)$') { $Matches[1] } else { $module.id }
                if (-not $includedSet.ContainsKey($moduleDirName)) { continue }
                if (-not $module.restore -or $module.restore.Count -eq 0) { continue }
                
                foreach ($entry in $module.restore) {
                    $clone = @{}
                    foreach ($key in $entry.Keys) { $clone[$key] = $entry[$key] }
                    $leaf = ($clone.source -replace '\\', '/').Split('/')[-1]
                    $clone.source = "./configs/$moduleDirName/$leaf"
                    $restoreEntries += $clone
                }
            }
            
            if ($restoreEntries.Count -gt 0) {
                $manifestData = Read-JsoncFile -Path $stagedManifest
                $manifestData.restore = $restoreEntries
                $manifestData | ConvertTo-Json -Depth 10 | Set-Content -Path $stagedManifest -Encoding UTF8 -NoNewline
            }
        }
        
        # Stage 3: Generate metadata
        $allWarnings = @($CaptureWarnings) + @($configResult.errors)
        $metadata = New-CaptureMetadata `
            -ConfigsIncluded $configResult.included `
            -ConfigsSkipped $configResult.skipped `
            -CaptureWarnings $allWarnings
        
        $result.Metadata = $metadata
        
        $metadataPath = Join-Path $stagingDir "metadata.json"
        $metadata | ConvertTo-Json -Depth 10 | Set-Content -Path $metadataPath -Encoding UTF8 -NoNewline
        
        # Stage 4: Create zip atomically
        $outDir = Split-Path -Parent $OutputZipPath
        if ($outDir -and -not (Test-Path $outDir)) {
            New-Item -ItemType Directory -Path $outDir -Force | Out-Null
        }
        
        # Write to temp zip first, then move
        $tempZip = "$OutputZipPath.tmp"
        if (Test-Path $tempZip) { Remove-Item $tempZip -Force }
        if (Test-Path $OutputZipPath) { Remove-Item $OutputZipPath -Force }
        
        # Use .NET ZipFile for reliable zip creation
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [System.IO.Compression.ZipFile]::CreateFromDirectory(
            $stagingDir,
            $tempZip,
            [System.IO.Compression.CompressionLevel]::Optimal,
            $false  # Don't include base directory name
        )
        
        # Atomic move
        Move-Item -Path $tempZip -Destination $OutputZipPath -Force
        
        $result.Success = $true
        
    } catch {
        $result.ConfigsCaptureErrors += "Bundle creation failed: $($_.Exception.Message)"
        # If manifest exists, the capture itself succeeded even if bundling failed
    } finally {
        # Cleanup staging directory
        if (Test-Path $stagingDir) {
            Remove-Item -Path $stagingDir -Recurse -Force -ErrorAction SilentlyContinue
        }
        # Cleanup temp zip if it exists
        $tempZip = "$OutputZipPath.tmp"
        if (Test-Path $tempZip) {
            Remove-Item -Path $tempZip -Force -ErrorAction SilentlyContinue
        }
    }
    
    return $result
}

function Expand-ProfileBundle {
    <#
    .SYNOPSIS
        Extract a zip profile bundle to a temporary directory.
    .DESCRIPTION
        Extracts the zip to a temp directory and returns the path.
        Caller is responsible for cleanup via Remove-ProfileBundleTemp.
    .PARAMETER ZipPath
        Path to the zip bundle.
    .OUTPUTS
        Hashtable with:
        - Success: boolean
        - ExtractedDir: path to extracted directory
        - ManifestPath: path to manifest.jsonc within extracted dir
        - HasConfigs: boolean indicating if configs/ directory exists
        - Metadata: parsed metadata.json (or $null)
        - Error: error message if failed
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ZipPath
    )
    
    $result = @{
        Success = $false
        ExtractedDir = $null
        ManifestPath = $null
        HasConfigs = $false
        Metadata = $null
        Error = $null
    }
    
    if (-not (Test-Path $ZipPath)) {
        $result.Error = "Zip file not found: $ZipPath"
        return $result
    }
    
    $extractDir = Join-Path $env:TEMP "endstate-apply-$([guid]::NewGuid().ToString('N').Substring(0,8))"
    
    try {
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [System.IO.Compression.ZipFile]::ExtractToDirectory($ZipPath, $extractDir)
        
        $result.ExtractedDir = $extractDir
        
        # Check for manifest
        $manifestPath = Join-Path $extractDir "manifest.jsonc"
        if (-not (Test-Path $manifestPath)) {
            $result.Error = "Zip does not contain manifest.jsonc"
            return $result
        }
        $result.ManifestPath = $manifestPath
        
        # Check for configs
        $configsDir = Join-Path $extractDir "configs"
        $result.HasConfigs = Test-Path $configsDir
        
        # Parse metadata if present
        $metadataPath = Join-Path $extractDir "metadata.json"
        if (Test-Path $metadataPath) {
            try {
                $metadataContent = Get-Content -Path $metadataPath -Raw -Encoding UTF8
                $result.Metadata = $metadataContent | ConvertFrom-Json
            } catch {
                # Metadata parse failure is non-fatal
            }
        }
        
        $result.Success = $true
        
    } catch {
        $result.Error = "Failed to extract zip: $($_.Exception.Message)"
        # Cleanup on failure
        if (Test-Path $extractDir) {
            Remove-Item -Path $extractDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    return $result
}

function Remove-ProfileBundleTemp {
    <#
    .SYNOPSIS
        Clean up a temporary extracted profile bundle directory.
    .PARAMETER ExtractedDir
        Path to the extracted directory to remove.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ExtractedDir
    )
    
    if ($ExtractedDir -and (Test-Path $ExtractedDir)) {
        Remove-Item -Path $ExtractedDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Resolve-ProfilePath {
    <#
    .SYNOPSIS
        Resolve a profile name to a path using three-format discovery.
    .DESCRIPTION
        Checks for profiles in order: zip → folder → bare manifest.
        First match wins.
    .PARAMETER ProfileName
        The profile name to resolve (without extension).
    .PARAMETER ProfilesDir
        The profiles directory to search in.
    .OUTPUTS
        Hashtable with:
        - Found: boolean
        - Path: resolved path
        - Format: "zip", "folder", or "bare"
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProfileName,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfilesDir
    )
    
    $result = @{
        Found = $false
        Path = $null
        Format = $null
    }
    
    # Check 1: Zip bundle
    $zipPath = Join-Path $ProfilesDir "$ProfileName.zip"
    if (Test-Path $zipPath) {
        $result.Found = $true
        $result.Path = $zipPath
        $result.Format = "zip"
        return $result
    }
    
    # Check 2: Loose folder
    $folderManifest = Join-Path $ProfilesDir "$ProfileName\manifest.jsonc"
    if (Test-Path $folderManifest) {
        $result.Found = $true
        $result.Path = $folderManifest
        $result.Format = "folder"
        return $result
    }
    
    # Check 3: Bare manifest
    $barePath = Join-Path $ProfilesDir "$ProfileName.jsonc"
    if (Test-Path $barePath) {
        $result.Found = $true
        $result.Path = $barePath
        $result.Format = "bare"
        return $result
    }
    
    return $result
}

# Functions exported: Get-MatchedConfigModulesForApps, Invoke-CollectConfigFiles, New-CaptureMetadata, New-CaptureBundle, Expand-ProfileBundle, Remove-ProfileBundleTemp, Resolve-ProfilePath
