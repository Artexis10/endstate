# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Config Modules catalog and expansion for Provisioning.

.DESCRIPTION
    Provides config module loading, validation, and manifest expansion.
    Config modules define reusable restore/verify configurations for applications.
    
    Module files are located in provisioning/modules/apps/<app>/module.jsonc
    and can be referenced by manifests via the configModules array.
#>

# Import dependencies
. "$PSScriptRoot\manifest.ps1"

# Capture script root at load time so functions resolve paths correctly
# even when dot-sourced from a different directory (e.g. bin/endstate.ps1)
$script:ConfigModulesRoot = $PSScriptRoot

# Module catalog cache (populated on first load)
$script:ConfigModuleCatalog = $null
$script:ConfigModuleCatalogLoaded = $false

function Get-ConfigModuleCatalog {
    <#
    .SYNOPSIS
        Load all config modules from the modules directory.
    .DESCRIPTION
        Scans provisioning/modules/apps/ for module.jsonc files,
        parses them, validates required fields, and returns a dictionary keyed by module id.
    .PARAMETER Force
        Force reload even if catalog is already cached.
    .OUTPUTS
        Hashtable keyed by module id, values are module definitions.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [switch]$Force
    )
    
    # Return cached catalog if available
    if ($script:ConfigModuleCatalogLoaded -and -not $Force) {
        return $script:ConfigModuleCatalog
    }
    
    $catalog = @{}
    $modulesRoot = Join-Path $script:ConfigModulesRoot "..\modules\apps"
    
    # If modules directory doesn't exist, return empty catalog
    if (-not (Test-Path $modulesRoot)) {
        $script:ConfigModuleCatalog = $catalog
        $script:ConfigModuleCatalogLoaded = $true
        return $catalog
    }
    
    # Find all module.jsonc files recursively
    $moduleFiles = Get-ChildItem -Path $modulesRoot -Filter "module.jsonc" -Recurse -File -ErrorAction SilentlyContinue
    
    foreach ($moduleFile in $moduleFiles) {
        try {
            $module = Read-JsoncFile -Path $moduleFile.FullName
            
            # Validate required fields
            $validationResult = Test-ConfigModuleSchema -Module $module -FilePath $moduleFile.FullName
            if (-not $validationResult.Valid) {
                Write-Warning "Invalid config module at $($moduleFile.FullName): $($validationResult.Error)"
                continue
            }
            
            # Check for duplicate IDs
            if ($catalog.ContainsKey($module.id)) {
                Write-Warning "Duplicate config module id '$($module.id)' found at $($moduleFile.FullName). Skipping."
                continue
            }
            
            # Store module with metadata
            $module._filePath = $moduleFile.FullName
            $module._moduleDir = Split-Path -Parent $moduleFile.FullName
            $catalog[$module.id] = $module
            
        } catch {
            Write-Warning "Failed to load config module at $($moduleFile.FullName): $($_.Exception.Message)"
        }
    }
    
    # Cache the catalog
    $script:ConfigModuleCatalog = $catalog
    $script:ConfigModuleCatalogLoaded = $true
    
    return $catalog
}

function Test-ConfigModuleSchema {
    <#
    .SYNOPSIS
        Validate a config module against the required schema.
    .PARAMETER Module
        The parsed module hashtable.
    .PARAMETER FilePath
        Path to the module file (for error messages).
    .OUTPUTS
        Hashtable with Valid (bool) and Error (string) properties.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Module,
        
        [Parameter(Mandatory = $false)]
        [string]$FilePath = "unknown"
    )
    
    $result = @{ Valid = $true; Error = $null }
    
    # Required: id (string, non-empty)
    if (-not $Module.id -or $Module.id -isnot [string] -or $Module.id.Trim() -eq "") {
        $result.Valid = $false
        $result.Error = "Missing or invalid 'id' field (must be non-empty string)"
        return $result
    }
    
    # Required: displayName (string, non-empty)
    if (-not $Module.displayName -or $Module.displayName -isnot [string] -or $Module.displayName.Trim() -eq "") {
        $result.Valid = $false
        $result.Error = "Missing or invalid 'displayName' field (must be non-empty string)"
        return $result
    }
    
    # Required: matches (object with at least one matcher)
    if (-not $Module.matches -or $Module.matches -isnot [hashtable]) {
        $result.Valid = $false
        $result.Error = "Missing or invalid 'matches' field (must be object)"
        return $result
    }
    
    # matches must have at least one of: winget, exe, uninstallDisplayName
    $hasWinget = $Module.matches.winget -and $Module.matches.winget.Count -gt 0
    $hasExe = $Module.matches.exe -and $Module.matches.exe.Count -gt 0
    $hasUninstall = $Module.matches.uninstallDisplayName -and $Module.matches.uninstallDisplayName.Count -gt 0
    
    if (-not ($hasWinget -or $hasExe -or $hasUninstall)) {
        $result.Valid = $false
        $result.Error = "matches must have at least one of: winget, exe, uninstallDisplayName"
        return $result
    }
    
    # Optional: restore (array)
    if ($Module.ContainsKey('restore') -and $null -ne $Module.restore) {
        if ($Module.restore -isnot [array]) {
            $result.Valid = $false
            $result.Error = "'restore' must be an array"
            return $result
        }
    }
    
    # Optional: verify (array)
    if ($Module.ContainsKey('verify') -and $null -ne $Module.verify) {
        if ($Module.verify -isnot [array]) {
            $result.Valid = $false
            $result.Error = "'verify' must be an array"
            return $result
        }
    }
    
    # Optional: sensitivity (enum)
    if ($Module.ContainsKey('sensitivity') -and $null -ne $Module.sensitivity) {
        $validSensitivities = @('low', 'medium', 'high', 'sensitive', 'machineBound')
        if ($Module.sensitivity -notin $validSensitivities) {
            $result.Valid = $false
            $result.Error = "'sensitivity' must be one of: $($validSensitivities -join ', ')"
            return $result
        }
    }
    
    # Optional: capture (object with files array)
    if ($Module.ContainsKey('capture') -and $null -ne $Module.capture) {
        if ($Module.capture -isnot [hashtable]) {
            $result.Valid = $false
            $result.Error = "'capture' must be an object"
            return $result
        }
        
        # capture.files is required if capture is present (empty array is valid for install-only modules)
        if (-not $Module.capture.ContainsKey('files') -or $Module.capture.files -isnot [array]) {
            $result.Valid = $false
            $result.Error = "'capture.files' must be an array when capture is defined"
            return $result
        }
        
        # Validate each file entry
        foreach ($fileEntry in $Module.capture.files) {
            if ($fileEntry -isnot [hashtable]) {
                $result.Valid = $false
                $result.Error = "Each entry in 'capture.files' must be an object"
                return $result
            }
            if (-not $fileEntry.source -or $fileEntry.source -isnot [string]) {
                $result.Valid = $false
                $result.Error = "Each 'capture.files' entry must have a 'source' string"
                return $result
            }
            if (-not $fileEntry.dest -or $fileEntry.dest -isnot [string]) {
                $result.Valid = $false
                $result.Error = "Each 'capture.files' entry must have a 'dest' string"
                return $result
            }
        }
        
        # Optional: excludeGlobs (array of strings)
        if ($Module.capture.ContainsKey('excludeGlobs') -and $null -ne $Module.capture.excludeGlobs) {
            if ($Module.capture.excludeGlobs -isnot [array]) {
                $result.Valid = $false
                $result.Error = "'capture.excludeGlobs' must be an array"
                return $result
            }
        }
    }
    
    return $result
}

function Expand-ManifestConfigModules {
    <#
    .SYNOPSIS
        Expand configModules references into restore/verify items.
    .DESCRIPTION
        For each module id in manifest.configModules:
        - Look up the module in the catalog
        - Append module.restore items to manifest.restore
        - Append module.verify items to manifest.verify
        
        Called after includes are resolved, before apply/verify executes.
    .PARAMETER Manifest
        The manifest hashtable (already includes-resolved).
    .PARAMETER Catalog
        Optional: pre-loaded catalog. If not provided, loads via Get-ConfigModuleCatalog.
    .OUTPUTS
        The manifest with expanded restore/verify arrays.
    .THROWS
        If any module id is not found in the catalog.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest,
        
        [Parameter(Mandatory = $false)]
        [hashtable]$Catalog = $null
    )
    
    # If no configModules, return manifest unchanged
    if (-not $Manifest.configModules -or $Manifest.configModules.Count -eq 0) {
        return $Manifest
    }
    
    # Load catalog if not provided
    if (-not $Catalog) {
        $Catalog = Get-ConfigModuleCatalog
    }
    
    # Track unknown modules for error reporting
    $unknownModules = @()
    $expandedRestore = @()
    $expandedVerify = @()
    
    # Build excludeConfigs lookup for fast skip
    $excludeSet = @{}
    if ($Manifest.excludeConfigs -and $Manifest.excludeConfigs.Count -gt 0) {
        foreach ($ex in $Manifest.excludeConfigs) { $excludeSet[$ex] = $true }
    }

    foreach ($moduleId in $Manifest.configModules) {
        # Skip modules suppressed by excludeConfigs
        if ($excludeSet.ContainsKey($moduleId)) {
            continue
        }

        if (-not $Catalog.ContainsKey($moduleId)) {
            $unknownModules += $moduleId
            continue
        }
        
        $module = $Catalog[$moduleId]
        
        # Append restore items (with source path resolution relative to module dir)
        if ($module.restore -and $module.restore.Count -gt 0) {
            foreach ($restoreItem in $module.restore) {
                # Clone the item to avoid modifying the cached module
                $expandedItem = @{}
                foreach ($key in $restoreItem.Keys) {
                    $expandedItem[$key] = $restoreItem[$key]
                }
                
                # Mark the source module for traceability
                $expandedItem._fromModule = $moduleId
                
                # Resolve relative source paths against module directory
                if ($expandedItem.source -and $expandedItem.source.StartsWith("./")) {
                    $expandedItem.source = Join-Path $module._moduleDir $expandedItem.source.Substring(2)
                }
                
                $expandedRestore += $expandedItem
            }
        }
        
        # Append verify items
        if ($module.verify -and $module.verify.Count -gt 0) {
            foreach ($verifyItem in $module.verify) {
                # Clone the item
                $expandedItem = @{}
                foreach ($key in $verifyItem.Keys) {
                    $expandedItem[$key] = $verifyItem[$key]
                }
                
                # Mark the source module for traceability
                $expandedItem._fromModule = $moduleId
                
                $expandedVerify += $expandedItem
            }
        }
    }
    
    # Fail if any modules were not found
    if ($unknownModules.Count -gt 0) {
        $availableIds = @($Catalog.Keys | Sort-Object)
        $availableList = if ($availableIds.Count -gt 0) { $availableIds -join ", " } else { "(none)" }
        throw "Unknown config module(s): $($unknownModules -join ', '). Available modules: $availableList"
    }
    
    # Append expanded items to manifest (after existing items)
    if (-not $Manifest.restore) { $Manifest.restore = @() }
    if (-not $Manifest.verify) { $Manifest.verify = @() }
    
    $Manifest.restore = @($Manifest.restore) + $expandedRestore
    $Manifest.verify = @($Manifest.verify) + $expandedVerify
    
    # Remove configModules from manifest (already expanded)
    # Keep it for reference but mark as expanded
    $Manifest._configModulesExpanded = $Manifest.configModules
    
    return $Manifest
}

function Get-ConfigModulesForInstalledApps {
    <#
    .SYNOPSIS
        Find config modules that match installed applications.
    .DESCRIPTION
        For discovery output: maps installed apps to available config modules
        based on winget IDs, exe names, and uninstall display names.
    .PARAMETER WingetInstalledIds
        Array of winget package IDs currently installed.
    .PARAMETER DiscoveredItems
        Array of discovery entries (from Invoke-Discovery).
    .OUTPUTS
        Array of matched modules with match details.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string[]]$WingetInstalledIds = @(),
        
        [Parameter(Mandatory = $false)]
        [array]$DiscoveredItems = @()
    )
    
    $catalog = Get-ConfigModuleCatalog
    $moduleMatches = @()
    
    foreach ($moduleId in $catalog.Keys) {
        $module = $catalog[$moduleId]
        $matchReasons = @()
        
        # Check winget ID matches
        if ($module.matches.winget) {
            foreach ($wingetPattern in $module.matches.winget) {
                foreach ($installedId in $WingetInstalledIds) {
                    if ($installedId -eq $wingetPattern -or $installedId -like $wingetPattern) {
                        $matchReasons += "winget:$installedId"
                    }
                }
            }
        }
        
        # Check exe matches (from discovered items with method = "path")
        if ($module.matches.exe) {
            foreach ($exePattern in $module.matches.exe) {
                foreach ($discovery in $DiscoveredItems) {
                    if ($discovery.method -eq "path" -and $discovery.name) {
                        $exeName = "$($discovery.name).exe"
                        if ($exeName -eq $exePattern -or $exeName -like $exePattern) {
                            $matchReasons += "exe:$exeName"
                        }
                    }
                }
            }
        }
        
        # Check uninstall display name matches (from discovered items with method = "registry")
        if ($module.matches.uninstallDisplayName) {
            foreach ($namePattern in $module.matches.uninstallDisplayName) {
                foreach ($discovery in $DiscoveredItems) {
                    if ($discovery.method -eq "registry" -and $discovery.displayName) {
                        if ($discovery.displayName -match $namePattern -or $discovery.displayName -like $namePattern) {
                            $matchReasons += "uninstall:$($discovery.displayName)"
                        }
                    }
                }
            }
        }
        
        # If any matches found, add to results
        if ($matchReasons.Count -gt 0) {
            $moduleMatches += @{
                moduleId = $moduleId
                displayName = $module.displayName
                matchReasons = $matchReasons
                hasRestore = ($module.restore -and $module.restore.Count -gt 0)
                hasVerify = ($module.verify -and $module.verify.Count -gt 0)
                hasCapture = ($module.capture -and $module.capture.files -and $module.capture.files.Count -gt 0)
                sensitivity = $module.sensitivity
            }
        }
    }
    
    # Sort deterministically by module ID
    if ($moduleMatches.Count -eq 0) {
        Write-Output -NoEnumerate @()
        return
    }
    $sorted = @($moduleMatches | Sort-Object -Property moduleId)
    
    Write-Output -NoEnumerate $sorted
}

function Format-ConfigModuleDiscoveryOutput {
    <#
    .SYNOPSIS
        Format config module matches for discovery output.
    .PARAMETER Matches
        Array of matched modules from Get-ConfigModulesForInstalledApps.
    .OUTPUTS
        Formatted string for console output.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [array]$Matches = @()
    )
    
    if ($Matches.Count -eq 0) {
        return "  No config modules available for detected apps."
    }
    
    $sb = [System.Text.StringBuilder]::new()
    
    foreach ($match in $Matches) {
        $features = @()
        if ($match.hasVerify) { $features += "verify" }
        if ($match.hasRestore) { $features += "restore" }
        if ($match.hasCapture) { $features += "capture" }
        $featureStr = if ($features.Count -gt 0) { " [$($features -join ', ')]" } else { "" }
        
        [void]$sb.AppendLine("  - $($match.moduleId): $($match.displayName)$featureStr")
        
        foreach ($reason in $match.matchReasons) {
            [void]$sb.AppendLine("      matched via $reason")
        }
    }
    
    return $sb.ToString().TrimEnd()
}

function Clear-ConfigModuleCatalogCache {
    <#
    .SYNOPSIS
        Clear the cached config module catalog.
    .DESCRIPTION
        Forces the next Get-ConfigModuleCatalog call to reload from disk.
        Useful for testing.
    #>
    $script:ConfigModuleCatalog = $null
    $script:ConfigModuleCatalogLoaded = $false
}

function Expand-ConfigPath {
    <#
    .SYNOPSIS
        Expand environment variables and ~ in a path.
    .DESCRIPTION
        Resolves paths like:
        - %APPDATA%\VSCodium\User\settings.json
        - ~/.gitconfig
        - $env:USERPROFILE\.ssh\config
    .PARAMETER Path
        The path to expand.
    .OUTPUTS
        Expanded absolute path string.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    # Expand ~ to user profile
    if ($Path.StartsWith("~/") -or $Path.StartsWith("~\")) {
        $Path = Join-Path $env:USERPROFILE $Path.Substring(2)
    } elseif ($Path -eq "~") {
        $Path = $env:USERPROFILE
    }
    
    # Expand environment variables (%VAR% and $env:VAR)
    $Path = [Environment]::ExpandEnvironmentVariables($Path)
    
    # Handle $env:VAR syntax (PowerShell-style)
    $Path = $Path -replace '\$env:([A-Za-z_][A-Za-z0-9_]*)', { 
        [Environment]::GetEnvironmentVariable($_.Groups[1].Value) 
    }
    
    return $Path
}

function Test-PathMatchesExcludeGlobs {
    <#
    .SYNOPSIS
        Check if a path matches any of the exclude glob patterns.
    .PARAMETER Path
        The path to check.
    .PARAMETER ExcludeGlobs
        Array of glob patterns to match against.
    .OUTPUTS
        $true if path matches any exclude pattern, $false otherwise.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [array]$ExcludeGlobs = @()
    )
    
    if ($ExcludeGlobs.Count -eq 0) {
        return $false
    }
    
    # Normalize path separators for matching
    $normalizedPath = $Path -replace '\\', '/'
    
    foreach ($glob in $ExcludeGlobs) {
        # Convert glob to regex-like pattern for -like operator
        $pattern = $glob -replace '\\', '/'
        
        if ($normalizedPath -like $pattern) {
            return $true
        }
    }
    
    return $false
}

function Invoke-ConfigModuleCapture {
    <#
    .SYNOPSIS
        Capture config files from modules into a payload directory.
    .DESCRIPTION
        For each selected module that has a capture section, copies source files
        to the payload output directory according to the module's capture.files mapping.
    .PARAMETER Modules
        Array of module IDs to capture from, or empty to use all matched modules.
    .PARAMETER MatchedModules
        Array of matched module info from Get-ConfigModulesForInstalledApps.
        Used when -Modules is empty to determine which modules to capture.
    .PARAMETER PayloadOut
        Output directory for captured payloads. Default: provisioning/payload/
    .OUTPUTS
        Hashtable with capture results: copied, skipped, missing, warnings, payloadRoot.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string[]]$Modules = @(),
        
        [Parameter(Mandatory = $false)]
        [array]$MatchedModules = @(),
        
        [Parameter(Mandatory = $false)]
        [string]$PayloadOut = $null
    )
    
    $catalog = Get-ConfigModuleCatalog
    
    # Determine payload output directory
    if (-not $PayloadOut) {
        $PayloadOut = Join-Path $PSScriptRoot "..\payload"
    }
    
    # Determine which modules to capture
    $modulesToCapture = @()
    
    if ($Modules.Count -gt 0) {
        # Explicit module selection
        foreach ($moduleId in $Modules) {
            if ($catalog.ContainsKey($moduleId)) {
                $modulesToCapture += $catalog[$moduleId]
            } else {
                Write-Warning "Unknown config module: $moduleId"
            }
        }
    } elseif ($MatchedModules.Count -gt 0) {
        # Use matched modules from discovery
        foreach ($match in $MatchedModules) {
            if ($catalog.ContainsKey($match.moduleId)) {
                $modulesToCapture += $catalog[$match.moduleId]
            }
        }
    }
    
    # Filter to modules that have capture sections
    $modulesToCapture = @($modulesToCapture | Where-Object { $_.capture -and $_.capture.files })
    
    $result = @{
        payloadRoot = $PayloadOut
        copied = @()
        skipped = @()
        missing = @()
        warnings = @()
        modulesCaptured = @()
    }
    
    if ($modulesToCapture.Count -eq 0) {
        $result.warnings += "No modules with capture definitions found"
        return $result
    }
    
    # Ensure payload directory exists
    if (-not (Test-Path $PayloadOut)) {
        New-Item -ItemType Directory -Path $PayloadOut -Force | Out-Null
    }
    
    foreach ($module in $modulesToCapture) {
        $moduleId = $module.id
        $excludeGlobs = if ($module.capture.excludeGlobs) { @($module.capture.excludeGlobs) } else { @() }
        $moduleCaptured = $false
        
        foreach ($fileEntry in $module.capture.files) {
            $sourcePath = Expand-ConfigPath -Path $fileEntry.source
            $destPath = Join-Path $PayloadOut $fileEntry.dest
            $isOptional = if ($fileEntry.ContainsKey('optional')) { $fileEntry.optional } else { $false }
            
            # Check if source matches exclude globs
            if (Test-PathMatchesExcludeGlobs -Path $sourcePath -ExcludeGlobs $excludeGlobs) {
                $result.skipped += @{
                    module = $moduleId
                    source = $sourcePath
                    dest = $destPath
                    reason = "Matched exclude glob"
                }
                continue
            }
            
            # Check if source exists
            if (-not (Test-Path $sourcePath)) {
                if ($isOptional) {
                    $result.skipped += @{
                        module = $moduleId
                        source = $sourcePath
                        dest = $destPath
                        reason = "Optional file not found"
                    }
                } else {
                    $result.missing += @{
                        module = $moduleId
                        source = $sourcePath
                        dest = $destPath
                    }
                    $result.warnings += "Missing required file: $sourcePath (module: $moduleId)"
                }
                continue
            }
            
            # Ensure destination directory exists
            $destDir = Split-Path -Parent $destPath
            if ($destDir -and -not (Test-Path $destDir)) {
                New-Item -ItemType Directory -Path $destDir -Force | Out-Null
            }
            
            # Copy the file or directory
            try {
                if (Test-Path -Path $sourcePath -PathType Container) {
                    # Source is a directory — copy recursively
                    Copy-Item -Path $sourcePath -Destination $destPath -Recurse -Force
                    $copiedFiles = @(Get-ChildItem -Path $destPath -Recurse -File -ErrorAction SilentlyContinue)
                    foreach ($f in $copiedFiles) {
                        $result.copied += @{
                            module = $moduleId
                            source = $f.FullName -replace [regex]::Escape($destPath), $sourcePath
                            dest = $f.FullName
                        }
                    }
                    if ($copiedFiles.Count -gt 0) {
                        $moduleCaptured = $true
                    }
                } else {
                    # Source is a single file
                    Copy-Item -Path $sourcePath -Destination $destPath -Force
                    $result.copied += @{
                        module = $moduleId
                        source = $sourcePath
                        dest = $destPath
                    }
                    $moduleCaptured = $true
                }
            } catch {
                $result.warnings += "Failed to copy $sourcePath to $destPath`: $($_.Exception.Message)"
                $result.missing += @{
                    module = $moduleId
                    source = $sourcePath
                    dest = $destPath
                    error = $_.Exception.Message
                }
            }
        }
        
        if ($moduleCaptured) {
            $result.modulesCaptured += $moduleId
        }
    }
    
    return $result
}

function Format-ConfigCaptureOutput {
    <#
    .SYNOPSIS
        Format capture results for console output.
    .PARAMETER CaptureResult
        Result hashtable from Invoke-ConfigModuleCapture.
    .OUTPUTS
        Formatted string for console output.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$CaptureResult
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    [void]$sb.AppendLine("Config Payload Capture Summary")
    [void]$sb.AppendLine("==============================")
    [void]$sb.AppendLine("Payload root: $($CaptureResult.payloadRoot)")
    [void]$sb.AppendLine("")
    [void]$sb.AppendLine("Copied:  $($CaptureResult.copied.Count)")
    [void]$sb.AppendLine("Skipped: $($CaptureResult.skipped.Count)")
    [void]$sb.AppendLine("Missing: $($CaptureResult.missing.Count)")
    
    if ($CaptureResult.modulesCaptured.Count -gt 0) {
        [void]$sb.AppendLine("")
        [void]$sb.AppendLine("Modules captured: $($CaptureResult.modulesCaptured -join ', ')")
    }
    
    if ($CaptureResult.copied.Count -gt 0) {
        [void]$sb.AppendLine("")
        [void]$sb.AppendLine("Copied files:")
        foreach ($item in $CaptureResult.copied) {
            [void]$sb.AppendLine("  + $($item.source) -> $($item.dest)")
        }
    }
    
    if ($CaptureResult.skipped.Count -gt 0) {
        [void]$sb.AppendLine("")
        [void]$sb.AppendLine("Skipped files:")
        foreach ($item in $CaptureResult.skipped) {
            [void]$sb.AppendLine("  - $($item.source) ($($item.reason))")
        }
    }
    
    if ($CaptureResult.warnings.Count -gt 0) {
        [void]$sb.AppendLine("")
        [void]$sb.AppendLine("Warnings:")
        foreach ($warning in $CaptureResult.warnings) {
            [void]$sb.AppendLine("  ! $warning")
        }
    }
    
    return $sb.ToString()
}

# Functions exported: Get-ConfigModuleCatalog, Test-ConfigModuleSchema, Expand-ManifestConfigModules, Get-ConfigModulesForInstalledApps, Format-ConfigModuleDiscoveryOutput, Clear-ConfigModuleCatalogCache, Expand-ConfigPath, Test-PathMatchesExcludeGlobs, Invoke-ConfigModuleCapture, Format-ConfigCaptureOutput
