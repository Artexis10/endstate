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
        Ensures Microsoft.WindowsAppRuntime.1.8 is installed.
    .DESCRIPTION
        Windows App Runtime 1.8 is required by recent versions of App Installer (winget).
        This function checks if it's present and installs it if missing.
    #>
    
    Write-Step "Checking for Windows App Runtime 1.8..."
    
    # Check if already installed
    $runtime = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime.1.8" -ErrorAction SilentlyContinue
    if ($runtime) {
        Write-Pass "Windows App Runtime 1.8 is installed: $($runtime.Version)"
        return
    }
    
    Write-Info "Windows App Runtime 1.8 not found. Installing..."
    
    # Ensure TLS 1.2+ for downloads
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    # Create temp directory for downloads
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    
    # Windows App Runtime 1.8 redistributable URL
    # Official Microsoft distribution via aka.ms
    $runtimeUrl = "https://aka.ms/windowsappsdk/1.8/latest/windowsappruntimeinstall-x64.exe"
    $runtimeExe = Join-Path $tempDir "windowsappruntimeinstall-x64.exe"
    
    # Download the runtime installer
    Write-Info "Downloading Windows App Runtime 1.8 from: $runtimeUrl"
    try {
        Invoke-WebRequest -Uri $runtimeUrl -OutFile $runtimeExe -UseBasicParsing
        Write-Pass "Downloaded: $runtimeExe"
    }
    catch {
        throw "Failed to download Windows App Runtime 1.8: $_"
    }
    
    # Install silently using the exe installer
    Write-Info "Installing Windows App Runtime 1.8..."
    $installArgs = @("--quiet", "--force")
    $installResult = Start-Process -FilePath $runtimeExe -ArgumentList $installArgs -Wait -PassThru
    $exitCode = $installResult.ExitCode
    
    Write-Info "Windows App Runtime installer exit code: $exitCode"
    
    # Exit codes: 0 = success, 3010 = success but reboot required
    if ($exitCode -ne 0 -and $exitCode -ne 3010) {
        # Build diagnostic info
        $diagInfo = @"
Windows App Runtime 1.8 installation failed.
Exit code: $exitCode
Installer path: $runtimeExe
Installer exists: $(Test-Path $runtimeExe)
Temp directory contents:
$(Get-ChildItem -Path $tempDir -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) ($($_.Length) bytes)" } | Out-String)
"@
        throw $diagInfo
    }
    
    # Validate installation
    $runtime = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime.1.8" -ErrorAction SilentlyContinue
    if (-not $runtime) {
        # Build diagnostic info
        $diagInfo = @"
Windows App Runtime 1.8 installation completed but package not found.
Exit code: $exitCode
Installer path: $runtimeExe
Temp directory contents:
$(Get-ChildItem -Path $tempDir -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) ($($_.Length) bytes)" } | Out-String)
Installed WindowsAppRuntime packages:
$(Get-AppxPackage -Name "Microsoft.WindowsAppRuntime*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
"@
        throw $diagInfo
    }
    
    Write-Pass "Windows App Runtime 1.8 installed: $($runtime.Version)"
}

function Ensure-Winget {
    <#
    .SYNOPSIS
        Ensures winget is available, bootstrapping it if necessary.
    .DESCRIPTION
        Checks if winget is installed. If not, downloads the App Installer MSIX bundle,
        extracts dependencies from within the bundle, and installs them.
        This is needed because Windows Sandbox does not include winget by default.
    #>
    
    Write-Step "Checking for winget..."
    
    # Check if winget is already available
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Pass "winget is available at: $($wingetCmd.Source)"
        return
    }
    
    Write-Info "winget not found. Bootstrapping from App Installer bundle..."
    
    # Ensure TLS 1.2+ for downloads
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    # Create temp directory for downloads
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    
    # Download App Installer bundle from aka.ms/getwinget
    $wingetUrl = "https://aka.ms/getwinget"
    $wingetBundlePath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    
    Write-Info "Downloading App Installer bundle from: $wingetUrl"
    try {
        Invoke-WebRequest -Uri $wingetUrl -OutFile $wingetBundlePath -UseBasicParsing
        Write-Pass "Downloaded: $wingetBundlePath"
    }
    catch {
        throw "Failed to download App Installer bundle: $_"
    }
    
    # Extract msixbundle to find dependencies
    # PowerShell 5.1 Expand-Archive requires .zip extension
    Write-Step "Extracting App Installer bundle to find dependencies..."
    $bundleZipPath = [System.IO.Path]::ChangeExtension($wingetBundlePath, '.zip')
    Copy-Item -Path $wingetBundlePath -Destination $bundleZipPath -Force
    
    $bundleExtractDir = Join-Path $tempDir "bundle-extract"
    if (Test-Path $bundleExtractDir) {
        Remove-Item $bundleExtractDir -Recurse -Force
    }
    Expand-Archive -Path $bundleZipPath -DestinationPath $bundleExtractDir -Force
    Write-Pass "Extracted bundle to: $bundleExtractDir"
    
    # Find all package files recursively in the extracted bundle
    $allPackages = Get-ChildItem -Path $bundleExtractDir -Recurse -Include "*.appx","*.msix" -File
    
    Write-Info "Found $($allPackages.Count) package files in bundle"
    
    # Find dependency packages within the bundle
    $vclibsCandidates = $allPackages | Where-Object { $_.Name -match 'Microsoft\.VCLibs\.140\.00' }
    $uiXamlCandidates = $allPackages | Where-Object { $_.Name -match 'Microsoft\.UI\.Xaml' }
    $runtimeCandidates = $allPackages | Where-Object { $_.Name -match 'Microsoft\.WindowsAppRuntime' }
    
    Write-Info "VCLibs candidates: $($vclibsCandidates.Count)"
    Write-Info "UI.Xaml candidates: $($uiXamlCandidates.Count)"
    Write-Info "WindowsAppRuntime candidates: $($runtimeCandidates.Count)"
    
    # Select best candidates using helper function
    $selectedVCLibs = Select-BestPackageCandidate -Candidates $vclibsCandidates -PackageName "VCLibs"
    $selectedUIXaml = Select-BestPackageCandidate -Candidates $uiXamlCandidates -PackageName "UI.Xaml"
    $selectedRuntime = Select-BestPackageCandidate -Candidates $runtimeCandidates -PackageName "WindowsAppRuntime"
    
    # Build list of found and missing dependencies for diagnostics
    $foundDeps = @()
    $missingDeps = @()
    $dependencyPaths = @()
    
    if ($selectedVCLibs) {
        $foundDeps += "VCLibs: $($selectedVCLibs.FullName) ($($selectedVCLibs.Length) bytes)"
        $dependencyPaths += $selectedVCLibs.FullName
    } else {
        $missingDeps += "Microsoft.VCLibs.140.00"
    }
    
    if ($selectedUIXaml) {
        $foundDeps += "UI.Xaml: $($selectedUIXaml.FullName) ($($selectedUIXaml.Length) bytes)"
        $dependencyPaths += $selectedUIXaml.FullName
    } else {
        $missingDeps += "Microsoft.UI.Xaml"
    }
    
    if ($selectedRuntime) {
        $foundDeps += "WindowsAppRuntime: $($selectedRuntime.FullName) ($($selectedRuntime.Length) bytes)"
        $dependencyPaths += $selectedRuntime.FullName
    }
    # WindowsAppRuntime is optional in bundle - we have fallback
    
    Write-Info "Found dependencies:"
    $foundDeps | ForEach-Object { Write-Info "  $_" }
    if ($missingDeps.Count -gt 0) {
        Write-Info "Dependencies not in bundle (will use fallback): $($missingDeps -join ', ')"
    }
    
    # Install dependencies explicitly first
    Write-Step "Installing bundle dependencies..."
    
    foreach ($depPath in $dependencyPaths) {
        $depName = [System.IO.Path]::GetFileName($depPath)
        try {
            Write-Info "Installing dependency: $depName"
            Add-AppxPackage -Path $depPath
            Write-Pass "Installed: $depName"
        }
        catch {
            Write-Host "[WARN] Dependency install failed (may already be present): $depName - $_" -ForegroundColor Yellow
        }
    }
    
    # Try to install the App Installer bundle
    Write-Step "Installing App Installer bundle..."
    $installSuccess = $false
    $installError = $null
    
    try {
        # Install with dependency paths
        if ($dependencyPaths.Count -gt 0) {
            Write-Info "Installing with -DependencyPath: $($dependencyPaths -join ', ')"
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
        Write-Host "[WARN] Initial App Installer install failed: $_" -ForegroundColor Yellow
    }
    
    # If install failed and WindowsAppRuntime was not in bundle, try fallback
    if (-not $installSuccess -and -not $selectedRuntime) {
        Write-Info "Attempting WindowsAppRuntime fallback..."
        try {
            Ensure-WindowsAppRuntime18
            
            # Retry App Installer installation
            Write-Step "Retrying App Installer installation after runtime install..."
            if ($dependencyPaths.Count -gt 0) {
                Add-AppxPackage -Path $wingetBundlePath -DependencyPath $dependencyPaths
            }
            else {
                Add-AppxPackage -Path $wingetBundlePath
            }
            $installSuccess = $true
            Write-Pass "Installed App Installer bundle (after runtime fallback)"
        }
        catch {
            # Build comprehensive diagnostic message
            $diagInfo = @"
App Installer installation failed even after WindowsAppRuntime fallback.
Original error: $installError
Retry error: $_

Bundle path: $wingetBundlePath
Bundle exists: $(Test-Path $wingetBundlePath)

Dependencies found in bundle:
$($foundDeps | ForEach-Object { "  $_" } | Out-String)

Dependencies missing from bundle:
$($missingDeps | ForEach-Object { "  $_" } | Out-String)

All packages in bundle:
$($allPackages | ForEach-Object { "  $($_.FullName) ($($_.Length) bytes)" } | Out-String)
"@
            throw $diagInfo
        }
    }
    elseif (-not $installSuccess) {
        # Build comprehensive diagnostic message
        $diagInfo = @"
App Installer installation failed.
Error: $installError

Bundle path: $wingetBundlePath
Bundle exists: $(Test-Path $wingetBundlePath)

Dependencies found in bundle:
$($foundDeps | ForEach-Object { "  $_" } | Out-String)

Dependencies missing from bundle:
$($missingDeps | ForEach-Object { "  $_" } | Out-String)

All packages in bundle:
$($allPackages | ForEach-Object { "  $($_.FullName) ($($_.Length) bytes)" } | Out-String)
"@
        throw $diagInfo
    }
    
    # Verify winget is now available
    # Need to refresh PATH - winget installs to WindowsApps which should be in PATH
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
    
    # Best-effort winget source update (don't fail if this doesn't work)
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
