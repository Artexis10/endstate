<#
.SYNOPSIS
    Sandbox-side script for module validation.

.DESCRIPTION
    Runs inside Windows Sandbox to validate a module's capture/restore cycle:
    1. Install app via winget
    2. Run seed script (if present)
    3. Capture config files defined in module
    4. Wipe (move captured files to .bak)
    5. Restore using module restore definitions
    6. Verify using module verify definitions

    Writes artifacts to mapped output folder and signals completion via DONE.txt/ERROR.txt.

.PARAMETER AppId
    Module app ID (folder name under modules/apps/).

.PARAMETER WingetId
    Winget package ID to install.

.PARAMETER OutputDir
    Mapped folder path for output artifacts.

.PARAMETER NoSeed
    Skip seed script even if present.

.EXAMPLE
    .\sandbox-validate.ps1 -AppId git -WingetId "Git.Git" -OutputDir "C:\Endstate\sandbox-tests\validation\git\20250125"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$AppId,
    
    [Parameter(Mandatory = $true)]
    [string]$WingetId,
    
    [Parameter(Mandatory = $true)]
    [string]$OutputDir,
    
    [Parameter(Mandatory = $false)]
    [switch]$NoSeed,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerPath,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerArgs,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerExePath
)

$ErrorActionPreference = 'Stop'

# ============================================================================
# PATH SANITIZATION
# ============================================================================
function Remove-SurroundingQuotes {
    <#
    .SYNOPSIS
        Strips surrounding matching quotes (single or double) from a string.
    #>
    param([string]$Value)
    if ([string]::IsNullOrEmpty($Value)) { return $Value }
    $Value = $Value.Trim()
    if ($Value.Length -ge 2) {
        $first = $Value[0]
        $last = $Value[$Value.Length - 1]
        if (($first -eq "'" -and $last -eq "'") -or ($first -eq '"' -and $last -eq '"')) {
            return $Value.Substring(1, $Value.Length - 2)
        }
    }
    return $Value
}

# Sanitize path arguments that may arrive with literal quotes from cmd.exe -> powershell chain
$OutputDir = Remove-SurroundingQuotes $OutputDir
$AppId = Remove-SurroundingQuotes $AppId
$WingetId = Remove-SurroundingQuotes $WingetId
if ($InstallerPath) { $InstallerPath = Remove-SurroundingQuotes $InstallerPath }
if ($InstallerExePath) { $InstallerExePath = Remove-SurroundingQuotes $InstallerExePath }

# ============================================================================
# IMMEDIATE STARTUP MARKER
# ============================================================================
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

$startedFile = Join-Path $OutputDir "STARTED.txt"
"Script started at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')`nPID: $PID`nAppId: $AppId`nWingetId: $WingetId" | Out-File -FilePath $startedFile -Encoding UTF8

$script:StepFile = Join-Path $OutputDir "STEP.txt"
$script:ErrorFile = Join-Path $OutputDir "ERROR.txt"
$script:ResultFile = Join-Path $OutputDir "result.json"

# Ensure TLS 1.2+
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# Resolve paths
$script:HarnessRoot = $PSScriptRoot
$script:RepoRoot = (Resolve-Path (Join-Path $script:HarnessRoot "..\..")).Path
$script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"
$script:EngineDir = Join-Path $script:RepoRoot "engine"

# ============================================================================
# HELPER FUNCTIONS
# ============================================================================

function Write-Step {
    param([string]$Message)
    Write-Host "[STEP] $Message" -ForegroundColor Yellow
    if ($script:StepFile) {
        "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') $Message" | Out-File -FilePath $script:StepFile -Encoding UTF8
    }
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Write-FatalError {
    param(
        [string]$Stage,
        [string]$Message,
        [string]$Details = ""
    )
    
    $content = @"
FATAL ERROR at stage: $Stage
Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Message: $Message

Details:
$Details
"@
    
    if ($script:ErrorFile) {
        $content | Out-File -FilePath $script:ErrorFile -Encoding UTF8
    }
    
    # Write failure result
    $result = @{
        status = "FAIL"
        appId = $AppId
        wingetId = $WingetId
        failedStage = $Stage
        failReason = $Message
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    }
    $result | ConvertTo-Json -Depth 5 | Out-File -FilePath $script:ResultFile -Encoding UTF8
    
    Write-Host "[FATAL] $Message" -ForegroundColor Red
    exit 1
}

function Write-Result {
    param([hashtable]$Result)
    $Result | ConvertTo-Json -Depth 5 | Out-File -FilePath $script:ResultFile -Encoding UTF8
}

# Load manifest.ps1 for JSONC parsing
$manifestModule = Join-Path $script:EngineDir "manifest.ps1"
if (Test-Path $manifestModule) {
    . $manifestModule
}

function Read-ModuleJsonc {
    param([string]$Path)
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    if (Get-Command Read-JsoncFile -ErrorAction SilentlyContinue) {
        return Read-JsoncFile -Path $Path
    }
    
    # Fallback: strip comments and parse
    $content = Get-Content -Path $Path -Raw
    $content = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
    return $content | ConvertFrom-Json -AsHashtable
}

function Expand-ConfigPath {
    param([string]$Path)
    
    # Expand ~ to user profile
    if ($Path.StartsWith("~/") -or $Path.StartsWith("~\")) {
        $Path = Join-Path $env:USERPROFILE $Path.Substring(2)
    } elseif ($Path -eq "~") {
        $Path = $env:USERPROFILE
    }
    
    # Expand environment variables
    $Path = [Environment]::ExpandEnvironmentVariables($Path)
    
    return $Path
}

# ============================================================================
# WINGET BOOTSTRAP FUNCTION
# ============================================================================

function Ensure-Winget {
    <#
    .SYNOPSIS
        Ensures winget is available, attempting bootstrap if missing.
    .OUTPUTS
        Returns $true if winget is available, $false otherwise.
    #>
    
    # Check if winget already exists
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Info "Winget is available: $($wingetCmd.Source)"
        return $true
    }
    
    Write-Info "Winget not found, attempting bootstrap..."
    $bootstrapLog = Join-Path $OutputDir "winget-bootstrap.log"
    $logContent = @("Bootstrap started at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    $logContent += "OS Version: $([System.Environment]::OSVersion.VersionString)"
    $logContent += "PowerShell Version: $($PSVersionTable.PSVersion)"
    
    # Bootstrap: Download and install App Installer bundle with all dependencies
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    $logContent += "Temp directory: $tempDir"
    
    # Step 1: Download Windows App Runtime 1.8 (required by modern winget)
    # Using the direct MSIX package URL for x64 framework
    $appRuntimeUrl = "https://aka.ms/windowsappsdk/1.8/1.8.250410000/windowsappruntimeinstall-x64.exe"
    $appRuntimePath = Join-Path $tempDir "windowsappruntimeinstall-x64.exe"
    Write-Info "Downloading Windows App Runtime 1.8..."
    $logContent += ""
    $logContent += "Step 1: Download Windows App Runtime 1.8"
    $logContent += "URL: $appRuntimeUrl"
    
    try {
        Invoke-WebRequest -Uri $appRuntimeUrl -OutFile $appRuntimePath -UseBasicParsing -ErrorAction Stop
        $fileSize = (Get-Item $appRuntimePath).Length
        $logContent += "SUCCESS: Downloaded to $appRuntimePath ($fileSize bytes)"
        Write-Info "Downloaded Windows App Runtime ($fileSize bytes)"
    } catch {
        $logContent += "FAILED: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        $logContent += "Stack: $($_.ScriptStackTrace)"
        $logContent += "(Continuing - will attempt App Installer install anyway)"
        Write-Info "Windows App Runtime download failed (continuing): $($_.Exception.Message)"
    }
    
    # Step 2: Install Windows App Runtime
    if (Test-Path $appRuntimePath) {
        Write-Info "Installing Windows App Runtime 1.8..."
        $logContent += ""
        $logContent += "Step 2: Install Windows App Runtime 1.8"
        try {
            $proc = Start-Process -FilePath $appRuntimePath -ArgumentList "--quiet" -Wait -PassThru -ErrorAction Stop
            if ($proc.ExitCode -eq 0) {
                $logContent += "SUCCESS: Windows App Runtime installed (exit code 0)"
                Write-Info "Windows App Runtime installed"
            } else {
                $logContent += "WARNING: Windows App Runtime installer exited with code $($proc.ExitCode)"
                $logContent += "(Continuing - runtime may already be present or partially installed)"
                Write-Info "Windows App Runtime install warning (exit code $($proc.ExitCode))"
            }
        } catch {
            $logContent += "WARNING: $($_.Exception.GetType().Name): $($_.Exception.Message)"
            $logContent += "(Continuing - will attempt App Installer install anyway)"
            Write-Info "Windows App Runtime install warning: $($_.Exception.Message)"
        }
    } else {
        $logContent += ""
        $logContent += "Step 2: Install Windows App Runtime 1.8"
        $logContent += "SKIPPED: Installer not downloaded"
    }
    
    # Step 3: Download VCLibs dependency
    $vclibsUrl = "https://aka.ms/Microsoft.VCLibs.x64.14.00.Desktop.appx"
    $vclibsPath = Join-Path $tempDir "Microsoft.VCLibs.x64.appx"
    Write-Info "Downloading VCLibs dependency..."
    $logContent += ""
    $logContent += "Step 3: Download VCLibs"
    $logContent += "URL: $vclibsUrl"
    
    try {
        Invoke-WebRequest -Uri $vclibsUrl -OutFile $vclibsPath -UseBasicParsing -ErrorAction Stop
        $logContent += "SUCCESS: Downloaded to $vclibsPath"
        Write-Info "Downloaded VCLibs"
    } catch {
        $logContent += "FAILED: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        $logContent += "Stack: $($_.ScriptStackTrace)"
        $logContent | Out-File -FilePath $bootstrapLog -Encoding UTF8
        Write-Info "VCLibs download failed - see winget-bootstrap.log"
        return $false
    }
    
    # Step 4: Install VCLibs
    Write-Info "Installing VCLibs..."
    $logContent += ""
    $logContent += "Step 4: Install VCLibs"
    try {
        Add-AppxPackage -Path $vclibsPath -ErrorAction Stop
        $logContent += "SUCCESS: VCLibs installed"
        Write-Info "VCLibs installed"
    } catch {
        # VCLibs may already be installed or have a newer version
        $logContent += "WARNING: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        $logContent += "(Continuing - VCLibs may already be present)"
        Write-Info "VCLibs install warning (may already exist): $($_.Exception.Message)"
    }
    
    # Step 5: Download App Installer bundle
    $downloadUrl = "https://aka.ms/getwinget"
    $msixBundlePath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    Write-Info "Downloading App Installer from aka.ms/getwinget..."
    $logContent += ""
    $logContent += "Step 5: Download App Installer"
    $logContent += "URL: $downloadUrl"
    
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $msixBundlePath -UseBasicParsing -ErrorAction Stop
        $fileSize = (Get-Item $msixBundlePath).Length
        $logContent += "SUCCESS: Downloaded to $msixBundlePath ($fileSize bytes)"
        Write-Info "Downloaded App Installer bundle ($fileSize bytes)"
    } catch {
        $logContent += "FAILED: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        $logContent += "Stack: $($_.ScriptStackTrace)"
        $logContent | Out-File -FilePath $bootstrapLog -Encoding UTF8
        Write-Info "App Installer download failed - see winget-bootstrap.log"
        return $false
    }
    
    # Step 6: Install App Installer
    Write-Info "Installing App Installer..."
    $logContent += ""
    $logContent += "Step 6: Install App Installer"
    try {
        Add-AppxPackage -Path $msixBundlePath -ErrorAction Stop
        $logContent += "SUCCESS: App Installer package installed"
        Write-Info "App Installer installed"
    } catch {
        $logContent += "FAILED: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        $logContent += "Stack: $($_.ScriptStackTrace)"
        $logContent | Out-File -FilePath $bootstrapLog -Encoding UTF8
        Write-Info "App Installer install failed - see winget-bootstrap.log"
        return $false
    }
    
    # Step 7: Verify winget is now available
    Start-Sleep -Seconds 3
    $logContent += ""
    $logContent += "Step 7: Verify winget availability"
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        $logContent += "SUCCESS: Winget is now available at $($wingetCmd.Source)"
        $logContent += ""
        $logContent += "Bootstrap completed at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') - SUCCESS"
        $logContent | Out-File -FilePath $bootstrapLog -Encoding UTF8
        Write-Pass "Winget bootstrap succeeded"
        return $true
    } else {
        $logContent += "FAILED: Winget command not found after installation"
        $logContent += "Checking installed packages..."
        try {
            $appxPackages = Get-AppxPackage -Name "*DesktopAppInstaller*" | Select-Object Name, Version, Status
            $logContent += ($appxPackages | Format-Table -AutoSize | Out-String)
        } catch {
            $logContent += "Could not enumerate AppX packages: $($_.Exception.Message)"
        }
        $logContent += ""
        $logContent += "Bootstrap completed at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') - FAILED"
        $logContent | Out-File -FilePath $bootstrapLog -Encoding UTF8
        Write-Info "Winget bootstrap failed - see winget-bootstrap.log"
        return $false
    }
}

# ============================================================================
# MAIN VALIDATION FLOW
# ============================================================================

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " Sandbox Validation: $AppId" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# Track results
$result = @{
    status = "FAIL"
    appId = $AppId
    wingetId = $WingetId
    seedRan = $false
    capturedFiles = 0
    wipedFiles = 0
    restoredFiles = 0
    verifyPass = 0
    verifyTotal = 0
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
}

# ============================================================================
# STAGE 1: Load Module
# ============================================================================
Write-Step "Loading module definition..."

$moduleDir = Join-Path $script:ModulesDir $AppId
$modulePath = Join-Path $moduleDir "module.jsonc"

if (-not (Test-Path $modulePath)) {
    Write-FatalError -Stage "load-module" -Message "Module not found: $modulePath"
}

$module = Read-ModuleJsonc -Path $modulePath
if (-not $module) {
    Write-FatalError -Stage "load-module" -Message "Failed to parse module: $modulePath"
}

Write-Pass "Loaded module: $($module.displayName)"

# ============================================================================
# STAGE 2: Install App via Winget (or Offline Fallback)
# ============================================================================
Write-Step "Installing $WingetId..."

# Try to ensure winget is available (Strategy A: Bootstrap)
$wingetAvailable = Ensure-Winget

if ($wingetAvailable) {
    # Install via winget
    Write-Info "Installing via winget..."
    try {
        $installOutput = & winget install --id $WingetId --accept-source-agreements --accept-package-agreements --silent 2>&1
        $installExitCode = $LASTEXITCODE
        
        # Log install output
        $installLogPath = Join-Path $OutputDir "install.log"
        $installOutput | Out-File -FilePath $installLogPath -Encoding UTF8
        
        if ($installExitCode -ne 0) {
            # Check if already installed (exit code 0x8A150061 or similar)
            if ($installOutput -match "already installed" -or $installExitCode -eq -1978335135) {
                Write-Info "App already installed, continuing..."
            } else {
                Write-FatalError -Stage "install" -Message "Winget install failed with exit code $installExitCode" -Details ($installOutput | Out-String)
            }
        } else {
            Write-Pass "Installed via winget: $WingetId"
        }
    } catch {
        Write-FatalError -Stage "install" -Message "Winget install threw exception: $_"
    }
} else {
    # Strategy B: Offline Installer Fallback
    Write-Info "Winget unavailable, checking for offline installer fallback..."
    
    if ($InstallerPath) {
        Write-Step "Using offline installer fallback..."
        
        # Resolve installer path (relative to repo root in sandbox)
        $fullInstallerPath = if ([System.IO.Path]::IsPathRooted($InstallerPath)) {
            $InstallerPath
        } else {
            Join-Path $script:RepoRoot $InstallerPath
        }
        
        if (-not (Test-Path $fullInstallerPath)) {
            Write-FatalError -Stage "install" -Message "Offline installer not found: $fullInstallerPath" -Details "Ensure the installer file exists at the specified path."
        }
        
        Write-Info "Installer: $fullInstallerPath"
        Write-Info "Args: $InstallerArgs"
        
        # Log offline install attempt
        $installLogPath = Join-Path $OutputDir "install.log"
        "Offline install started at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" | Out-File -FilePath $installLogPath -Encoding UTF8
        "Installer: $fullInstallerPath" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
        "Args: $InstallerArgs" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
        
        try {
            # Determine installer type and execute
            $extension = [System.IO.Path]::GetExtension($fullInstallerPath).ToLower()
            $isAppxType = $extension -in @(".msix", ".msixbundle", ".appx", ".appxbundle")
            $installOutput = $null
            $installExitCode = 0
            
            "Extension: $extension (isAppxType: $isAppxType)" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
            
            if ($extension -eq ".msi") {
                "Executing: msiexec /i `"$fullInstallerPath`" $InstallerArgs" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
                $installOutput = & msiexec /i "$fullInstallerPath" $InstallerArgs 2>&1
                $installExitCode = $LASTEXITCODE
            } elseif ($extension -eq ".exe") {
                "Executing: `"$fullInstallerPath`" $InstallerArgs" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
                $installOutput = & "$fullInstallerPath" $InstallerArgs 2>&1
                $installExitCode = $LASTEXITCODE
            } elseif ($isAppxType) {
                "Executing: Add-AppxPackage -Path `"$fullInstallerPath`"" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
                # For AppX packages, success = no exception thrown; don't rely on $LASTEXITCODE
                Add-AppxPackage -Path $fullInstallerPath -ErrorAction Stop
                $installOutput = "AppX package installed successfully"
                $installExitCode = 0
            } else {
                Write-FatalError -Stage "install" -Message "Unsupported installer type: $extension"
            }
            
            if ($installOutput) {
                $installOutput | Out-File -FilePath $installLogPath -Append -Encoding UTF8
            }
            "Exit code: $installExitCode" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
            
            # Only check exit code for exe/msi (AppX success is determined by no exception)
            if (-not $isAppxType -and $installExitCode -ne 0) {
                Write-FatalError -Stage "install" -Message "Offline installer failed with exit code $installExitCode" -Details ($installOutput | Out-String)
            }
            
            # Verify installation if exePath provided
            if ($InstallerExePath) {
                Start-Sleep -Seconds 2
                $expandedExePath = Expand-ConfigPath -Path $InstallerExePath
                if (Test-Path $expandedExePath) {
                    Write-Pass "Installed via offline installer (verified: $expandedExePath)"
                } else {
                    Write-Info "Warning: Could not verify install at $expandedExePath"
                    Write-Pass "Installed via offline installer (unverified)"
                }
            } else {
                Write-Pass "Installed via offline installer"
            }
        } catch {
            $errorDetail = "$($_.Exception.GetType().Name): $($_.Exception.Message)"
            "EXCEPTION: $errorDetail" | Out-File -FilePath $installLogPath -Append -Encoding UTF8
            Write-FatalError -Stage "install" -Message "Offline installer threw exception" -Details $errorDetail
        }
    } else {
        # No fallback available - provide actionable error
        $errorMsg = @"
Winget bootstrap failed and no offline installer fallback is configured.

To fix this, add installer metadata to sandbox-tests/golden-queue.jsonc:

{
  "appId": "$AppId",
  "wingetId": "$WingetId",
  "installer": {
    "path": "sandbox-tests/installers/<installer-file>",
    "silentArgs": "/S or /quiet or appropriate args",
    "exePath": "C:\\Path\\To\\installed\\app.exe"
  }
}

Then place the installer file in sandbox-tests/installers/
"@
        Write-FatalError -Stage "install" -Message "Winget unavailable and no offline installer configured for $AppId" -Details $errorMsg
    }
}

# Refresh PATH
$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

# ============================================================================
# STAGE 3: Run Seed Script (if present and enabled)
# ============================================================================
$seedScript = Join-Path $moduleDir "seed.ps1"
$hasSeed = Test-Path $seedScript

if ($hasSeed -and -not $NoSeed) {
    Write-Step "Running seed script..."
    
    try {
        $seedOutput = & $seedScript 2>&1
        $seedExitCode = $LASTEXITCODE
        
        # Log seed output
        $seedLogPath = Join-Path $OutputDir "seed.log"
        $seedOutput | Out-File -FilePath $seedLogPath -Encoding UTF8
        
        if ($seedExitCode -ne 0) {
            Write-FatalError -Stage "seed" -Message "Seed script failed with exit code $seedExitCode" -Details ($seedOutput | Out-String)
        }
        
        $result.seedRan = $true
        Write-Pass "Seed script completed"
    } catch {
        Write-FatalError -Stage "seed" -Message "Seed script threw exception: $_"
    }
} else {
    Write-Info "Seed script skipped (NoSeed=$NoSeed, hasSeed=$hasSeed)"
}

# ============================================================================
# STAGE 4: Capture Config Files
# ============================================================================
Write-Step "Capturing config files..."

$captureDir = Join-Path $OutputDir "capture"
if (-not (Test-Path $captureDir)) {
    New-Item -ItemType Directory -Path $captureDir -Force | Out-Null
}

$capturedFiles = @()

if ($module.capture -and $module.capture.files) {
    foreach ($fileEntry in $module.capture.files) {
        $sourcePath = Expand-ConfigPath -Path $fileEntry.source
        $destPath = Join-Path $captureDir $fileEntry.dest
        
        if (Test-Path $sourcePath) {
            # Ensure dest directory exists
            $destDir = Split-Path -Parent $destPath
            if (-not (Test-Path $destDir)) {
                New-Item -ItemType Directory -Path $destDir -Force | Out-Null
            }
            
            # Copy file or directory
            if ((Get-Item $sourcePath).PSIsContainer) {
                Copy-Item -Path $sourcePath -Destination $destPath -Recurse -Force
            } else {
                Copy-Item -Path $sourcePath -Destination $destPath -Force
            }
            
            $capturedFiles += @{
                source = $sourcePath
                dest = $destPath
                originalSource = $fileEntry.source
            }
            Write-Info "Captured: $sourcePath"
        } elseif (-not $fileEntry.optional) {
            Write-FatalError -Stage "capture" -Message "Required capture source not found: $sourcePath"
        } else {
            Write-Info "Skipped (optional, not found): $sourcePath"
        }
    }
}

$result.capturedFiles = $capturedFiles.Count
Write-Pass "Captured $($capturedFiles.Count) files/directories"

# Save capture manifest
$captureManifest = @{
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    files = $capturedFiles
}
$captureManifest | ConvertTo-Json -Depth 5 | Out-File -FilePath (Join-Path $OutputDir "capture-manifest.json") -Encoding UTF8

# ============================================================================
# STAGE 5: Wipe (Simulate Loss)
# ============================================================================
Write-Step "Wiping config files (simulating loss)..."

$wipeDir = Join-Path $OutputDir "wipe-backup"
if (-not (Test-Path $wipeDir)) {
    New-Item -ItemType Directory -Path $wipeDir -Force | Out-Null
}

$wipedFiles = @()

foreach ($captured in $capturedFiles) {
    $sourcePath = $captured.source
    
    if (Test-Path $sourcePath) {
        # Move to backup location (don't delete permanently)
        $backupPath = Join-Path $wipeDir (Split-Path -Leaf $sourcePath)
        $counter = 0
        while (Test-Path $backupPath) {
            $counter++
            $backupPath = Join-Path $wipeDir "$(Split-Path -Leaf $sourcePath).$counter"
        }
        
        Move-Item -Path $sourcePath -Destination $backupPath -Force
        $wipedFiles += @{
            original = $sourcePath
            backup = $backupPath
        }
        Write-Info "Wiped: $sourcePath -> $backupPath"
    }
}

$result.wipedFiles = $wipedFiles.Count
Write-Pass "Wiped $($wipedFiles.Count) files/directories"

# Save wipe manifest
$wipeManifest = @{
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    files = $wipedFiles
}
$wipeManifest | ConvertTo-Json -Depth 5 | Out-File -FilePath (Join-Path $OutputDir "wipe-manifest.json") -Encoding UTF8

# ============================================================================
# STAGE 6: Restore
# ============================================================================
Write-Step "Restoring config files..."

$restoredFiles = @()

if ($module.restore -and $module.restore.Count -gt 0) {
    foreach ($restoreItem in $module.restore) {
        $restoreType = if ($restoreItem.type) { $restoreItem.type } else { "copy" }
        
        if ($restoreType -eq "copy") {
            # Resolve source path (relative to module dir or captured payload)
            $sourcePath = $restoreItem.source
            if ($sourcePath.StartsWith("./")) {
                # Relative to module directory - but we use captured files
                # Map ./payload/apps/git/.gitconfig to capture/apps/git/.gitconfig
                $relativePart = $sourcePath.Substring(2)
                if ($relativePart.StartsWith("payload/")) {
                    $relativePart = $relativePart.Substring(8)  # Remove "payload/"
                }
                $sourcePath = Join-Path $captureDir $relativePart
            }
            
            $targetPath = Expand-ConfigPath -Path $restoreItem.target
            
            if (Test-Path $sourcePath) {
                # Ensure target directory exists
                $targetDir = Split-Path -Parent $targetPath
                if ($targetDir -and -not (Test-Path $targetDir)) {
                    New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
                }
                
                # Copy
                if ((Get-Item $sourcePath).PSIsContainer) {
                    Copy-Item -Path $sourcePath -Destination $targetPath -Recurse -Force
                } else {
                    Copy-Item -Path $sourcePath -Destination $targetPath -Force
                }
                
                $restoredFiles += @{
                    source = $sourcePath
                    target = $targetPath
                }
                Write-Info "Restored: $sourcePath -> $targetPath"
            } elseif (-not $restoreItem.optional) {
                Write-FatalError -Stage "restore" -Message "Required restore source not found: $sourcePath"
            } else {
                Write-Info "Skipped (optional, not found): $sourcePath"
            }
        } else {
            Write-Info "Skipped restore type '$restoreType' (not implemented in validation)"
        }
    }
}

$result.restoredFiles = $restoredFiles.Count
Write-Pass "Restored $($restoredFiles.Count) files/directories"

# Save restore manifest
$restoreManifest = @{
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    files = $restoredFiles
}
$restoreManifest | ConvertTo-Json -Depth 5 | Out-File -FilePath (Join-Path $OutputDir "restore-manifest.json") -Encoding UTF8

# ============================================================================
# STAGE 7: Verify
# ============================================================================
Write-Step "Running verification checks..."

# Load verifiers
$fileExistsVerifier = Join-Path $script:RepoRoot "verifiers\file-exists.ps1"
$commandExistsVerifier = Join-Path $script:RepoRoot "verifiers\command-exists.ps1"
$registryKeyExistsVerifier = Join-Path $script:RepoRoot "verifiers\registry-key-exists.ps1"

if (Test-Path $fileExistsVerifier) { . $fileExistsVerifier }
if (Test-Path $commandExistsVerifier) { . $commandExistsVerifier }
if (Test-Path $registryKeyExistsVerifier) { . $registryKeyExistsVerifier }

$verifyResults = @()
$verifyPass = 0
$verifyFail = 0

if ($module.verify -and $module.verify.Count -gt 0) {
    foreach ($verifyItem in $module.verify) {
        $verifyResult = @{
            type = $verifyItem.type
            status = "fail"
            message = ""
        }
        
        switch ($verifyItem.type) {
            "file-exists" {
                $checkPath = Expand-ConfigPath -Path $verifyItem.path
                $verifyResult.path = $checkPath
                
                if (Get-Command Test-FileExistsVerifier -ErrorAction SilentlyContinue) {
                    $result = Test-FileExistsVerifier -Path $checkPath
                    $verifyResult.status = if ($result.Success) { "pass" } else { "fail" }
                    $verifyResult.message = $result.Message
                } else {
                    # Fallback
                    if (Test-Path $checkPath) {
                        $verifyResult.status = "pass"
                        $verifyResult.message = "File exists: $checkPath"
                    } else {
                        $verifyResult.status = "fail"
                        $verifyResult.message = "File not found: $checkPath"
                    }
                }
            }
            "command-exists" {
                $verifyResult.command = $verifyItem.command
                
                if (Get-Command Test-CommandExistsVerifier -ErrorAction SilentlyContinue) {
                    $result = Test-CommandExistsVerifier -Command $verifyItem.command
                    $verifyResult.status = if ($result.Success) { "pass" } else { "fail" }
                    $verifyResult.message = $result.Message
                } else {
                    # Fallback
                    $cmd = Get-Command $verifyItem.command -ErrorAction SilentlyContinue
                    if ($cmd) {
                        $verifyResult.status = "pass"
                        $verifyResult.message = "Command exists: $($verifyItem.command)"
                    } else {
                        $verifyResult.status = "fail"
                        $verifyResult.message = "Command not found: $($verifyItem.command)"
                    }
                }
            }
            "registry-key-exists" {
                $verifyResult.path = $verifyItem.path
                $verifyResult.name = $verifyItem.name
                
                if (Get-Command Test-RegistryKeyExistsVerifier -ErrorAction SilentlyContinue) {
                    $result = Test-RegistryKeyExistsVerifier -Path $verifyItem.path -Name $verifyItem.name
                    $verifyResult.status = if ($result.Success) { "pass" } else { "fail" }
                    $verifyResult.message = $result.Message
                } else {
                    $verifyResult.status = "skip"
                    $verifyResult.message = "Registry verifier not available"
                }
            }
            default {
                $verifyResult.status = "skip"
                $verifyResult.message = "Unknown verify type: $($verifyItem.type)"
            }
        }
        
        $verifyResults += $verifyResult
        
        if ($verifyResult.status -eq "pass") {
            $verifyPass++
            Write-Pass "VERIFY: $($verifyResult.message)"
        } elseif ($verifyResult.status -eq "fail") {
            $verifyFail++
            Write-Host "[FAIL] VERIFY: $($verifyResult.message)" -ForegroundColor Red
        } else {
            Write-Info "VERIFY: $($verifyResult.message)"
        }
    }
}

$result.verifyPass = $verifyPass
$result.verifyTotal = $verifyResults.Count

# Save verify results
$verifyManifest = @{
    timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    results = $verifyResults
    summary = @{
        pass = $verifyPass
        fail = $verifyFail
        total = $verifyResults.Count
    }
}
$verifyManifest | ConvertTo-Json -Depth 5 | Out-File -FilePath (Join-Path $OutputDir "verify-manifest.json") -Encoding UTF8

# ============================================================================
# FINAL RESULT
# ============================================================================
Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan

if ($verifyFail -eq 0) {
    $result.status = "PASS"
    Write-Host " VALIDATION PASSED" -ForegroundColor Green
} else {
    $result.status = "FAIL"
    $result.failedStage = "verify"
    $result.failReason = "$verifyFail verification(s) failed"
    Write-Host " VALIDATION FAILED" -ForegroundColor Red
}

Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Host "  App:      $AppId" -ForegroundColor White
Write-Host "  Winget:   $WingetId" -ForegroundColor White
Write-Host "  Seed:     $($result.seedRan)" -ForegroundColor White
Write-Host "  Captured: $($result.capturedFiles) files" -ForegroundColor White
Write-Host "  Wiped:    $($result.wipedFiles) files" -ForegroundColor White
Write-Host "  Restored: $($result.restoredFiles) files" -ForegroundColor White
Write-Host "  Verified: $verifyPass/$($verifyResults.Count) checks passed" -ForegroundColor White
Write-Host ""

# Write result file
Write-Result -Result $result

# Write DONE sentinel
$doneContent = @"
Validation completed at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Status: $($result.status)
App: $AppId
Winget: $WingetId
"@
$doneContent | Out-File -FilePath (Join-Path $OutputDir "DONE.txt") -Encoding UTF8

Write-Host "Artifacts written to: $OutputDir" -ForegroundColor Green
Write-Host ""

if ($result.status -eq "PASS") {
    exit 0
} else {
    exit 1
}
