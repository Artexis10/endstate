<#
.SYNOPSIS
    Sandbox-side script for discovery harness.

.DESCRIPTION
    Runs inside Windows Sandbox to:
    1. Capture pre-install filesystem snapshot
    2. Install app via winget
    3. Capture post-install filesystem snapshot
    4. Copy artifacts to mapped output folder

.PARAMETER WingetId
    The winget package ID to install.

.PARAMETER OutputDir
    Mapped folder path for output artifacts.

.PARAMETER Roots
    Directories to snapshot (comma-separated).

.PARAMETER DryRun
    If set, skip winget install (for testing wiring).
#>
param(
    [Parameter(Mandatory = $true)]
    [string]$WingetId,
    
    [Parameter(Mandatory = $true)]
    [string]$OutputDir,
    
    [Parameter(Mandatory = $false)]
    [string]$Roots = "",
    
    [Parameter(Mandatory = $false)]
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

# Resolve script location
$script:HarnessRoot = $PSScriptRoot
$script:RepoRoot = (Resolve-Path (Join-Path $script:HarnessRoot "..\..")).Path

# Load snapshot module
$snapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
if (-not (Test-Path $snapshotModule)) {
    Write-Error "Snapshot module not found: $snapshotModule"
    exit 1
}
. $snapshotModule

function Write-Step {
    param([string]$Message)
    Write-Host "[STEP] $Message" -ForegroundColor Yellow
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Select-BestPackageCandidate {
    <#
    .SYNOPSIS
        Selects the best package candidate from a list, preferring x64 > neutral > largest.
    .PARAMETER Candidates
        Array of FileInfo objects representing package candidates.
    .PARAMETER PackageName
        Name of the package (for logging purposes).
    .OUTPUTS
        The selected FileInfo object, or $null if no candidates.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [AllowNull()]
        [System.IO.FileInfo[]]$Candidates,
        
        [Parameter(Mandatory = $false)]
        [string]$PackageName = "package"
    )
    
    if (-not $Candidates -or $Candidates.Count -eq 0) {
        return $null
    }
    
    # Prefer x64 candidates
    $x64Candidates = $Candidates | Where-Object {
        $_.Name -match 'x64' -or 
        $_.FullName -match '\\x64\\' -or 
        $_.FullName -match '\\win10-x64\\' -or
        $_.FullName -match '\\runtimes\\win10-x64\\'
    }
    
    # Neutral candidates
    $neutralCandidates = $Candidates | Where-Object {
        $_.Name -match 'neutral' -or
        $_.FullName -match '\\neutral\\'
    }
    
    $selected = $null
    if ($x64Candidates -and $x64Candidates.Count -gt 0) {
        $selected = $x64Candidates | Sort-Object -Property Length -Descending | Select-Object -First 1
        Write-Info "Selected x64 $PackageName candidate: $($selected.Name) ($($selected.Length) bytes)"
    }
    elseif ($neutralCandidates -and $neutralCandidates.Count -gt 0) {
        $selected = $neutralCandidates | Sort-Object -Property Length -Descending | Select-Object -First 1
        Write-Info "Selected neutral $PackageName candidate: $($selected.Name) ($($selected.Length) bytes)"
    }
    elseif ($Candidates.Count -eq 1) {
        $selected = $Candidates[0]
        Write-Info "Using single $PackageName candidate: $($selected.Name) ($($selected.Length) bytes)"
    }
    else {
        $selected = $Candidates | Sort-Object -Property Length -Descending | Select-Object -First 1
        Write-Info "Selected largest $PackageName candidate: $($selected.Name) ($($selected.Length) bytes)"
    }
    
    return $selected
}

function Ensure-WindowsAppRuntime18 {
    <#
    .SYNOPSIS
        Ensures Microsoft.WindowsAppRuntime.1.8 framework packages are installed.
    .DESCRIPTION
        Windows App Runtime 1.8 is required by recent versions of App Installer (winget).
        This function downloads the official redistributable zip and installs the MSIX packages.
    .PARAMETER TempDir
        Temp directory for downloads. If not specified, uses $env:TEMP\winget-bootstrap.
    .OUTPUTS
        Array of installed MSIX package paths for use as dependencies.
    #>
    param(
        [Parameter(Mandatory = $false)]
        [string]$TempDir = (Join-Path $env:TEMP "winget-bootstrap")
    )
    
    Write-Step "Checking for Windows App Runtime 1.8..."
    
    # Check if already installed
    $runtime = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime.1.8" -ErrorAction SilentlyContinue
    if ($runtime) {
        Write-Pass "Windows App Runtime 1.8 is installed: $($runtime.Version)"
        return @()
    }
    
    Write-Info "Windows App Runtime 1.8 not found. Installing from redist zip..."
    
    # Ensure TLS 1.2+ for downloads
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    # Create temp directory for downloads
    if (-not (Test-Path $TempDir)) {
        New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
    }
    
    # Windows App Runtime 1.8 redistributable zip URL
    # Contains MSIX packages that can be installed directly
    $redistUrl = "https://aka.ms/windowsappsdk/1.8/latest/Microsoft.WindowsAppRuntime.Redist.1.8.zip"
    $redistZipPath = Join-Path $TempDir "Microsoft.WindowsAppRuntime.Redist.1.8.zip"
    
    # Download the redist zip
    Write-Info "Downloading Windows App Runtime 1.8 redist from: $redistUrl"
    try {
        Invoke-WebRequest -Uri $redistUrl -OutFile $redistZipPath -UseBasicParsing
        Write-Pass "Downloaded: $redistZipPath ($((Get-Item $redistZipPath).Length) bytes)"
    }
    catch {
        throw "Failed to download Windows App Runtime 1.8 redist: $_"
    }
    
    # Extract the zip
    $redistExtractDir = Join-Path $TempDir "windowsappruntime-extract"
    if (Test-Path $redistExtractDir) {
        Remove-Item $redistExtractDir -Recurse -Force
    }
    
    Write-Info "Extracting redist zip..."
    Expand-Archive -Path $redistZipPath -DestinationPath $redistExtractDir -Force
    Write-Pass "Extracted to: $redistExtractDir"
    
    # Find all MSIX packages in the extracted zip
    $msixPackages = Get-ChildItem -Path $redistExtractDir -Recurse -Include "*.msix" -File
    Write-Info "Found $($msixPackages.Count) MSIX packages in redist"
    
    # Filter for x64 packages (prefer x64 for Windows Sandbox which is x64)
    $x64Packages = $msixPackages | Where-Object { $_.Name -match 'x64' }
    Write-Info "Found $($x64Packages.Count) x64 packages"
    
    # Install each x64 package
    $installedPaths = @()
    foreach ($pkg in $x64Packages) {
        try {
            Write-Info "Installing: $($pkg.Name)"
            Add-AppxPackage -Path $pkg.FullName
            Write-Pass "Installed: $($pkg.Name)"
            $installedPaths += $pkg.FullName
        }
        catch {
            Write-Host "[WARN] Package install failed (may already be present): $($pkg.Name) - $_" -ForegroundColor Yellow
        }
    }
    
    # Validate installation
    $runtime = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime.1.8" -ErrorAction SilentlyContinue
    if (-not $runtime) {
        # Build diagnostic info
        $diagInfo = @"
Windows App Runtime 1.8 installation completed but main package not found.
Redist URL: $redistUrl
Redist zip exists: $(Test-Path $redistZipPath)
Extract directory: $redistExtractDir

All MSIX packages found:
$($msixPackages | ForEach-Object { "  $($_.FullName) ($($_.Length) bytes)" } | Out-String)

x64 packages attempted:
$($x64Packages | ForEach-Object { "  $($_.FullName) ($($_.Length) bytes)" } | Out-String)

Installed WindowsAppRuntime packages:
$(Get-AppxPackage -Name "Microsoft.WindowsAppRuntime*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
"@
        throw $diagInfo
    }
    
    Write-Pass "Windows App Runtime 1.8 installed: $($runtime.Version)"
    return $installedPaths
}

function Ensure-Winget {
    <#
    .SYNOPSIS
        Ensures winget is available, bootstrapping it if necessary.
    .DESCRIPTION
        Checks if winget is installed. If not, downloads the App Installer MSIX bundle
        and its required dependencies (VCLibs, UI.Xaml, WindowsAppRuntime) from explicit URLs,
        then installs them. This is needed because Windows Sandbox does not include winget
        by default and the msixbundle does not contain all required dependencies.
    #>
    
    Write-Step "Checking for winget..."
    
    # Check if winget is already available
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Pass "winget is available at: $($wingetCmd.Source)"
        return
    }
    
    Write-Info "winget not found. Bootstrapping with explicit dependency downloads..."
    
    # Ensure TLS 1.2+ for downloads
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    # Create temp directory for downloads
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    
    # Track all dependency paths for -DependencyPath parameter
    $dependencyPaths = @()
    $downloadedDeps = @()
    
    # ========================================================================
    # Step 1: Install Windows App Runtime 1.8 (required framework)
    # ========================================================================
    Write-Step "Installing Windows App Runtime 1.8..."
    $runtimePaths = Ensure-WindowsAppRuntime18 -TempDir $tempDir
    if ($runtimePaths -and $runtimePaths.Count -gt 0) {
        $dependencyPaths += $runtimePaths
        $downloadedDeps += "WindowsAppRuntime: $($runtimePaths.Count) packages installed"
    }
    
    # ========================================================================
    # Step 2: Download and install VCLibs Desktop (x64)
    # ========================================================================
    Write-Step "Downloading VCLibs Desktop dependency..."
    $vcLibsDesktopUrl = "https://aka.ms/Microsoft.VCLibs.x64.14.00.Desktop.appx"
    $vcLibsDesktopPath = Join-Path $tempDir "Microsoft.VCLibs.x64.14.00.Desktop.appx"
    
    try {
        Write-Info "Downloading VCLibs Desktop from: $vcLibsDesktopUrl"
        Invoke-WebRequest -Uri $vcLibsDesktopUrl -OutFile $vcLibsDesktopPath -UseBasicParsing
        Write-Pass "Downloaded: $vcLibsDesktopPath ($((Get-Item $vcLibsDesktopPath).Length) bytes)"
        $downloadedDeps += "VCLibs Desktop: $vcLibsDesktopPath"
        
        Write-Info "Installing VCLibs Desktop..."
        Add-AppxPackage -Path $vcLibsDesktopPath
        Write-Pass "Installed VCLibs Desktop"
        $dependencyPaths += $vcLibsDesktopPath
    }
    catch {
        Write-Host "[WARN] VCLibs Desktop install failed (may already be present): $_" -ForegroundColor Yellow
    }
    
    # ========================================================================
    # Step 3: Download and install UI.Xaml (from GitHub release)
    # ========================================================================
    Write-Step "Downloading UI.Xaml dependency..."
    # Microsoft.UI.Xaml 2.8.x release - x64 appx from GitHub
    $uiXamlUrl = "https://github.com/nicholasrice/nicholasrice.github.io/releases/download/v1.0.0/Microsoft.UI.Xaml.2.8.x64.appx"
    $uiXamlPath = Join-Path $tempDir "Microsoft.UI.Xaml.2.8.x64.appx"
    
    # Fallback: try NuGet package extraction if GitHub fails
    $uiXamlInstalled = $false
    
    try {
        Write-Info "Downloading UI.Xaml from: $uiXamlUrl"
        Invoke-WebRequest -Uri $uiXamlUrl -OutFile $uiXamlPath -UseBasicParsing
        Write-Pass "Downloaded: $uiXamlPath ($((Get-Item $uiXamlPath).Length) bytes)"
        $downloadedDeps += "UI.Xaml: $uiXamlPath"
        
        Write-Info "Installing UI.Xaml..."
        Add-AppxPackage -Path $uiXamlPath
        Write-Pass "Installed UI.Xaml"
        $dependencyPaths += $uiXamlPath
        $uiXamlInstalled = $true
    }
    catch {
        Write-Host "[WARN] UI.Xaml download/install from GitHub failed: $_" -ForegroundColor Yellow
    }
    
    # Fallback: Extract from NuGet package
    if (-not $uiXamlInstalled) {
        Write-Info "Trying UI.Xaml fallback via NuGet package..."
        $uiXamlNugetUrl = "https://www.nuget.org/api/v2/package/Microsoft.UI.Xaml/2.8.6"
        $uiXamlNupkg = Join-Path $tempDir "Microsoft.UI.Xaml.2.8.6.nupkg"
        
        try {
            Write-Info "Downloading UI.Xaml NuGet package from: $uiXamlNugetUrl"
            Invoke-WebRequest -Uri $uiXamlNugetUrl -OutFile $uiXamlNupkg -UseBasicParsing
            Write-Pass "Downloaded: $uiXamlNupkg"
            
            # Extract nupkg (copy to .zip for PS 5.1 compatibility)
            $uiXamlZip = [System.IO.Path]::ChangeExtension($uiXamlNupkg, '.zip')
            Copy-Item -Path $uiXamlNupkg -Destination $uiXamlZip -Force
            
            $uiXamlExtract = Join-Path $tempDir "uixaml-extract"
            if (Test-Path $uiXamlExtract) {
                Remove-Item $uiXamlExtract -Recurse -Force
            }
            Expand-Archive -Path $uiXamlZip -DestinationPath $uiXamlExtract -Force
            
            # Find x64 appx
            $uiXamlCandidates = Get-ChildItem -Path $uiXamlExtract -Recurse -Include "*.appx" -File
            $selectedUiXaml = Select-BestPackageCandidate -Candidates $uiXamlCandidates -PackageName "UI.Xaml"
            
            if ($selectedUiXaml) {
                Write-Info "Installing UI.Xaml from NuGet: $($selectedUiXaml.Name)"
                Add-AppxPackage -Path $selectedUiXaml.FullName
                Write-Pass "Installed UI.Xaml from NuGet"
                $dependencyPaths += $selectedUiXaml.FullName
                $downloadedDeps += "UI.Xaml (NuGet): $($selectedUiXaml.FullName)"
                $uiXamlInstalled = $true
            }
        }
        catch {
            Write-Host "[WARN] UI.Xaml NuGet fallback failed: $_" -ForegroundColor Yellow
        }
    }
    
    # ========================================================================
    # Step 4: Download App Installer bundle
    # ========================================================================
    Write-Step "Downloading App Installer bundle..."
    $wingetUrl = "https://aka.ms/getwinget"
    $wingetBundlePath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    
    try {
        Write-Info "Downloading App Installer bundle from: $wingetUrl"
        Invoke-WebRequest -Uri $wingetUrl -OutFile $wingetBundlePath -UseBasicParsing
        Write-Pass "Downloaded: $wingetBundlePath ($((Get-Item $wingetBundlePath).Length) bytes)"
    }
    catch {
        throw "Failed to download App Installer bundle: $_"
    }
    
    # ========================================================================
    # Step 5: Install App Installer bundle with dependencies
    # ========================================================================
    Write-Step "Installing App Installer bundle..."
    $installSuccess = $false
    $installError = $null
    
    try {
        if ($dependencyPaths.Count -gt 0) {
            Write-Info "Installing with -DependencyPath: $($dependencyPaths.Count) dependencies"
            foreach ($dep in $dependencyPaths) {
                Write-Info "  Dependency: $dep"
            }
            Add-AppxPackage -Path $wingetBundlePath -DependencyPath $dependencyPaths
        }
        else {
            Add-AppxPackage -Path $wingetBundlePath
        }
        $installSuccess = $true
        Write-Pass "Installed App Installer bundle"
    }
    catch {
        $installError = $_
        Write-Host "[WARN] App Installer install failed: $_" -ForegroundColor Yellow
        
        # Build comprehensive diagnostic message
        $diagInfo = @"
App Installer installation failed.
Error: $installError

Bundle path: $wingetBundlePath
Bundle exists: $(Test-Path $wingetBundlePath)
Bundle size: $((Get-Item $wingetBundlePath -ErrorAction SilentlyContinue).Length) bytes

Downloaded dependencies:
$($downloadedDeps | ForEach-Object { "  $_" } | Out-String)

Dependency paths used:
$($dependencyPaths | ForEach-Object { "  $_ (exists: $(Test-Path $_))" } | Out-String)

Installed AppxPackages (VCLibs):
$(Get-AppxPackage -Name "*VCLibs*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)

Installed AppxPackages (UI.Xaml):
$(Get-AppxPackage -Name "*UI.Xaml*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)

Installed AppxPackages (WindowsAppRuntime):
$(Get-AppxPackage -Name "*WindowsAppRuntime*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
"@
        throw $diagInfo
    }
    
    # ========================================================================
    # Step 6: Verify winget is available
    # ========================================================================
    # Refresh PATH - winget installs to WindowsApps which should be in PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $wingetCmd) {
        # Try common install location directly
        $wingetExe = Join-Path $env:LOCALAPPDATA "Microsoft\WindowsApps\winget.exe"
        if (Test-Path $wingetExe) {
            Write-Pass "winget installed at: $wingetExe"
        }
        else {
            throw "winget installation completed but winget command is still not available"
        }
    }
    else {
        Write-Pass "winget available at: $($wingetCmd.Source)"
    }
    
    # Verify winget version
    try {
        $wingetVersion = & winget --version 2>&1
        Write-Pass "winget version: $wingetVersion"
    }
    catch {
        Write-Host "[WARN] Could not get winget version: $_" -ForegroundColor Yellow
    }
    
    # ========================================================================
    # Step 7: Best-effort winget source update
    # ========================================================================
    Write-Step "Updating winget sources (best-effort)..."
    try {
        $sourceResult = & winget source update 2>&1
        Write-Pass "winget source update completed"
    }
    catch {
        Write-Host "[WARN] winget source update failed (non-fatal): $_" -ForegroundColor Yellow
    }
    
    Write-Pass "winget bootstrap complete"
}

# Parse roots
$snapshotRoots = if ($Roots) {
    $Roots -split ','
} else {
    @(
        $env:LOCALAPPDATA,
        $env:APPDATA,
        $env:ProgramData,
        $env:ProgramFiles,
        ${env:ProgramFiles(x86)}
    )
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " Sandbox Discovery: $WingetId" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Info "Output directory: $OutputDir"
Write-Info "Snapshot roots: $($snapshotRoots -join ', ')"
Write-Info "Dry run: $DryRun"

# Ensure output directory exists
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

# Sentinel file paths
$doneFile = Join-Path $OutputDir "DONE.txt"
$errorFile = Join-Path $OutputDir "ERROR.txt"

try {
    # Step 1: Pre-install snapshot
    Write-Step "Capturing pre-install snapshot..."
    $preSnapshot = Get-FilesystemSnapshot -Roots $snapshotRoots -MaxDepth 8
    Write-Info "Pre-snapshot: $($preSnapshot.Count) items"

    $preJsonPath = Join-Path $OutputDir "pre.json"
    $preSnapshot | ConvertTo-Json -Depth 10 -Compress | Out-File -FilePath $preJsonPath -Encoding UTF8
    Write-Pass "Saved: $preJsonPath"

    # Step 2: Ensure winget is available (bootstrap if needed)
    if (-not $DryRun) {
        Ensure-Winget
    }
    
    # Step 3: Install via winget
    if ($DryRun) {
        Write-Step "DRY RUN: Skipping winget install for $WingetId"
        Write-Info "Would run: winget install --id $WingetId --silent --accept-package-agreements --accept-source-agreements"
    } else {
        Write-Step "Installing $WingetId via winget..."
        
        $wingetArgs = @(
            "install",
            "--id", $WingetId,
            "--silent",
            "--accept-package-agreements",
            "--accept-source-agreements"
        )
        
        Write-Info "Running: winget $($wingetArgs -join ' ')"
        
        $result = & winget @wingetArgs 2>&1
        $exitCode = $LASTEXITCODE
        
        Write-Info "Winget output:"
        $result | ForEach-Object { Write-Host "  $_" }
        
        if ($exitCode -ne 0) {
            Write-Host "[WARN] Winget exited with code $exitCode (may be OK if already installed)" -ForegroundColor Yellow
        } else {
            Write-Pass "Winget install completed"
        }
        
        # Wait for installers to settle
        Write-Info "Waiting 5 seconds for installers to settle..."
        Start-Sleep -Seconds 5
    }

    # Step 4: Post-install snapshot
    Write-Step "Capturing post-install snapshot..."
    $postSnapshot = Get-FilesystemSnapshot -Roots $snapshotRoots -MaxDepth 8
    Write-Info "Post-snapshot: $($postSnapshot.Count) items"

    $postJsonPath = Join-Path $OutputDir "post.json"
    $postSnapshot | ConvertTo-Json -Depth 10 -Compress | Out-File -FilePath $postJsonPath -Encoding UTF8
    Write-Pass "Saved: $postJsonPath"

    # Step 5: Compute diff inside sandbox (for immediate feedback)
    Write-Step "Computing diff..."
    $diff = Compare-FilesystemSnapshots -PreSnapshot $preSnapshot -PostSnapshot $postSnapshot
    Write-Info "Added: $($diff.added.Count) items"
    Write-Info "Modified: $($diff.modified.Count) items"

    $diffJsonPath = Join-Path $OutputDir "diff.json"
    $diff | ConvertTo-Json -Depth 10 | Out-File -FilePath $diffJsonPath -Encoding UTF8
    Write-Pass "Saved: $diffJsonPath"

    # Write DONE.txt sentinel on success
    "SUCCESS" | Out-File -FilePath $doneFile -Encoding UTF8
    Write-Pass "Wrote sentinel: $doneFile"

    # Summary
    Write-Host ""
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host " Sandbox Discovery Complete" -ForegroundColor Cyan
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host ""
    Write-Pass "Artifacts saved to: $OutputDir"
    Write-Info "  - pre.json ($($preSnapshot.Count) items)"
    Write-Info "  - post.json ($($postSnapshot.Count) items)"
    Write-Info "  - diff.json ($($diff.added.Count) added, $($diff.modified.Count) modified)"
    Write-Info "  - DONE.txt"
    Write-Host ""

    exit 0
}
catch {
    # Write ERROR.txt with exception details
    $errorContent = @"
Exception: $($_.Exception.Message)
ScriptStackTrace: $($_.ScriptStackTrace)
InvocationInfo: $($_.InvocationInfo.PositionMessage)
"@
    $errorContent | Out-File -FilePath $errorFile -Encoding UTF8
    Write-Host "[ERROR] $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "[ERROR] Details written to: $errorFile" -ForegroundColor Red
    exit 1
}
