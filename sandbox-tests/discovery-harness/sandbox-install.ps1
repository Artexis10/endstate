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
        Checks if winget is installed. If not, downloads and installs the
        App Installer MSIX bundle and its dependencies (Windows App Runtime 1.8, VCLibs, UI.Xaml).
        This is needed because Windows Sandbox does not include winget by default.
    #>
    
    Write-Step "Checking for winget..."
    
    # Check if winget is already available
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Pass "winget is available at: $($wingetCmd.Source)"
        return
    }
    
    Write-Info "winget not found. Bootstrapping..."
    
    # First ensure Windows App Runtime 1.8 is installed (required by App Installer)
    Ensure-WindowsAppRuntime18
    
    # Ensure TLS 1.2+ for downloads
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    # Create temp directory for downloads
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    
    # Download URLs (documented for maintainability)
    # VCLibs: Required C++ runtime dependency
    $vcLibsUrl = "https://aka.ms/Microsoft.VCLibs.x64.14.00.Desktop.appx"
    $vcLibsPath = Join-Path $tempDir "Microsoft.VCLibs.x64.14.00.Desktop.appx"
    
    # UI.Xaml: Required UI framework dependency (using stable 2.8.x release)
    # Source: https://www.nuget.org/packages/Microsoft.UI.Xaml
    $uiXamlUrl = "https://www.nuget.org/api/v2/package/Microsoft.UI.Xaml/2.8.6"
    $uiXamlNupkg = Join-Path $tempDir "Microsoft.UI.Xaml.2.8.6.nupkg"
    $uiXamlAppx = Join-Path $tempDir "Microsoft.UI.Xaml.2.8.appx"
    
    # Winget (App Installer): The main package
    $wingetUrl = "https://aka.ms/getwinget"
    $wingetPath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    
    # Download VCLibs
    Write-Info "Downloading VCLibs dependency..."
    try {
        Invoke-WebRequest -Uri $vcLibsUrl -OutFile $vcLibsPath -UseBasicParsing
        Write-Pass "Downloaded: $vcLibsPath"
    }
    catch {
        throw "Failed to download VCLibs: $_"
    }
    
    # Download UI.Xaml (comes as nupkg, need to extract appx)
    Write-Info "Downloading UI.Xaml dependency..."
    try {
        Invoke-WebRequest -Uri $uiXamlUrl -OutFile $uiXamlNupkg -UseBasicParsing
        Write-Pass "Downloaded: $uiXamlNupkg"
        
        # Extract the appx from the nupkg (it's a zip file)
        # PowerShell 5.1 Expand-Archive refuses .nupkg extension, so copy to .zip first
        $uiXamlZip = [System.IO.Path]::ChangeExtension($uiXamlNupkg, '.zip')
        Copy-Item -Path $uiXamlNupkg -Destination $uiXamlZip -Force
        
        $uiXamlExtract = Join-Path $tempDir "uixaml-extract"
        if (Test-Path $uiXamlExtract) {
            Remove-Item $uiXamlExtract -Recurse -Force
        }
        Expand-Archive -Path $uiXamlZip -DestinationPath $uiXamlExtract -Force
        
        # Find candidate packages (appx, msix, msixbundle) recursively
        $candidates = Get-ChildItem -Path $uiXamlExtract -Recurse -Include "*.appx","*.msix","*.msixbundle" -File
        
        if (-not $candidates -or $candidates.Count -eq 0) {
            throw "Could not find any appx/msix/msixbundle files in UI.Xaml package at: $uiXamlExtract"
        }
        
        # Identify x64 candidates by filename or folder path
        $x64Candidates = $candidates | Where-Object {
            $_.Name -match 'x64' -or 
            $_.FullName -match '\\x64\\' -or 
            $_.FullName -match '\\win10-x64\\' -or 
            $_.FullName -match '\\runtimes\\win10-x64\\'
        }
        
        $selectedCandidate = $null
        if ($x64Candidates -and $x64Candidates.Count -gt 0) {
            # Pick largest x64 candidate
            $selectedCandidate = $x64Candidates | Sort-Object -Property Length -Descending | Select-Object -First 1
            Write-Info "Selected x64 candidate: $($selectedCandidate.FullName) ($($selectedCandidate.Length) bytes)"
        }
        elseif ($candidates.Count -eq 1) {
            # Only one candidate, use it
            $selectedCandidate = $candidates[0]
            Write-Info "Using single candidate: $($selectedCandidate.FullName) ($($selectedCandidate.Length) bytes)"
        }
        else {
            # Multiple non-x64 candidates, pick largest
            $selectedCandidate = $candidates | Sort-Object -Property Length -Descending | Select-Object -First 1
            Write-Info "Selected largest candidate: $($selectedCandidate.FullName) ($($selectedCandidate.Length) bytes)"
        }
        
        if (-not $selectedCandidate) {
            # Build diagnostic message with all candidates
            $candidateList = ($candidates | ForEach-Object { "  $($_.FullName) ($($_.Length) bytes)" }) -join "`n"
            throw "Could not select UI.Xaml package candidate. Found $($candidates.Count) candidates:`n$candidateList"
        }
        
        Copy-Item -Path $selectedCandidate.FullName -Destination $uiXamlAppx -Force
        Write-Pass "Extracted: $uiXamlAppx"
    }
    catch {
        throw "Failed to download/extract UI.Xaml: $_"
    }
    
    # Download winget
    Write-Info "Downloading winget (App Installer)..."
    try {
        Invoke-WebRequest -Uri $wingetUrl -OutFile $wingetPath -UseBasicParsing
        Write-Pass "Downloaded: $wingetPath"
    }
    catch {
        throw "Failed to download winget: $_"
    }
    
    # Install dependencies first, then winget
    Write-Step "Installing dependencies..."
    
    try {
        Write-Info "Installing VCLibs..."
        Add-AppxPackage -Path $vcLibsPath
        Write-Pass "Installed VCLibs"
    }
    catch {
        Write-Host "[WARN] VCLibs install failed (may already be present): $_" -ForegroundColor Yellow
    }
    
    try {
        Write-Info "Installing UI.Xaml..."
        Add-AppxPackage -Path $uiXamlAppx
        Write-Pass "Installed UI.Xaml"
    }
    catch {
        Write-Host "[WARN] UI.Xaml install failed (may already be present): $_" -ForegroundColor Yellow
    }
    
    Write-Step "Installing winget..."
    try {
        Add-AppxPackage -Path $wingetPath
        Write-Pass "Installed winget"
    }
    catch {
        throw "Failed to install winget: $_"
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
            return
        }
        throw "winget installation completed but winget command is still not available"
    }
    
    Write-Pass "winget bootstrap complete: $($wingetCmd.Source)"
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
