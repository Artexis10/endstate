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
$script:BootstrapLog = Join-Path $OutputDir "winget-bootstrap.log"
$script:CurrentStage = "init"

# Create bootstrap log immediately
"[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] Bootstrap log initialized" | Out-File -FilePath $script:BootstrapLog -Encoding UTF8

function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
    "[$timestamp] $Message" | Out-File -FilePath $script:BootstrapLog -Append -Encoding UTF8
}

function Set-Stage {
    param([string]$Stage)
    $script:CurrentStage = $Stage
    $timestamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
    "$timestamp stage=$Stage" | Out-File -FilePath $script:StepFile -Encoding UTF8
    Write-Log "STAGE: $Stage"
}

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
    .DESCRIPTION
        Uses the Windows App SDK redistributable ZIP (MSIX packages) instead of the
        EXE bootstrapper to avoid download corruption issues in Windows Sandbox.
    .OUTPUTS
        Returns $true if winget is available, $false otherwise.
    #>
    
    # Minimum file sizes to detect truncated/corrupted downloads
    $script:MinFileSizes = @{
        'redist-zip'   = 50MB    # Windows App Runtime redist ZIP should be ~80MB+
        'vclibs'       = 500KB   # VCLibs appx should be ~700KB+
        'appinstaller' = 10MB    # App Installer msixbundle should be ~60MB+
    }
    
    # =========================================================================
    # Robust Download Helper - handles redirects, TLS, content validation
    # =========================================================================
    function Invoke-RobustDownload {
        <#
        .SYNOPSIS
            Downloads a file with robust redirect handling, TLS config, and content validation.
        .PARAMETER Url
            The URL to download from.
        .PARAMETER OutFile
            The local file path to save to.
        .PARAMETER FileType
            The type key for minimum size lookup (redist-zip, vclibs, appinstaller).
        .PARAMETER ExpectBinary
            If true, validates the file starts with PK signature (ZIP/MSIX).
        .OUTPUTS
            Returns $true if download succeeded and passed validation, $false otherwise.
            On failure, writes detailed diagnostics to log.
        #>
        param(
            [Parameter(Mandatory)]
            [string]$Url,
            [Parameter(Mandatory)]
            [string]$OutFile,
            [Parameter(Mandatory)]
            [string]$FileType,
            [switch]$ExpectBinary
        )
        
        Write-Log "--- Invoke-RobustDownload ---"
        Write-Log "  URL: $Url"
        Write-Log "  OutFile: $OutFile"
        Write-Log "  FileType: $FileType"
        Write-Log "  ExpectBinary: $ExpectBinary"
        
        # Ensure TLS 1.2+ is enabled (TLS 1.3 may not be available on all systems)
        try {
            $tlsProtocols = [Net.SecurityProtocolType]::Tls12
            if ([Enum]::IsDefined([Net.SecurityProtocolType], 'Tls13')) {
                $tlsProtocols = $tlsProtocols -bor [Net.SecurityProtocolType]::Tls13
            }
            [Net.ServicePointManager]::SecurityProtocol = $tlsProtocols
            Write-Log "  TLS protocols set: $([Net.ServicePointManager]::SecurityProtocol)"
        } catch {
            Write-Log "  WARNING: Could not set TLS protocols: $($_.Exception.Message)"
        }
        
        $downloadInfo = @{
            OriginalUrl = $Url
            FinalUrl = $null
            StatusCode = $null
            ContentType = $null
            ContentLength = $null
            ActualSize = $null
            FirstBytes = $null
            FirstText = $null
            IsValidBinary = $false
            Error = $null
        }
        
        try {
            # Use Invoke-WebRequest with explicit redirect handling
            # -MaximumRedirection 10 ensures we follow redirects
            # -UseBasicParsing avoids IE engine dependency
            # -TimeoutSec 600 (10 min) - sandbox downloads can be very slow
            $response = Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -PassThru -MaximumRedirection 10 -TimeoutSec 600 -ErrorAction Stop
            
            # Capture response metadata
            $downloadInfo.StatusCode = $response.StatusCode
            $downloadInfo.FinalUrl = if ($response.BaseResponse.ResponseUri) { 
                $response.BaseResponse.ResponseUri.ToString() 
            } else { 
                $Url 
            }
            
            # Try to get headers
            if ($response.Headers) {
                $downloadInfo.ContentType = $response.Headers['Content-Type']
                $downloadInfo.ContentLength = $response.Headers['Content-Length']
            }
            
            Write-Log "  HTTP Status: $($downloadInfo.StatusCode)"
            Write-Log "  Final URL: $($downloadInfo.FinalUrl)"
            Write-Log "  Content-Type: $($downloadInfo.ContentType)"
            Write-Log "  Content-Length header: $($downloadInfo.ContentLength)"
            
        } catch {
            $downloadInfo.Error = "$($_.Exception.GetType().Name): $($_.Exception.Message)"
            Write-Log "  DOWNLOAD FAILED: $($downloadInfo.Error)"
            Write-Log "  Stack: $($_.ScriptStackTrace)"
            return $false
        }
        
        # Verify file exists and get actual size
        if (-not (Test-Path $OutFile)) {
            Write-Log "  FAIL: Output file does not exist after download"
            return $false
        }
        
        $fileInfo = Get-Item $OutFile
        $downloadInfo.ActualSize = $fileInfo.Length
        Write-Log "  Actual file size: $($downloadInfo.ActualSize) bytes ($([math]::Round($downloadInfo.ActualSize / 1MB, 2)) MB)"
        
        # Read first bytes for signature validation
        try {
            $stream = [System.IO.File]::OpenRead($OutFile)
            $buffer = New-Object byte[] 4
            $bytesRead = $stream.Read($buffer, 0, 4)
            $stream.Close()
            
            if ($bytesRead -ge 2) {
                $downloadInfo.FirstBytes = [BitConverter]::ToString($buffer[0..($bytesRead-1)])
                Write-Log "  First bytes (hex): $($downloadInfo.FirstBytes)"
                
                # Check for PK signature (ZIP/MSIX/APPX)
                if ($buffer[0] -eq 0x50 -and $buffer[1] -eq 0x4B) {
                    $downloadInfo.IsValidBinary = $true
                    Write-Log "  Signature: PK (valid ZIP/MSIX)"
                } else {
                    Write-Log "  Signature: NOT PK - may be HTML/text"
                }
            }
        } catch {
            Write-Log "  WARNING: Could not read first bytes: $($_.Exception.Message)"
        }
        
        # If file is small or not valid binary, read first text for diagnostics
        if ($downloadInfo.ActualSize -lt 1MB -or (-not $downloadInfo.IsValidBinary -and $ExpectBinary)) {
            try {
                $textContent = Get-Content -Path $OutFile -Raw -ErrorAction SilentlyContinue
                if ($textContent) {
                    $downloadInfo.FirstText = $textContent.Substring(0, [Math]::Min(500, $textContent.Length))
                    Write-Log "  First 500 chars of content:"
                    Write-Log "  ----"
                    foreach ($line in ($downloadInfo.FirstText -split "`n" | Select-Object -First 10)) {
                        Write-Log "  $line"
                    }
                    Write-Log "  ----"
                }
            } catch {
                Write-Log "  Could not read text content: $($_.Exception.Message)"
            }
        }
        
        # Validate minimum size
        $minSize = $script:MinFileSizes[$FileType]
        if ($minSize -and $downloadInfo.ActualSize -lt $minSize) {
            Write-Log "  INTEGRITY FAIL: File too small"
            Write-Log "    Expected minimum: $([math]::Round($minSize / 1MB, 2)) MB ($minSize bytes)"
            Write-Log "    Actual size: $([math]::Round($downloadInfo.ActualSize / 1MB, 2)) MB ($($downloadInfo.ActualSize) bytes)"
            Write-Log "    Final URL: $($downloadInfo.FinalUrl)"
            Write-Log "    Content-Type: $($downloadInfo.ContentType)"
            return $false
        }
        
        # Validate binary signature if expected
        if ($ExpectBinary -and -not $downloadInfo.IsValidBinary) {
            Write-Log "  INTEGRITY FAIL: Expected ZIP/MSIX but file does not have PK signature"
            Write-Log "    First bytes: $($downloadInfo.FirstBytes)"
            Write-Log "    This is likely an HTML error page or redirect page"
            Write-Log "    Final URL: $($downloadInfo.FinalUrl)"
            Write-Log "    Content-Type: $($downloadInfo.ContentType)"
            return $false
        }
        
        Write-Log "  DOWNLOAD OK: Size=$($downloadInfo.ActualSize) bytes, ValidBinary=$($downloadInfo.IsValidBinary)"
        Write-Log "--- End Invoke-RobustDownload ---"
        return $true
    }
    
    function Get-DownloadDiagnostics {
        <#
        .SYNOPSIS
            Returns a formatted string with download failure diagnostics.
        #>
        param(
            [string]$Url,
            [string]$FilePath,
            [string]$FileType
        )
        
        $diag = @()
        $diag += "URL: $Url"
        
        if (Test-Path $FilePath) {
            $fileInfo = Get-Item $FilePath
            $diag += "File size: $($fileInfo.Length) bytes"
            $diag += "Expected minimum: $($script:MinFileSizes[$FileType]) bytes"
            
            # Read first bytes
            try {
                $stream = [System.IO.File]::OpenRead($FilePath)
                $buffer = New-Object byte[] 4
                $bytesRead = $stream.Read($buffer, 0, 4)
                $stream.Close()
                $diag += "First bytes (hex): $([BitConverter]::ToString($buffer[0..($bytesRead-1)]))"
            } catch { }
            
            # Read first text if small
            if ($fileInfo.Length -lt 1MB) {
                try {
                    $text = Get-Content -Path $FilePath -Raw -ErrorAction SilentlyContinue
                    if ($text) {
                        $snippet = $text.Substring(0, [Math]::Min(200, $text.Length))
                        $diag += "Content preview: $snippet"
                    }
                } catch { }
            }
        } else {
            $diag += "File does not exist"
        }
        
        return ($diag -join "`n")
    }
    
    function Write-DiagnosticPackageListing {
        Write-Log ""
        Write-Log "=== DIAGNOSTIC: Package Listings ==="
        
        Write-Log "--- Microsoft.WindowsAppRuntime packages ---"
        try {
            $runtimePkgs = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime*" -ErrorAction SilentlyContinue
            if ($runtimePkgs) {
                foreach ($pkg in $runtimePkgs) {
                    Write-Log "  $($pkg.Name) v$($pkg.Version) [$($pkg.Architecture)]"
                }
            } else {
                Write-Log "  (none found)"
            }
        } catch {
            Write-Log "  ERROR: $($_.Exception.Message)"
        }
        
        Write-Log "--- Microsoft.DesktopAppInstaller packages ---"
        try {
            $installerPkgs = Get-AppxPackage -Name "Microsoft.DesktopAppInstaller*" -ErrorAction SilentlyContinue
            if ($installerPkgs) {
                foreach ($pkg in $installerPkgs) {
                    Write-Log "  $($pkg.Name) v$($pkg.Version) [$($pkg.Architecture)]"
                }
            } else {
                Write-Log "  (none found)"
            }
        } catch {
            Write-Log "  ERROR: $($_.Exception.Message)"
        }
        
        Write-Log "--- Microsoft.VCLibs packages ---"
        try {
            $vclibsPkgs = Get-AppxPackage -Name "Microsoft.VCLibs*" -ErrorAction SilentlyContinue
            if ($vclibsPkgs) {
                foreach ($pkg in $vclibsPkgs) {
                    Write-Log "  $($pkg.Name) v$($pkg.Version) [$($pkg.Architecture)]"
                }
            } else {
                Write-Log "  (none found)"
            }
        } catch {
            Write-Log "  ERROR: $($_.Exception.Message)"
        }
        
        Write-Log "=== END DIAGNOSTIC ==="
        Write-Log ""
    }
    
    # Check if winget already exists
    Set-Stage "winget-bootstrap:check-existing"
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Log "Winget already available: $($wingetCmd.Source)"
        Write-Info "Winget is available: $($wingetCmd.Source)"
        return $true
    }
    
    Write-Log "Winget not found, starting bootstrap..."
    Write-Log "OS Version: $([System.Environment]::OSVersion.VersionString)"
    Write-Log "PowerShell Version: $($PSVersionTable.PSVersion)"
    Write-Info "Winget not found, attempting bootstrap..."
    
    # Create temp directory for downloads
    $tempDir = Join-Path $env:TEMP "winget-bootstrap"
    if (-not (Test-Path $tempDir)) {
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    }
    Write-Log "Temp directory: $tempDir"
    
    # =========================================================================
    # STEP 1: Download winget dependencies from GitHub (includes Windows App Runtime)
    # =========================================================================
    Set-Stage "winget-bootstrap:download-deps"
    
    # Use GitHub direct URLs - aka.ms URLs redirect to Bing search in Windows Sandbox
    # The winget-cli releases include a Dependencies.zip with all required packages
    $wingetVersion = "v1.12.460"
    $depsZipUrl = "https://github.com/microsoft/winget-cli/releases/download/$wingetVersion/DesktopAppInstaller_Dependencies.zip"
    $depsZipPath = Join-Path $tempDir "DesktopAppInstaller_Dependencies.zip"
    
    Write-Log ""
    Write-Log "Step 1: Download winget dependencies from GitHub"
    Write-Log "URL: $depsZipUrl"
    Write-Info "Downloading winget dependencies (~98MB)..."
    
    $downloadOk = Invoke-RobustDownload -Url $depsZipUrl -OutFile $depsZipPath -FileType 'redist-zip' -ExpectBinary
    if (-not $downloadOk) {
        Write-DiagnosticPackageListing
        $diagDetails = Get-DownloadDiagnostics -Url $depsZipUrl -FilePath $depsZipPath -FileType 'redist-zip'
        Write-FatalError -Stage "winget-bootstrap:download-deps" `
            -Message "Winget dependencies download failed (not a valid ZIP)" `
            -Details $diagDetails
    }
    
    $fileSize = (Get-Item $depsZipPath).Length
    Write-Info "Downloaded winget dependencies ($([math]::Round($fileSize / 1MB, 2)) MB)"
    
    # =========================================================================
    # STEP 2: Extract and Install dependency packages (includes Windows App Runtime)
    # =========================================================================
    Set-Stage "winget-bootstrap:install-deps"
    
    $depsExtractDir = Join-Path $tempDir "winget-deps"
    Write-Log ""
    Write-Log "Step 2: Extract and install dependency packages"
    Write-Log "Extracting to: $depsExtractDir"
    Write-Info "Extracting dependency packages..."
    
    try {
        if (Test-Path $depsExtractDir) {
            Remove-Item -Path $depsExtractDir -Recurse -Force
        }
        Expand-Archive -Path $depsZipPath -DestinationPath $depsExtractDir -Force -ErrorAction Stop
        Write-Log "Extraction successful"
    } catch {
        Write-Log "FAILED to extract ZIP: $($_.Exception.GetType().Name): $($_.Exception.Message)"
        Write-DiagnosticPackageListing
        Write-FatalError -Stage "winget-bootstrap:install-deps" `
            -Message "Failed to extract dependencies ZIP (file may be corrupted)" `
            -Details "$($_.Exception.GetType().Name): $($_.Exception.Message)"
    }
    
    # Find all MSIX/APPX packages in the extracted directory
    Write-Log "Scanning for packages in extracted directory..."
    $msixFiles = @()
    $msixFiles += Get-ChildItem -Path $depsExtractDir -Filter "*.msix" -Recurse -ErrorAction SilentlyContinue
    $msixFiles += Get-ChildItem -Path $depsExtractDir -Filter "*.appx" -Recurse -ErrorAction SilentlyContinue
    Write-Log "Found $($msixFiles.Count) package files:"
    foreach ($msix in $msixFiles) {
        Write-Log "  - $($msix.FullName) ($($msix.Length) bytes)"
    }
    
    # Filter for x64 packages only - check FULL PATH since files have same name in different arch folders
    # The ZIP structure is: x64/*.msix, x86/*.msix, arm64/*.msix
    $x64Packages = $msixFiles | Where-Object { 
        $_.FullName -match '[/\\]x64[/\\]' -or $_.Name -match '\.x64\.'
    }
    
    Write-Log "Filtered to $($x64Packages.Count) x64 packages:"
    foreach ($pkg in $x64Packages) {
        Write-Log "  - $($pkg.FullName)"
    }
    
    # Separate by type for install order: VCLibs -> UI.Xaml -> WindowsAppRuntime
    $vclibsPackages = $x64Packages | Where-Object { $_.Name -match 'VCLibs' }
    $uixamlPackages = $x64Packages | Where-Object { $_.Name -match 'UI\.Xaml' }
    $runtimePackages = $x64Packages | Where-Object { $_.Name -match 'WindowsAppRuntime' }
    
    # Install in dependency order: VCLibs -> UI.Xaml -> WindowsAppRuntime
    $installOrder = @()
    $installOrder += $vclibsPackages
    $installOrder += $uixamlPackages
    $installOrder += $runtimePackages
    
    # Remove duplicates and nulls
    $installOrder = $installOrder | Where-Object { $_ } | Select-Object -Unique
    
    Write-Log "Installing $($installOrder.Count) dependency packages in order:"
    $installedCount = 0
    $failedPackages = @()
    
    foreach ($package in $installOrder) {
        Write-Log "  Installing: $($package.Name)"
        try {
            Add-AppxPackage -Path $package.FullName -ErrorAction Stop
            Write-Log "    SUCCESS"
            $installedCount++
        } catch {
            $errorMsg = $_.Exception.Message
            $hresult = ""
            if ($errorMsg -match '0x[0-9A-Fa-f]+') {
                $hresult = $Matches[0]
            }
            
            # Some packages may already be installed or have newer versions
            if ($errorMsg -match 'already installed|higher version|provided package is already installed') {
                Write-Log "    SKIPPED (already installed or newer version present)"
                $installedCount++
            } else {
                Write-Log "    FAILED: $errorMsg"
                if ($hresult) { Write-Log "    HRESULT: $hresult" }
                $failedPackages += @{ Name = $package.Name; Error = $errorMsg; HResult = $hresult }
            }
        }
    }
    
    Write-Log "Installed $installedCount dependency packages"
    Write-Info "Installed $installedCount dependency packages"
    
    # Verify Windows App Runtime is now present
    Write-Log "Verifying Microsoft.WindowsAppRuntime installation..."
    $runtimeCheck = Get-AppxPackage -Name "Microsoft.WindowsAppRuntime*" -ErrorAction SilentlyContinue
    if (-not $runtimeCheck) {
        Write-Log "WARNING: Microsoft.WindowsAppRuntime not found after install (may be OK if pre-installed)"
        Write-DiagnosticPackageListing
    } else {
        Write-Log "VERIFIED: Microsoft.WindowsAppRuntime is installed"
        foreach ($pkg in $runtimeCheck) {
            Write-Log "  $($pkg.Name) v$($pkg.Version)"
        }
    }
    Write-Info "Dependencies installed"
    
    # =========================================================================
    # STEP 3: Download App Installer bundle from GitHub
    # =========================================================================
    Set-Stage "winget-bootstrap:download-appinstaller"
    
    # Use GitHub direct URL - aka.ms URLs redirect to Bing search in Windows Sandbox
    $appInstallerUrl = "https://github.com/microsoft/winget-cli/releases/download/$wingetVersion/Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle"
    $msixBundlePath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    
    Write-Log ""
    Write-Log "Step 3: Download App Installer bundle from GitHub"
    Write-Log "URL: $appInstallerUrl"
    Write-Info "Downloading App Installer (~205MB)..."
    
    $downloadOk = Invoke-RobustDownload -Url $appInstallerUrl -OutFile $msixBundlePath -FileType 'appinstaller' -ExpectBinary
    if (-not $downloadOk) {
        Write-DiagnosticPackageListing
        $diagDetails = Get-DownloadDiagnostics -Url $appInstallerUrl -FilePath $msixBundlePath -FileType 'appinstaller'
        Write-FatalError -Stage "winget-bootstrap:download-appinstaller" `
            -Message "App Installer download failed or corrupted (not a valid MSIX)" `
            -Details $diagDetails
    }
    
    $fileSize = (Get-Item $msixBundlePath).Length
    Write-Info "Downloaded App Installer bundle ($([math]::Round($fileSize / 1MB, 2)) MB)"
    
    # =========================================================================
    # STEP 4: Install App Installer
    # =========================================================================
    Set-Stage "winget-bootstrap:install-appinstaller"
    
    Write-Log ""
    Write-Log "Step 4: Install App Installer"
    Write-Info "Installing App Installer..."
    
    try {
        Add-AppxPackage -Path $msixBundlePath -ErrorAction Stop
        Write-Log "SUCCESS: App Installer package installed"
        Write-Info "App Installer installed"
    } catch {
        $errorMsg = $_.Exception.Message
        $hresult = ""
        if ($errorMsg -match '(0x[0-9A-Fa-f]+)') {
            $hresult = $Matches[1]
        }
        
        Write-Log "FAILED: $($_.Exception.GetType().Name): $errorMsg"
        if ($hresult) { Write-Log "HRESULT: $hresult" }
        Write-DiagnosticPackageListing
        Write-FatalError -Stage "winget-bootstrap:install-appinstaller" `
            -Message "Failed to install App Installer (HRESULT: $hresult)" `
            -Details "$($_.Exception.GetType().Name): $errorMsg"
    }
    
    # =========================================================================
    # STEP 5: Verify winget is now available
    # =========================================================================
    Set-Stage "winget-bootstrap:verify-winget"
    
    Write-Log ""
    Write-Log "Step 5: Verify winget availability"
    Write-Info "Verifying winget..."
    
    # Give Windows a moment to register the package
    Start-Sleep -Seconds 3
    
    # Try multiple methods to find winget
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    
    if (-not $wingetCmd) {
        # Try resolving the expected path directly
        Write-Log "Get-Command failed, trying direct path resolution..."
        $possiblePaths = @(
            "$env:LOCALAPPDATA\Microsoft\WindowsApps\winget.exe",
            "$env:ProgramFiles\WindowsApps\Microsoft.DesktopAppInstaller_*_x64__8wekyb3d8bbwe\winget.exe"
        )
        
        foreach ($pathPattern in $possiblePaths) {
            $resolved = Get-Item -Path $pathPattern -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($resolved) {
                Write-Log "Found winget at: $($resolved.FullName)"
                $wingetCmd = $resolved
                break
            }
        }
    }
    
    if ($wingetCmd) {
        $wingetPath = if ($wingetCmd.Source) { $wingetCmd.Source } else { $wingetCmd.FullName }
        Write-Log "SUCCESS: Winget found at $wingetPath"
        
        # Try to get version
        try {
            $versionOutput = & $wingetPath --version 2>&1
            Write-Log "Winget version: $versionOutput"
            Write-Info "Winget version: $versionOutput"
        } catch {
            Write-Log "Could not get winget version: $($_.Exception.Message)"
        }
        
        Write-Log ""
        Write-Log "Bootstrap completed at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') - SUCCESS"
        Write-DiagnosticPackageListing
        Write-Pass "Winget bootstrap succeeded"
        return $true
    } else {
        Write-Log "FAILED: Winget command not found after installation"
        Write-Log ""
        Write-Log "Diagnostic information:"
        Write-Log "  PATH: $env:Path"
        Write-Log ""
        
        try {
            $allWingetCmds = Get-Command winget -All -ErrorAction SilentlyContinue
            Write-Log "  Get-Command winget -All: $($allWingetCmds | Out-String)"
        } catch {
            Write-Log "  Get-Command winget -All: (none found)"
        }
        
        Write-DiagnosticPackageListing
        
        Write-Log ""
        Write-Log "Bootstrap completed at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') - FAILED"
        
        Write-FatalError -Stage "winget-bootstrap:verify-winget" `
            -Message "Winget command not found after installation" `
            -Details "App Installer was installed but winget.exe is not accessible. Check winget-bootstrap.log for diagnostic package listings."
    }
}

# ============================================================================
# MAIN VALIDATION FLOW
# ============================================================================

Set-Stage "main-init"
Write-Log "Starting main validation flow for AppId=$AppId WingetId=$WingetId"

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

try {

# ============================================================================
# STAGE 1: Load Module
# ============================================================================
Set-Stage "load-module"
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
Set-Stage "winget-bootstrap"
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
Set-Stage "seed"
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
Set-Stage "capture"
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
Set-Stage "wipe"
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
Set-Stage "restore"
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
Set-Stage "verify"
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

Write-Log "END stage=$($script:CurrentStage) status=$($result.status)"

} catch {
    # Catch any unhandled exception
    $errorMsg = $_.Exception.Message
    $errorStack = $_.ScriptStackTrace
    Write-Log "EXCEPTION in stage=$($script:CurrentStage): $errorMsg"
    Write-Log "Stack: $errorStack"
    
    # Write ERROR.txt
    $errorContent = @"
FATAL ERROR at stage: $($script:CurrentStage)
Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Exception: $errorMsg

Stack Trace:
$errorStack
"@
    $errorContent | Out-File -FilePath $script:ErrorFile -Encoding UTF8
    
    # Update STEP.txt to error state
    "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') error:$($script:CurrentStage)" | Out-File -FilePath $script:StepFile -Encoding UTF8
    
    # Write failure result
    $failResult = @{
        status = "FAIL"
        appId = $AppId
        wingetId = $WingetId
        failedStage = $script:CurrentStage
        failReason = $errorMsg
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    }
    $failResult | ConvertTo-Json -Depth 5 | Out-File -FilePath $script:ResultFile -Encoding UTF8
    
    Write-Host "[FATAL] Unhandled exception: $errorMsg" -ForegroundColor Red
    exit 1
} finally {
    Write-Log "FINALLY stage=$($script:CurrentStage)"
}

if ($result.status -eq "PASS") {
    exit 0
} else {
    exit 1
}
