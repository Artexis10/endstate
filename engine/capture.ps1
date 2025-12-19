<#
.SYNOPSIS
    Capture current machine state into a provisioning manifest.

.DESCRIPTION
    Uses winget export to capture installed applications and generates
    a platform-agnostic manifest for provisioning.
    
    Supports profile-based output, filtering (runtimes, store apps), 
    minimization, optional template generation, and discovery mode.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\external.ps1"

# Sensitive paths that should never be auto-exported
$script:SensitivePaths = @(
    "$env:USERPROFILE\.ssh"
    "$env:USERPROFILE\.gnupg"
    "$env:USERPROFILE\.aws"
    "$env:USERPROFILE\.azure"
    "$env:APPDATA\Microsoft\Credentials"
    "$env:LOCALAPPDATA\Microsoft\Credentials"
    "$env:APPDATA\Mozilla\Firefox\Profiles"
    "$env:LOCALAPPDATA\Google\Chrome\User Data"
    "$env:LOCALAPPDATA\Microsoft\Edge\User Data"
    "$env:APPDATA\1Password"
    "$env:LOCALAPPDATA\1Password"
)

# Runtime/framework patterns to filter out (unless -IncludeRuntimes)
$script:RuntimePatterns = @(
    'Microsoft.VCRedist.*'
    'Microsoft.VCLibs.*'
    'Microsoft.UI.Xaml.*'
    'Microsoft.DotNet.*'
    'Microsoft.DotNet.DesktopRuntime.*'
    'Microsoft.DotNet.HostingBundle.*'
    'Microsoft.WindowsAppRuntime.*'
    'Microsoft.DirectX.*'
)

# Store app ID patterns (9N*, XP* prefixes)
$script:StoreIdPatterns = @(
    '^9[A-Z0-9]{10,}$'
    '^XP[A-Z0-9]{10,}$'
)

function Invoke-Capture {
    <#
    .SYNOPSIS
        Capture installed applications into a provisioning manifest.
    .PARAMETER Profile
        Profile name. Writes to manifests/<profile>.jsonc by default.
    .PARAMETER OutManifest
        Explicit output path (overrides profile-based path).
    .PARAMETER IncludeRuntimes
        Include runtime/framework packages (vcredist, .NET, etc.). Default: false.
    .PARAMETER IncludeStoreApps
        Include Microsoft Store apps. Default: false.
    .PARAMETER Minimize
        Drop entries without stable refs (no windows ref).
    .PARAMETER IncludeRestoreTemplate
        Generate manifests/includes/<profile>-restore.jsonc template.
    .PARAMETER IncludeVerifyTemplate
        Generate manifests/includes/<profile>-verify.jsonc template.
    .PARAMETER Discover
        Enable discovery mode: detect software present but not winget-managed.
    .PARAMETER DiscoverWriteManualInclude
        Generate manifests/includes/<profile>-manual.jsonc with commented suggestions.
        Default: true when -Discover is enabled. Requires -Profile.
    .PARAMETER Update
        Merge new capture into existing manifest instead of overwriting.
        Preserves includes, restore, verify blocks. Updates captured timestamp.
        If manifest doesn't exist, behaves like normal capture.
    .PARAMETER PruneMissingApps
        When used with -Update, remove apps from root manifest that are no longer
        present in the new capture. Never prunes apps from included manifests.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$Profile,
        
        [Parameter(Mandatory = $false)]
        [string]$OutManifest,
        
        [Parameter(Mandatory = $false)]
        [switch]$IncludeRuntimes,
        
        [Parameter(Mandatory = $false)]
        [switch]$IncludeStoreApps,
        
        [Parameter(Mandatory = $false)]
        [switch]$Minimize,
        
        [Parameter(Mandatory = $false)]
        [switch]$IncludeRestoreTemplate,
        
        [Parameter(Mandatory = $false)]
        [switch]$IncludeVerifyTemplate,
        
        [Parameter(Mandatory = $false)]
        [switch]$Discover,
        
        [Parameter(Mandatory = $false)]
        [Nullable[bool]]$DiscoverWriteManualInclude = $null,
        
        [Parameter(Mandatory = $false)]
        [switch]$Update,
        
        [Parameter(Mandatory = $false)]
        [switch]$PruneMissingApps
    )
    
    # Default DiscoverWriteManualInclude to true when Discover is enabled
    if ($Discover -and $null -eq $DiscoverWriteManualInclude) {
        $DiscoverWriteManualInclude = $true
    }
    
    # Validate: templates require -Profile
    if (($IncludeRestoreTemplate -or $IncludeVerifyTemplate) -and -not $Profile) {
        Write-Host "[ERROR] -IncludeRestoreTemplate and -IncludeVerifyTemplate require -Profile." -ForegroundColor Red
        return $null
    }
    
    # Validate: manual include requires -Profile
    if ($DiscoverWriteManualInclude -and -not $Profile) {
        Write-Host "[ERROR] -DiscoverWriteManualInclude requires -Profile." -ForegroundColor Red
        return $null
    }
    
    # Validate: -PruneMissingApps requires -Update
    if ($PruneMissingApps -and -not $Update) {
        Write-Host "[ERROR] -PruneMissingApps requires -Update." -ForegroundColor Red
        return $null
    }
    
    # Validate: need either -Profile or -OutManifest
    if (-not $Profile -and -not $OutManifest) {
        Write-Host "[ERROR] Either -Profile or -OutManifest is required." -ForegroundColor Red
        return $null
    }
    
    # Determine output path
    $manifestsDir = Join-Path $PSScriptRoot "..\manifests"
    if ($OutManifest) {
        $outputPath = $OutManifest
    } else {
        $outputPath = Join-Path $manifestsDir "$Profile.jsonc"
    }
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "capture-$runId"
    
    Write-ProvisioningSection "Provisioning Capture"
    Write-ProvisioningLog "Starting capture on $env:COMPUTERNAME" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    if ($Profile) {
        Write-ProvisioningLog "Profile: $Profile" -Level INFO
    }
    Write-ProvisioningLog "Output manifest: $outputPath" -Level INFO
    Write-ProvisioningLog "Filters: IncludeRuntimes=$IncludeRuntimes, IncludeStoreApps=$IncludeStoreApps, Minimize=$Minimize" -Level INFO
    if ($Update) {
        Write-ProvisioningLog "Update mode: enabled (PruneMissingApps=$PruneMissingApps)" -Level INFO
    }
    
    # Ensure output directory exists
    $outDir = Split-Path -Parent $outputPath
    if ($outDir -and -not (Test-Path $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
        Write-ProvisioningLog "Created output directory: $outDir" -Level INFO
    }
    
    # Create capture directory for intermediate files
    $captureDir = Join-Path $PSScriptRoot "..\state\capture\$runId"
    if (-not (Test-Path $captureDir)) {
        New-Item -ItemType Directory -Path $captureDir -Force | Out-Null
    }
    Write-ProvisioningLog "Capture directory: $captureDir" -Level INFO
    
    # Check for winget
    Write-ProvisioningSection "Checking Prerequisites"
    $wingetAvailable = Test-WingetAvailable
    if (-not $wingetAvailable) {
        Write-ProvisioningLog "winget is not available. Cannot capture applications." -Level ERROR
        return $null
    }
    Write-ProvisioningLog "winget is available" -Level SUCCESS
    
    # Capture applications
    Write-ProvisioningSection "Capturing Applications"
    $rawApps = Get-InstalledAppsViaWinget -CaptureDir $captureDir
    Write-ProvisioningLog "Raw capture: $($rawApps.Count) applications" -Level INFO
    
    # Apply filters
    $filteredApps = $rawApps
    $filterStats = @{ runtimes = 0; storeApps = 0; minimized = 0 }
    
    if (-not $IncludeRuntimes) {
        $beforeCount = $filteredApps.Count
        $filteredApps = @($filteredApps | Where-Object { -not (Test-IsRuntimePackage -PackageId $_.refs.windows) })
        $filterStats.runtimes = $beforeCount - $filteredApps.Count
        if ($filterStats.runtimes -gt 0) {
            Write-ProvisioningLog "Filtered $($filterStats.runtimes) runtime packages" -Level INFO
        }
    }
    
    if (-not $IncludeStoreApps) {
        $beforeCount = $filteredApps.Count
        $filteredApps = @($filteredApps | Where-Object { -not (Test-IsStoreApp -App $_) })
        $filterStats.storeApps = $beforeCount - $filteredApps.Count
        if ($filterStats.storeApps -gt 0) {
            Write-ProvisioningLog "Filtered $($filterStats.storeApps) store apps" -Level INFO
        }
    }
    
    if ($Minimize) {
        $beforeCount = $filteredApps.Count
        $filteredApps = @($filteredApps | Where-Object { $_.refs -and $_.refs.windows })
        $filterStats.minimized = $beforeCount - $filteredApps.Count
        if ($filterStats.minimized -gt 0) {
            Write-ProvisioningLog "Minimized: dropped $($filterStats.minimized) entries without stable refs" -Level INFO
        }
    }
    
    # Sort apps deterministically by id
    $sortedApps = @($filteredApps | Sort-Object -Property { $_.id })
    Write-ProvisioningLog "Final app count: $($sortedApps.Count) applications" -Level SUCCESS
    
    # Check for sensitive paths
    Write-ProvisioningSection "Security Check"
    $sensitiveFound = Test-SensitivePaths
    if ($sensitiveFound.Count -gt 0) {
        Write-ProvisioningLog "Detected $($sensitiveFound.Count) sensitive paths (NOT exported):" -Level WARN
        foreach ($path in $sensitiveFound) {
            Write-ProvisioningLog "  - $path" -Level WARN
        }
    } else {
        Write-ProvisioningLog "No sensitive paths detected in common locations" -Level SUCCESS
    }
    
    # Discovery mode (opt-in)
    $discoveredItems = @()
    $manualIncludePath = $null
    if ($Discover) {
        Write-ProvisioningSection "Discovery Mode"
        Write-ProvisioningLog "Running discovery detectors..." -Level INFO
        
        # Load discovery module
        . "$PSScriptRoot\discovery.ps1"
        
        # Get winget installed package IDs for ownership cross-check
        $wingetInstalledIds = @($sortedApps | ForEach-Object { $_.refs.windows } | Where-Object { $_ })
        
        # Run discovery
        $discoveredItems = Invoke-Discovery -WingetInstalledIds $wingetInstalledIds
        
        # Filter to non-winget-owned items (the interesting findings)
        $nonOwnedDiscoveries = @($discoveredItems | Where-Object { -not $_.ownedByWinget })
        
        if ($nonOwnedDiscoveries.Count -gt 0) {
            Write-ProvisioningLog "Discovered $($nonOwnedDiscoveries.Count) non-winget-managed item(s):" -Level INFO
            foreach ($item in $nonOwnedDiscoveries) {
                $versionStr = if ($item.version) { " v$($item.version)" } elseif ($item.displayVersion) { " v$($item.displayVersion)" } else { "" }
                Write-ProvisioningLog "  [DISCOVERY] $($item.name)$versionStr ($($item.method)) - suggested: $($item.suggestedWingetId)" -Level INFO
            }
        } else {
            Write-ProvisioningLog "No non-winget-managed software discovered" -Level SUCCESS
        }
        
        # Generate manual include if requested
        if ($DiscoverWriteManualInclude -and $Profile) {
            $includesDir = Join-Path $manifestsDir "includes"
            if (-not (Test-Path $includesDir)) {
                New-Item -ItemType Directory -Path $includesDir -Force | Out-Null
            }
            
            $manualIncludePath = Join-Path $includesDir "$Profile-manual.jsonc"
            Write-ManualIncludeTemplate -Path $manualIncludePath -ProfileName $Profile -Discoveries $discoveredItems
            Write-ProvisioningLog "Generated manual include: $manualIncludePath" -Level SUCCESS
        }
        
        # Config module discovery mapping
        Write-ProvisioningSection "Config Modules Available"
        $configModulesPath = Join-Path $PSScriptRoot "config-modules.ps1"
        if (Test-Path $configModulesPath) {
            . $configModulesPath
            $moduleMatches = Get-ConfigModulesForInstalledApps -WingetInstalledIds $wingetInstalledIds -DiscoveredItems $discoveredItems
            
            if ($moduleMatches.Count -gt 0) {
                Write-ProvisioningLog "Config modules available for detected apps:" -Level INFO
                $formattedOutput = Format-ConfigModuleDiscoveryOutput -Matches $moduleMatches
                foreach ($line in ($formattedOutput -split "`n")) {
                    if ($line.Trim()) {
                        Write-ProvisioningLog $line -Level INFO
                    }
                }
                Write-Host ""
                Write-Host "To use these modules, add to your manifest:" -ForegroundColor Yellow
                Write-Host "  `"configModules`": [`"$($moduleMatches[0].moduleId)`"]" -ForegroundColor DarkGray
            } else {
                Write-ProvisioningLog "No config modules available for detected apps" -Level INFO
            }
        }
    }
    
    # Build manifest
    Write-ProvisioningSection "Generating Manifest"
    
    # Derive name from profile or output path
    $manifestName = if ($Profile) {
        $Profile.ToLower() -replace '\s+', '-'
    } else {
        $fileName = [System.IO.Path]::GetFileNameWithoutExtension($outputPath)
        $fileName.ToLower() -replace '\s+', '-'
    }
    
    # Collect new includes to add
    $generatedTemplates = @()
    $newIncludes = @()
    
    # Add manual include first (if generated by discovery)
    if ($manualIncludePath) {
        $newIncludes += "./includes/$Profile-manual.jsonc"
        $generatedTemplates += $manualIncludePath
    }
    
    if ($Profile -and ($IncludeRestoreTemplate -or $IncludeVerifyTemplate)) {
        $includesDir = Join-Path $manifestsDir "includes"
        if (-not (Test-Path $includesDir)) {
            New-Item -ItemType Directory -Path $includesDir -Force | Out-Null
            Write-ProvisioningLog "Created includes directory: $includesDir" -Level INFO
        }
        
        if ($IncludeRestoreTemplate) {
            $restoreTemplatePath = Join-Path $includesDir "$Profile-restore.jsonc"
            Write-RestoreTemplate -Path $restoreTemplatePath -ProfileName $Profile
            $newIncludes += "./includes/$Profile-restore.jsonc"
            $generatedTemplates += $restoreTemplatePath
            Write-ProvisioningLog "Generated restore template: $restoreTemplatePath" -Level SUCCESS
        }
        
        if ($IncludeVerifyTemplate) {
            $verifyTemplatePath = Join-Path $includesDir "$Profile-verify.jsonc"
            Write-VerifyTemplate -Path $verifyTemplatePath -ProfileName $Profile
            $newIncludes += "./includes/$Profile-verify.jsonc"
            $generatedTemplates += $verifyTemplatePath
            Write-ProvisioningLog "Generated verify template: $verifyTemplatePath" -Level SUCCESS
        }
    }
    
    # Check if we're in update mode and existing manifest exists
    $existingManifest = $null
    $isUpdateMode = $false
    
    if ($Update -and (Test-Path $outputPath)) {
        Write-ProvisioningLog "Loading existing manifest for update..." -Level INFO
        $existingManifest = Read-ManifestRaw -Path $outputPath
        
        if ($existingManifest) {
            $isUpdateMode = $true
            Write-ProvisioningLog "Existing manifest loaded: $($existingManifest.apps.Count) apps" -Level INFO
        } else {
            Write-ProvisioningLog "Could not load existing manifest, creating new" -Level WARN
        }
    }
    
    if ($isUpdateMode) {
        # Update mode: merge with existing manifest
        Write-ProvisioningLog "Merging with existing manifest..." -Level INFO
        
        # Get app IDs from included manifests to avoid duplication
        $includedAppIds = @{}
        $allIncludes = @()
        
        if ($existingManifest.includes) {
            $allIncludes += @($existingManifest.includes)
        }
        $allIncludes += $newIncludes
        
        if ($allIncludes.Count -gt 0) {
            $baseDir = Split-Path -Parent $outputPath
            try {
                $includedAppIds = Get-IncludedAppIds -IncludePaths $allIncludes -BaseDir $baseDir
                Write-ProvisioningLog "Found $($includedAppIds.Count) app IDs from includes (will not duplicate)" -Level INFO
            } catch {
                Write-ProvisioningLog "Warning: Could not load includes for deduplication: $($_.Exception.Message)" -Level WARN
            }
        }
        
        # Merge manifests
        $mergeParams = @{
            ExistingManifest = $existingManifest
            NewCaptureApps = $sortedApps
            IncludedAppIds = $includedAppIds
            NewIncludes = $newIncludes
        }
        
        if ($PruneMissingApps) {
            $mergeParams.PruneMissingApps = $true
        }
        
        $manifest = Merge-ManifestsForUpdate @mergeParams
        
        # Log merge results
        $existingCount = if ($existingManifest.apps) { $existingManifest.apps.Count } else { 0 }
        $newCount = $sortedApps.Count
        $mergedCount = $manifest.apps.Count
        Write-ProvisioningLog "Merge complete: existing=$existingCount, captured=$newCount, merged=$mergedCount" -Level INFO
        
        if ($PruneMissingApps) {
            $prunedCount = $existingCount - $mergedCount + ($newCount - $existingCount)
            if ($prunedCount -gt 0) {
                Write-ProvisioningLog "Pruned apps no longer present: ~$prunedCount" -Level INFO
            }
        }
    } else {
        # Normal capture mode: create new manifest
        $manifest = @{
            version = 1
            name = $manifestName
            captured = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
            apps = $sortedApps
            restore = @()
            verify = @()
        }
        
        if ($newIncludes.Count -gt 0) {
            $manifest.includes = $newIncludes
        }
    }
    
    # Save manifest to specified path
    Write-Manifest -Path $outputPath -Manifest $manifest
    Write-ProvisioningLog "Manifest saved: $outputPath" -Level SUCCESS
    
    # Summary
    $totalFiltered = $filterStats.runtimes + $filterStats.storeApps + $filterStats.minimized
    Close-ProvisioningLog -SuccessCount $sortedApps.Count -SkipCount $totalFiltered -FailCount 0
    
    Write-Host ""
    Write-Host "Capture complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Yellow
    Write-Host "  1. Review the manifest: $outputPath"
    Write-Host "  2. Generate a plan:     .\cli.ps1 -Command plan -Manifest `"$outputPath`""
    Write-Host "  3. Dry-run apply:       .\cli.ps1 -Command apply -Manifest `"$outputPath`" -DryRun"
    Write-Host ""
    
    $result = @{
        ManifestPath = $outputPath
        CaptureDir = $captureDir
        AppCount = $manifest.apps.Count
        FilteredCount = $totalFiltered
        FilterStats = $filterStats
        GeneratedTemplates = $generatedTemplates
        LogFile = $logFile
    }
    
    # Add discovery results if discovery was enabled
    if ($Discover) {
        $result.Discovered = $discoveredItems
        if ($manualIncludePath) {
            $result.ManualIncludePath = $manualIncludePath
        }
    }
    
    # Add update mode results
    if ($isUpdateMode) {
        $result.UpdateMode = $true
        $result.MergedFromExisting = $existingCount
        $result.PruneMissingApps = $PruneMissingApps.IsPresent
    }
    
    return $result
}

function Test-WingetAvailable {
    try {
        $null = Get-Command winget -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

function Invoke-WingetExport {
    <#
    .SYNOPSIS
        Execute winget export command. Separated for testability.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ExportPath
    )
    
    $null = & winget export -o $ExportPath --accept-source-agreements 2>&1
    return (Test-Path $ExportPath)
}

function Get-InstalledAppsViaWinget {
    param(
        [Parameter(Mandatory = $true)]
        [string]$CaptureDir
    )
    
    Write-ProvisioningLog "Running winget export..." -Level INFO
    
    # Export to JSON for parsing
    $exportPath = Join-Path $CaptureDir "winget-export.json"
    
    try {
        # Run winget export (via wrapper for testability)
        $exportSuccess = Invoke-WingetExport -ExportPath $exportPath
        
        if (-not $exportSuccess) {
            Write-ProvisioningLog "winget export did not produce output file" -Level ERROR
            return @()
        }
        
        # Parse the export
        $exportData = Get-Content -Path $exportPath -Raw | ConvertFrom-Json
        
        $apps = @()
        $sources = $exportData.Sources
        
        foreach ($source in $sources) {
            $sourceName = $source.SourceDetails.Name
            Write-ProvisioningLog "Processing source: $sourceName" -Level INFO
            
            foreach ($package in $source.Packages) {
                $packageId = $package.PackageIdentifier
                
                # Create app entry with platform-agnostic ID
                $appId = $packageId -replace '\.', '-' -replace '_', '-'
                $appId = $appId.ToLower()
                
                $app = @{
                    id = $appId
                    refs = @{
                        windows = $packageId
                    }
                    _source = $sourceName  # Internal metadata for filtering (msstore vs winget)
                }
                
                $apps += $app
                Write-ProvisioningLog "  + $packageId (source: $sourceName)" -Level ACTION
            }
        }
        
        Write-ProvisioningLog "Parsed $($apps.Count) packages from winget export" -Level INFO
        return $apps
        
    } catch {
        Write-ProvisioningLog "Error during winget export: $_" -Level ERROR
        return @()
    }
}

function Test-SensitivePaths {
    $found = @()
    
    foreach ($path in $script:SensitivePaths) {
        $expandedPath = [Environment]::ExpandEnvironmentVariables($path)
        if (Test-Path $expandedPath) {
            $found += $expandedPath
        }
    }
    
    return $found
}

function Test-IsRuntimePackage {
    <#
    .SYNOPSIS
        Check if a package ID matches runtime/framework patterns.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$PackageId
    )
    
    if (-not $PackageId) { return $false }
    
    foreach ($pattern in $script:RuntimePatterns) {
        if ($PackageId -like $pattern) {
            return $true
        }
    }
    
    return $false
}

function Test-IsStoreApp {
    <#
    .SYNOPSIS
        Check if an app is a Microsoft Store app.
        Detects by source (msstore) or store-ish ID patterns (9N*, XP*).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$App
    )
    
    # Check source metadata if available
    if ($App._source -and $App._source -eq 'msstore') {
        return $true
    }
    
    # Check ID patterns as fallback
    $packageId = $App.refs.windows
    if (-not $packageId) { return $false }
    
    foreach ($pattern in $script:StoreIdPatterns) {
        if ($packageId -match $pattern) {
            return $true
        }
    }
    
    return $false
}

function Write-RestoreTemplate {
    <#
    .SYNOPSIS
        Generate a commented restore template file.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfileName
    )
    
    $content = @"
{
  // Restore Template for profile: $ProfileName
  // Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
  // 
  // This file contains example restore steps. Uncomment and customize as needed.
  // Include this file in your main manifest via the "includes" array.

  "restore": [
    // Example: Copy a config file
    // { "type": "copy", "source": "./configs/.gitconfig", "target": "~/.gitconfig", "backup": true },
    
    // Example: Merge JSON settings
    // {
    //   "type": "merge",
    //   "format": "json",
    //   "source": "./configs/vscode-settings.json",
    //   "target": "`$env:APPDATA/Code/User/settings.json",
    //   "backup": true
    // },
    
    // Example: Append lines to a file
    // { "type": "append", "source": "./configs/extra-hosts.txt", "target": "C:/Windows/System32/drivers/etc/hosts", "backup": true }
  ]
}
"@
    
    $parentDir = Split-Path -Parent $Path
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    $content | Out-File -FilePath $Path -Encoding UTF8 -NoNewline
}

function Write-VerifyTemplate {
    <#
    .SYNOPSIS
        Generate a commented verify template file.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $true)]
        [string]$ProfileName
    )
    
    $content = @"
{
  // Verify Template for profile: $ProfileName
  // Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
  // 
  // This file contains example verification steps. Uncomment and customize as needed.
  // Include this file in your main manifest via the "includes" array.

  "verify": [
    // Example: Check if a file exists
    // { "type": "file-exists", "path": "~/.gitconfig" },
    
    // Example: Check if a command is available
    // { "type": "command-exists", "command": "git" },
    
    // Example: Check if a directory exists
    // { "type": "file-exists", "path": "C:/Program Files/Git" }
  ]
}
"@
    
    $parentDir = Split-Path -Parent $Path
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    $content | Out-File -FilePath $Path -Encoding UTF8 -NoNewline
}

# Functions exported: Invoke-Capture, Test-WingetAvailable, Invoke-WingetExport, Get-InstalledAppsViaWinget, Test-SensitivePaths, Test-IsRuntimePackage, Test-IsStoreApp, Write-RestoreTemplate, Write-VerifyTemplate
