<#
.SYNOPSIS
    Capture current machine state into a provisioning manifest.

.DESCRIPTION
    Uses winget export to capture installed applications and generates
    a platform-agnostic manifest for provisioning.
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"

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

function Invoke-Capture {
    param(
        [Parameter(Mandatory = $true)]
        [string]$OutManifest
    )
    
    $runId = Get-RunId
    $logFile = Initialize-ProvisioningLog -RunId "capture-$runId"
    
    Write-ProvisioningSection "Provisioning Capture"
    Write-ProvisioningLog "Starting capture on $env:COMPUTERNAME" -Level INFO
    Write-ProvisioningLog "Run ID: $runId" -Level INFO
    Write-ProvisioningLog "Output manifest: $OutManifest" -Level INFO
    
    # Ensure output directory exists
    $outDir = Split-Path -Parent $OutManifest
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
    $apps = Get-InstalledAppsViaWinget -CaptureDir $captureDir
    Write-ProvisioningLog "Captured $($apps.Count) applications" -Level SUCCESS
    
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
    
    # Build manifest
    Write-ProvisioningSection "Generating Manifest"
    
    # Derive name from output path
    $manifestFileName = [System.IO.Path]::GetFileNameWithoutExtension($OutManifest)
    $manifestName = $manifestFileName.ToLower() -replace '\s+', '-'
    
    $manifest = @{
        version = 1
        name = $manifestName
        captured = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        apps = $apps
        restore = @()
        verify = @()
    }
    
    # Save manifest to specified path
    Write-Manifest -Path $OutManifest -Manifest $manifest
    Write-ProvisioningLog "Manifest saved: $OutManifest" -Level SUCCESS
    
    # Summary
    Close-ProvisioningLog -SuccessCount $apps.Count -SkipCount 0 -FailCount 0
    
    Write-Host ""
    Write-Host "Capture complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Yellow
    Write-Host "  1. Review the manifest: $OutManifest"
    Write-Host "  2. Generate a plan:     .\cli.ps1 -Command plan -Manifest `"$OutManifest`""
    Write-Host "  3. Dry-run apply:       .\cli.ps1 -Command apply -Manifest `"$OutManifest`" -DryRun"
    Write-Host ""
    
    return @{
        ManifestPath = $OutManifest
        CaptureDir = $captureDir
        AppCount = $apps.Count
        LogFile = $logFile
    }
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
                }
                
                $apps += $app
                Write-ProvisioningLog "  + $packageId" -Level ACTION
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

# Functions exported: Invoke-Capture, Test-WingetAvailable, Invoke-WingetExport, Get-InstalledAppsViaWinget, Test-SensitivePaths
