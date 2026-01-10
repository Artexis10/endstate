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

# ============================================================================
# IMMEDIATE STARTUP MARKER - Write before ANY other logic
# ============================================================================
if ($OutputDir -and -not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}
if ($OutputDir) {
    $startedFile = Join-Path $OutputDir "STARTED.txt"
    "Script started at $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')`nPID: $PID`nWingetId: $WingetId`nOutputDir: $OutputDir`nDryRun: $DryRun" | Out-File -FilePath $startedFile -Encoding UTF8
}

# Global output directory for heartbeat (set after param validation)
$script:HeartbeatDir = $OutputDir
$script:StepFile = if ($OutputDir) { Join-Path $OutputDir "STEP.txt" } else { $null }
$script:ErrorFile = if ($OutputDir) { Join-Path $OutputDir "ERROR.txt" } else { $null }

# Ensure TLS 1.2+ globally for all downloads
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

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
    # Also write to STEP.txt for diagnostics
    if ($script:StepFile) {
        "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') $Message" | Out-File -FilePath $script:StepFile -Encoding UTF8
    }
}

function Write-FatalError {
    <#
    .SYNOPSIS
        Writes ERROR.txt and exits with code 1.
    #>
    param(
        [string]$Step,
        [string]$Message,
        [string]$Details = ""
    )
    
    $content = @"
FATAL ERROR at step: $Step
Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Message: $Message

Details:
$Details

Installed AppxPackages (relevant):
VCLibs:
$(Get-AppxPackage -Name "*VCLibs*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
UI.Xaml:
$(Get-AppxPackage -Name "*UI.Xaml*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
WindowsAppRuntime:
$(Get-AppxPackage -Name "*WindowsAppRuntime*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
DesktopAppInstaller:
$(Get-AppxPackage -Name "*DesktopAppInstaller*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
"@
    
    if ($script:ErrorFile) {
        $content | Out-File -FilePath $script:ErrorFile -Encoding UTF8
    }
    Write-Heartbeat "FATAL: $Step" -Details $Message
    Write-Host "[FATAL] $Message" -ForegroundColor Red
    exit 1
}

function Invoke-WithTimeout {
    <#
    .SYNOPSIS
        Runs a script block with a timeout. On timeout, writes ERROR.txt and exits.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [scriptblock]$ScriptBlock,
        [Parameter(Mandatory = $true)]
        [int]$TimeoutSeconds,
        [Parameter(Mandatory = $true)]
        [string]$StepName
    )
    
    Write-Heartbeat "$StepName`: starting (timeout ${TimeoutSeconds}s)"
    
    $job = Start-Job -ScriptBlock $ScriptBlock
    $completed = $job | Wait-Job -Timeout $TimeoutSeconds
    
    if ($null -eq $completed) {
        # Timeout occurred
        $job | Stop-Job
        $job | Remove-Job -Force
        Write-FatalError -Step $StepName -Message "Operation timed out after ${TimeoutSeconds} seconds" -Details "The operation did not complete within the allowed time."
    }
    
    $result = $job | Receive-Job
    $hadError = $job.State -eq 'Failed'
    $job | Remove-Job -Force
    
    if ($hadError) {
        Write-FatalError -Step $StepName -Message "Operation failed" -Details ($result | Out-String)
    }
    
    Write-Heartbeat "$StepName`: completed"
    return $result
}

function Resolve-FinalUrl {
    <#
    .SYNOPSIS
        Resolves redirects and returns the final URL with metadata.
    .DESCRIPTION
        Uses curl.exe (preferred) or HttpClient to follow redirects and capture:
        - Final URL after all redirects
        - Redirect chain (list of intermediate URLs)
        - Response headers (Content-Type, Content-Length)
    .OUTPUTS
        PSCustomObject with: FinalUrl, RedirectChain, ContentType, ContentLength, Success, Error
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Url
    )
    
    $result = [PSCustomObject]@{
        FinalUrl = $Url
        RedirectChain = @()
        ContentType = $null
        ContentLength = $null
        Success = $false
        Error = $null
    }
    
    # Try curl.exe first (most reliable for redirect handling in PS 5.1)
    $curlExe = Get-Command curl.exe -ErrorAction SilentlyContinue
    if ($curlExe) {
        try {
            # Use curl to follow redirects and get final URL + headers
            # -L = follow redirects, -I = HEAD request, -s = silent, -w = write-out format
            $curlOutput = & curl.exe -L -I -s -w "`n%{url_effective}`n%{content_type}" $Url 2>&1
            $lines = $curlOutput -split "`n"
            
            # Parse Location headers for redirect chain
            $redirectChain = @()
            foreach ($line in $lines) {
                if ($line -match '^Location:\s*(.+)$') {
                    $redirectChain += $matches[1].Trim()
                }
            }
            
            # Last two lines are effective URL and content type from -w format
            if ($lines.Count -ge 2) {
                $result.FinalUrl = $lines[-2].Trim()
                $result.ContentType = $lines[-1].Trim()
            }
            
            # Parse Content-Length if present
            foreach ($line in $lines) {
                if ($line -match '^Content-Length:\s*(\d+)') {
                    $result.ContentLength = [int64]$matches[1]
                    break
                }
            }
            
            $result.RedirectChain = $redirectChain
            $result.Success = $true
            return $result
        }
        catch {
            $result.Error = "curl.exe failed: $_"
        }
    }
    
    # Fallback to HttpClient
    try {
        $handler = New-Object System.Net.Http.HttpClientHandler
        $handler.AllowAutoRedirect = $true
        $handler.MaxAutomaticRedirections = 10
        
        $client = New-Object System.Net.Http.HttpClient($handler)
        $client.DefaultRequestHeaders.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Endstate-Sandbox/1.0")
        
        # Use GET with range header to minimize download (HEAD often rejected)
        $request = New-Object System.Net.Http.HttpRequestMessage([System.Net.Http.HttpMethod]::Get, $Url)
        $request.Headers.Range = New-Object System.Net.Headers.RangeHeaderValue(0, 0)
        
        $response = $client.SendAsync($request).GetAwaiter().GetResult()
        
        $result.FinalUrl = $response.RequestMessage.RequestUri.ToString()
        if ($response.Content.Headers.ContentType) {
            $result.ContentType = $response.Content.Headers.ContentType.ToString()
        }
        if ($response.Content.Headers.ContentLength) {
            $result.ContentLength = $response.Content.Headers.ContentLength
        }
        $result.Success = $true
        
        $response.Dispose()
        $client.Dispose()
        $handler.Dispose()
    }
    catch {
        $result.Error = "HttpClient failed: $_"
    }
    
    return $result
}

function Test-ZipMagic {
    <#
    .SYNOPSIS
        Validates that a file has valid ZIP magic bytes.
    .DESCRIPTION
        Reads first 4 bytes and checks for valid ZIP signatures:
        - 50 4B 03 04 (regular ZIP)
        - 50 4B 05 06 (empty archive)
        - 50 4B 07 08 (spanned archive)
    .OUTPUTS
        PSCustomObject with: IsValid, MagicBytes (hex), FirstBytesHex, FirstBytesAscii, FileSize, Error
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath
    )
    
    $result = [PSCustomObject]@{
        IsValid = $false
        MagicBytes = $null
        FirstBytesHex = $null
        FirstBytesAscii = $null
        FileSize = 0
        Error = $null
    }
    
    if (-not (Test-Path $FilePath)) {
        $result.Error = "File not found: $FilePath"
        return $result
    }
    
    try {
        $fileInfo = Get-Item $FilePath
        $result.FileSize = $fileInfo.Length
        
        if ($result.FileSize -lt 4) {
            $result.Error = "File too small for ZIP header (< 4 bytes)"
            return $result
        }
        
        # Read first 32 bytes for diagnostics
        $bytes = [byte[]]::new(32)
        $stream = [System.IO.File]::OpenRead($FilePath)
        $bytesRead = $stream.Read($bytes, 0, [Math]::Min(32, $result.FileSize))
        $stream.Close()
        
        # Format hex and ASCII preview
        $result.FirstBytesHex = ($bytes[0..([Math]::Min($bytesRead, 32) - 1)] | ForEach-Object { $_.ToString("X2") }) -join " "
        $result.FirstBytesAscii = -join ($bytes[0..([Math]::Min($bytesRead, 32) - 1)] | ForEach-Object { 
            if ($_ -ge 32 -and $_ -le 126) { [char]$_ } else { "." }
        })
        
        # Check magic bytes (first 4 bytes)
        $magic = $bytes[0..3]
        $result.MagicBytes = ($magic | ForEach-Object { $_.ToString("X2") }) -join " "
        
        # Valid ZIP signatures
        $validSignatures = @(
            @(0x50, 0x4B, 0x03, 0x04),  # Regular ZIP
            @(0x50, 0x4B, 0x05, 0x06),  # Empty archive
            @(0x50, 0x4B, 0x07, 0x08)   # Spanned archive
        )
        
        foreach ($sig in $validSignatures) {
            if ($magic[0] -eq $sig[0] -and $magic[1] -eq $sig[1] -and 
                $magic[2] -eq $sig[2] -and $magic[3] -eq $sig[3]) {
                $result.IsValid = $true
                return $result
            }
        }
        
        # Check if it's HTML (common redirect/error page)
        $headerStr = [System.Text.Encoding]::ASCII.GetString($bytes[0..([Math]::Min($bytesRead, 20) - 1)])
        if ($headerStr -match '^<!DOCTYPE' -or $headerStr -match '^<html' -or $headerStr -match '^<HTML') {
            $result.Error = "File appears to be HTML, not ZIP"
        } else {
            $result.Error = "Invalid ZIP magic bytes"
        }
    }
    catch {
        $result.Error = "Failed to read file: $_"
    }
    
    return $result
}

function Invoke-RobustDownload {
    <#
    .SYNOPSIS
        Downloads a file with timeout, retries, size validation, redirect resolution, and ZIP validation.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Url,
        [Parameter(Mandatory = $true)]
        [string]$OutFile,
        [Parameter(Mandatory = $true)]
        [string]$StepName,
        [int]$TimeoutSeconds = 180,
        [int]$MinExpectedBytes = 1000000,
        [int]$MaxRetries = 3,
        [switch]$ValidateZip
    )
    
    Write-Step "Downloading: $StepName"
    Write-Heartbeat "$StepName`: downloading" -Details "URL: $Url"
    
    # Resolve redirects first to get final URL and metadata
    $urlInfo = Resolve-FinalUrl -Url $Url
    $finalUrl = if ($urlInfo.Success) { $urlInfo.FinalUrl } else { $Url }
    $downloadUrl = $finalUrl
    
    if ($urlInfo.Success -and $urlInfo.FinalUrl -ne $Url) {
        Write-Info "Resolved final URL: $finalUrl"
        if ($urlInfo.RedirectChain.Count -gt 0) {
            Write-Info "Redirect chain: $($urlInfo.RedirectChain.Count) hops"
        }
        if ($urlInfo.ContentType) {
            Write-Info "Content-Type: $($urlInfo.ContentType)"
        }
        Write-Heartbeat "$StepName`: resolved URL" -Details "Final: $finalUrl`nContent-Type: $($urlInfo.ContentType)"
    }
    
    for ($retry = 1; $retry -le $MaxRetries; $retry++) {
        try {
            # Download to temp file first, then move on success
            $tempFile = "$OutFile.tmp"
            if (Test-Path $tempFile) {
                Remove-Item $tempFile -Force
            }
            if (Test-Path $OutFile) {
                Remove-Item $OutFile -Force
            }
            
            Write-Info "Download attempt $retry of $MaxRetries..."
            Write-Heartbeat "$StepName`: attempt $retry"
            
            # Use WebClient for better timeout control
            $webClient = New-Object System.Net.WebClient
            $webClient.Headers.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Endstate-Sandbox/1.0")
            
            # Download with timeout using async + wait
            $downloadTask = $webClient.DownloadFileTaskAsync($downloadUrl, $tempFile)
            $completed = $downloadTask.Wait($TimeoutSeconds * 1000)
            
            if (-not $completed) {
                $webClient.CancelAsync()
                Write-Host "[WARN] Download timed out after ${TimeoutSeconds}s, retrying..." -ForegroundColor Yellow
                Write-Heartbeat "$StepName`: timeout on attempt $retry"
                continue
            }
            
            if ($downloadTask.IsFaulted) {
                throw $downloadTask.Exception.InnerException
            }
            
            # Validate file exists and size
            if (-not (Test-Path $tempFile)) {
                Write-Host "[WARN] Download did not create file, retrying..." -ForegroundColor Yellow
                continue
            }
            
            $fileSize = (Get-Item $tempFile).Length
            Write-Info "Downloaded file size: $fileSize bytes"
            
            if ($fileSize -lt $MinExpectedBytes) {
                # File too small - likely HTML error page
                $fileContent = Get-Content -Path $tempFile -TotalCount 20 -ErrorAction SilentlyContinue
                $contentPreview = $fileContent -join "`n"
                
                Write-Host "[WARN] Downloaded file too small ($fileSize bytes < $MinExpectedBytes expected)" -ForegroundColor Yellow
                Write-Heartbeat "$StepName`: file too small" -Details "Size: $fileSize bytes`nContent preview:`n$contentPreview"
                
                if ($retry -eq $MaxRetries) {
                    $diagDetails = @"
Expected: >= $MinExpectedBytes bytes
Actual: $fileSize bytes
Original URL: $Url
Final URL: $finalUrl
Content-Type: $($urlInfo.ContentType)

Diagnostics:
- Redirect chain: $($urlInfo.RedirectChain -join ' -> ')
- URL resolution success: $($urlInfo.Success)

File content (first 20 lines):
$contentPreview
"@
                    Write-FatalError -Step $StepName -Message "Downloaded file too small after $MaxRetries attempts" -Details $diagDetails
                }
                Start-Sleep -Seconds 2
                continue
            }
            
            # Validate it's not HTML (common error response)
            $bytes = [System.IO.File]::ReadAllBytes($tempFile)
            if ($bytes.Length -ge 5) {
                $header = [System.Text.Encoding]::ASCII.GetString($bytes[0..4])
                if ($header -match '^<!DOC' -or $header -match '^<html' -or $header -match '^<HTML') {
                    $fileContent = Get-Content -Path $tempFile -TotalCount 20 -ErrorAction SilentlyContinue
                    $contentPreview = $fileContent -join "`n"
                    $diagDetails = @"
Downloaded file is HTML (error page or redirect page)

Original URL: $Url
Final URL: $finalUrl
Content-Type: $($urlInfo.ContentType)
File size: $fileSize bytes

Diagnostics:
- Redirect chain: $($urlInfo.RedirectChain -join ' -> ')

Content:
$contentPreview
"@
                    Write-FatalError -Step $StepName -Message "Downloaded file is HTML (error page)" -Details $diagDetails
                }
            }
            
            # Validate ZIP magic bytes if requested
            if ($ValidateZip) {
                $zipCheck = Test-ZipMagic -FilePath $tempFile
                if (-not $zipCheck.IsValid) {
                    $diagDetails = @"
ZIP validation failed: $($zipCheck.Error)

Original URL: $Url
Final URL: $finalUrl
Content-Type: $($urlInfo.ContentType)
File size: $($zipCheck.FileSize) bytes

Diagnostics:
- Magic bytes: $($zipCheck.MagicBytes)
- First 32 bytes (hex): $($zipCheck.FirstBytesHex)
- First 32 bytes (ASCII): $($zipCheck.FirstBytesAscii)
- Redirect chain: $($urlInfo.RedirectChain -join ' -> ')
"@
                    if ($retry -eq $MaxRetries) {
                        Write-FatalError -Step $StepName -Message "Downloaded file is not a valid ZIP" -Details $diagDetails
                    }
                    Write-Host "[WARN] Invalid ZIP file, retrying..." -ForegroundColor Yellow
                    Write-Heartbeat "$StepName`: invalid ZIP" -Details $diagDetails
                    Start-Sleep -Seconds 2
                    continue
                }
                Write-Info "ZIP validation passed (magic: $($zipCheck.MagicBytes))"
            }
            
            # Move temp file to final location
            Move-Item -Path $tempFile -Destination $OutFile -Force
            
            Write-Pass "Downloaded: $OutFile ($fileSize bytes)"
            Write-Heartbeat "$StepName`: downloaded" -Details "Size: $fileSize bytes"
            return $true
        }
        catch {
            Write-Host "[WARN] Download attempt $retry failed: $_" -ForegroundColor Yellow
            Write-Heartbeat "$StepName`: attempt $retry failed" -Details "$_"
            if ($retry -lt $MaxRetries) {
                Start-Sleep -Seconds 2
            }
        }
        finally {
            # Clean up temp file on failure
            if (Test-Path $tempFile -ErrorAction SilentlyContinue) {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
    }
    
    $diagDetails = @"
Download failed after $MaxRetries attempts

Original URL: $Url
Final URL: $finalUrl
Content-Type: $($urlInfo.ContentType)

Diagnostics:
- URL resolution success: $($urlInfo.Success)
- Redirect chain: $($urlInfo.RedirectChain -join ' -> ')
- Resolution error: $($urlInfo.Error)
"@
    Write-FatalError -Step $StepName -Message "Download failed after $MaxRetries attempts" -Details $diagDetails
}

function Invoke-AppxInstall {
    <#
    .SYNOPSIS
        Installs an AppX package with error handling.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$StepName,
        [string[]]$DependencyPath = @(),
        [switch]$AllowFailure
    )
    
    Write-Step "Installing: $StepName"
    Write-Heartbeat "$StepName`: installing" -Details "Path: $Path, Dependencies: $($DependencyPath.Count)"
    
    try {
        # Run Add-AppxPackage directly (not in a job to preserve array parameters)
        if ($DependencyPath -and $DependencyPath.Count -gt 0) {
            Write-Info "Installing with $($DependencyPath.Count) dependencies"
            Add-AppxPackage -Path $Path -DependencyPath $DependencyPath
        } else {
            Add-AppxPackage -Path $Path
        }
        
        Write-Pass "Installed: $StepName"
        Write-Heartbeat "$StepName`: installed"
        return $true
    }
    catch {
        if ($AllowFailure) {
            $errorDetails = Get-AppxErrorDetails -Exception $_
            Write-Host "[WARN] $StepName failed (allowed): $_" -ForegroundColor Yellow
            Write-Heartbeat "$StepName`: failed (allowed)" -Details $errorDetails
            return $false
        }
        $errorDetails = Get-AppxErrorDetails -Exception $_
        Write-FatalError -Step $StepName -Message "Add-AppxPackage failed" -Details $errorDetails
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

function Write-Heartbeat {
    <#
    .SYNOPSIS
        Writes a heartbeat file to track progress through risky operations.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Step,
        [string]$Details = ""
    )
    
    if (-not $script:HeartbeatDir) { return }
    
    $heartbeatFile = Join-Path $script:HeartbeatDir "HEARTBEAT.txt"
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $content = "[$timestamp] $Step"
    if ($Details) {
        $content += "`n  $Details"
    }
    $content += "`n"
    
    # Append to heartbeat file
    Add-Content -Path $heartbeatFile -Value $content -Encoding UTF8
    Write-Info "Heartbeat: $Step"
}

function Get-AppxErrorDetails {
    <#
    .SYNOPSIS
        Extracts detailed error information from Add-AppxPackage failures.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Exception
    )
    
    $details = @()
    $details += "Exception: $($Exception.Exception.Message)"
    
    # Try to extract ActivityId from error message
    $activityIdPattern = 'ActivityId:\s*([a-fA-F0-9\-]+)'
    if ($Exception.Exception.Message -match $activityIdPattern) {
        $activityId = $matches[1]
        $details += "ActivityId: $activityId"
        
        # Try to get AppxPackageLog for this activity
        try {
            $logPath = Join-Path $env:TEMP "AppxPackageLog_$activityId.txt"
            $log = Get-AppPackageLog -ActivityId $activityId -ErrorAction SilentlyContinue
            if ($log) {
                # Format EventLogRecord objects as readable text (not raw type names)
                $formattedLog = $log | Format-List TimeCreated, Id, LevelDisplayName, Message | Out-String -Width 200
                $formattedLog | Out-File -FilePath $logPath -Encoding UTF8
                $details += "AppPackageLog saved to: $logPath"
                $details += "Log content:"
                $details += $formattedLog
            }
        }
        catch {
            $details += "Could not retrieve AppPackageLog: $_"
        }
    }
    
    # Include inner exception if present
    if ($Exception.Exception.InnerException) {
        $details += "InnerException: $($Exception.Exception.InnerException.Message)"
    }
    
    return $details -join "`n"
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
    Write-Heartbeat "WindowsAppRuntime18: checking"
    
    # Check if already installed - query framework and CBS identities separately
    # CRITICAL: CBS packages (Microsoft.WindowsAppRuntime.CBS.1.8) do NOT satisfy App Installer's
    # dependency requirement. We must check for the actual framework identity.
    #
    # Framework identity: Microsoft.WindowsAppRuntime.1.8* (e.g. Microsoft.WindowsAppRuntime.1.8_8000.xxx_x64__8wekyb3d8bbwe)
    # CBS identity:       Microsoft.WindowsAppRuntime.CBS.1.8* (NOT sufficient for App Installer)
    
    # Query framework packages (explicit namespace - NOT CBS)
    $frameworkPackages = Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object { 
        $_.Name -like "Microsoft.WindowsAppRuntime.1.8*" -and $_.Name -notlike "Microsoft.WindowsAppRuntime.CBS.*"
    }
    
    # Query CBS packages separately (for logging only - they NEVER satisfy the requirement)
    $cbsPackages = Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object { 
        $_.Name -like "Microsoft.WindowsAppRuntime.CBS.1.8*"
    }
    
    if ($cbsPackages -and -not $frameworkPackages) {
        Write-Info "Found CBS package (does NOT satisfy App Installer requirement):"
        $cbsPackages | ForEach-Object { Write-Info "  $($_.Name) v$($_.Version)" }
        Write-Heartbeat "WindowsAppRuntime18: CBS present but insufficient" -Details ($cbsPackages | ForEach-Object { "$($_.Name) v$($_.Version)" } | Out-String)
    }
    
    if ($frameworkPackages) {
        $first = $frameworkPackages | Select-Object -First 1
        Write-Pass "Windows App Runtime 1.8 framework found: $($first.Name) v$($first.Version)"
        Write-Heartbeat "WindowsAppRuntime18: already installed" -Details "Package: $($first.Name), Version: $($first.Version)"
        return @()
    }
    
    Write-Info "Windows App Runtime 1.8 not found. Installing from redist zip..."
    
    # Create temp directory for downloads
    if (-not (Test-Path $TempDir)) {
        New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
    }
    
    # Windows App Runtime 1.8 redistributable zip URL
    $redistUrl = "https://aka.ms/windowsappsdk/1.8/latest/Microsoft.WindowsAppRuntime.Redist.1.8.zip"
    $redistZipPath = Join-Path $TempDir "Microsoft.WindowsAppRuntime.Redist.1.8.zip"
    
    # Download with robust helper (timeout, retries, size validation, ZIP validation)
    Invoke-RobustDownload -Url $redistUrl -OutFile $redistZipPath -StepName "WindowsAppRuntime18-download" -MinExpectedBytes 1000000 -ValidateZip
    
    # Extract the zip
    $redistExtractDir = Join-Path $TempDir "windowsappruntime-extract"
    if (Test-Path $redistExtractDir) {
        Remove-Item $redistExtractDir -Recurse -Force
    }
    
    Write-Step "Extracting Windows App Runtime zip..."
    Write-Heartbeat "WindowsAppRuntime18: extracting"
    try {
        Expand-Archive -Path $redistZipPath -DestinationPath $redistExtractDir -Force
        Write-Pass "Extracted to: $redistExtractDir"
        Write-Heartbeat "WindowsAppRuntime18: extracted"
    }
    catch {
        $fileInfo = Get-Item $redistZipPath -ErrorAction SilentlyContinue
        Write-FatalError -Step "WindowsAppRuntime18-extract" -Message "Failed to extract zip" -Details "File: $redistZipPath, Size: $($fileInfo.Length) bytes`nError: $_"
    }
    
    # Find and install x64 MSIX packages
    $msixPackages = Get-ChildItem -Path $redistExtractDir -Recurse -Include "*.msix" -File
    $x64Packages = $msixPackages | Where-Object { $_.Name -match 'x64' }
    Write-Info "Found $($x64Packages.Count) x64 packages to install"
    Write-Heartbeat "WindowsAppRuntime18: installing packages" -Details "Count: $($x64Packages.Count)"
    
    $installedPaths = @()
    foreach ($pkg in $x64Packages) {
        $installed = Invoke-AppxInstall -Path $pkg.FullName -StepName "WindowsAppRuntime18-$($pkg.BaseName)" -AllowFailure
        if ($installed) {
            $installedPaths += $pkg.FullName
        }
    }
    
    # Validate installation - check for actual framework identity (explicit namespace, not CBS)
    $runtime = Get-AppxPackage -ErrorAction SilentlyContinue | Where-Object { 
        $_.Name -like "Microsoft.WindowsAppRuntime.1.8*" -and $_.Name -notlike "Microsoft.WindowsAppRuntime.CBS.*"
    } | Select-Object -First 1
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
        Write-Heartbeat "WindowsAppRuntime18: FAILED" -Details $diagInfo
        throw $diagInfo
    }
    
    Write-Pass "Windows App Runtime 1.8 installed: $($runtime.Version)"
    Write-Heartbeat "WindowsAppRuntime18: SUCCESS" -Details "Version: $($runtime.Version)"
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
    Write-Heartbeat "Winget: checking"
    
    # Check if winget is already available
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetCmd) {
        Write-Pass "winget is available at: $($wingetCmd.Source)"
        Write-Heartbeat "Winget: already available" -Details "Path: $($wingetCmd.Source)"
        return
    }
    
    Write-Info "winget not found. Bootstrapping with explicit dependency downloads..."
    Write-Heartbeat "Winget: bootstrapping"
    
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
    # Use official aka.ms URL which is stable and Microsoft-hosted
    # ========================================================================
    $vcLibsDesktopUrl = "https://aka.ms/Microsoft.VCLibs.x64.14.00.Desktop.appx"
    $vcLibsDesktopPath = Join-Path $tempDir "Microsoft.VCLibs.x64.14.00.Desktop.appx"
    
    Invoke-RobustDownload -Url $vcLibsDesktopUrl -OutFile $vcLibsDesktopPath -StepName "VCLibs-download" -MinExpectedBytes 500000
    $downloadedDeps += "VCLibs Desktop: $vcLibsDesktopPath"
    
    $vcLibsInstalled = Invoke-AppxInstall -Path $vcLibsDesktopPath -StepName "VCLibs-install" -AllowFailure
    if ($vcLibsInstalled) {
        $dependencyPaths += $vcLibsDesktopPath
    }
    
    # ========================================================================
    # Step 3: Download and install UI.Xaml from NuGet
    # ========================================================================
    $uiXamlInstalled = $false
    $uiXamlNugetUrl = "https://www.nuget.org/api/v2/package/Microsoft.UI.Xaml/2.8.6"
    $uiXamlNupkg = Join-Path $tempDir "Microsoft.UI.Xaml.2.8.6.nupkg"
    
    try {
        Invoke-RobustDownload -Url $uiXamlNugetUrl -OutFile $uiXamlNupkg -StepName "UI.Xaml-download" -MinExpectedBytes 100000
        
        # Extract nupkg (copy to .zip for PS 5.1 compatibility)
        Write-Step "Extracting UI.Xaml NuGet package..."
        Write-Heartbeat "UI.Xaml: extracting"
        $uiXamlZip = [System.IO.Path]::ChangeExtension($uiXamlNupkg, '.zip')
        Copy-Item -Path $uiXamlNupkg -Destination $uiXamlZip -Force
        
        $uiXamlExtract = Join-Path $tempDir "uixaml-extract"
        if (Test-Path $uiXamlExtract) {
            Remove-Item $uiXamlExtract -Recurse -Force
        }
        Expand-Archive -Path $uiXamlZip -DestinationPath $uiXamlExtract -Force
        Write-Heartbeat "UI.Xaml: extracted"
        
        # Find x64 appx
        $uiXamlCandidates = Get-ChildItem -Path $uiXamlExtract -Recurse -Include "*.appx" -File
        Write-Info "Found $($uiXamlCandidates.Count) appx candidates in NuGet package"
        $selectedUiXaml = Select-BestPackageCandidate -Candidates $uiXamlCandidates -PackageName "UI.Xaml"
        
        if ($selectedUiXaml) {
            $uiXamlInstalled = Invoke-AppxInstall -Path $selectedUiXaml.FullName -StepName "UI.Xaml-install" -AllowFailure
            if ($uiXamlInstalled) {
                $dependencyPaths += $selectedUiXaml.FullName
                $downloadedDeps += "UI.Xaml (NuGet): $($selectedUiXaml.FullName)"
            }
        } else {
            Write-Host "[WARN] No suitable UI.Xaml appx found in NuGet package" -ForegroundColor Yellow
            Write-Heartbeat "UI.Xaml: no suitable appx in NuGet package"
        }
    }
    catch {
        Write-Host "[WARN] UI.Xaml NuGet approach failed: $_" -ForegroundColor Yellow
        Write-Heartbeat "UI.Xaml: NuGet failed" -Details "$_"
    }
    
    if (-not $uiXamlInstalled) {
        Write-Host "[WARN] UI.Xaml could not be installed (may already be present)" -ForegroundColor Yellow
        Write-Heartbeat "UI.Xaml: install skipped or failed"
    }
    
    # ========================================================================
    # Step 4: Download App Installer bundle
    # ========================================================================
    $wingetUrl = "https://aka.ms/getwinget"
    $wingetBundlePath = Join-Path $tempDir "Microsoft.DesktopAppInstaller.msixbundle"
    
    Invoke-RobustDownload -Url $wingetUrl -OutFile $wingetBundlePath -StepName "AppInstaller-download" -MinExpectedBytes 1000000
    
    # ========================================================================
    # Step 5: Install App Installer bundle with dependencies
    # ========================================================================
    Write-Step "Installing App Installer bundle..."
    Write-Heartbeat "AppInstaller: installing" -Details "Bundle: $wingetBundlePath, Dependencies: $($dependencyPaths.Count)"
    Write-Info "Installing with $($dependencyPaths.Count) dependencies"
    $dependencyPaths | ForEach-Object { Write-Info "  Dependency: $_" }
    
    try {
        if ($dependencyPaths -and $dependencyPaths.Count -gt 0) {
            Add-AppxPackage -Path $wingetBundlePath -DependencyPath $dependencyPaths -ErrorAction Stop
        } else {
            Add-AppxPackage -Path $wingetBundlePath -ErrorAction Stop
        }
        Write-Pass "Installed App Installer bundle"
        Write-Heartbeat "AppInstaller: installed"
    }
    catch {
        # Build rich diagnostic output for App Installer installation failure
        Write-Heartbeat "AppInstaller: FAILED" -Details $_.Exception.Message
        
        # Full exception dump
        $exceptionDump = $_ | Format-List * -Force | Out-String
        
        # Try to extract ActivityId and get AppPackageLog
        $appPackageLog = ""
        $activityIdPattern = '([a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})'
        if ($_.Exception.Message -match $activityIdPattern) {
            $activityId = $matches[1]
            try {
                $logOutput = Get-AppPackageLog -ActivityID $activityId -ErrorAction SilentlyContinue
                if ($logOutput) {
                    # Format EventLogRecord objects as readable text (not raw type names)
                    $formattedLog = $logOutput | Format-List TimeCreated, Id, LevelDisplayName, Message | Out-String -Width 200
                    $appPackageLog = "=== Get-AppPackageLog for ActivityId $activityId ===`n$formattedLog"
                } else {
                    $appPackageLog = "=== Get-AppPackageLog for ActivityId $activityId returned no output ==="
                }
            }
            catch {
                $appPackageLog = "=== Get-AppPackageLog failed: $_ ==="
            }
        }
        
        # List currently installed packages
        $installerPkgs = Get-AppxPackage Microsoft.DesktopAppInstaller* -ErrorAction SilentlyContinue | Format-List * | Out-String
        $vclibsPkgs = Get-AppxPackage Microsoft.VCLibs* -ErrorAction SilentlyContinue | Select-Object Name, Version, Architecture | Format-Table -AutoSize | Out-String
        $uixamlPkgs = Get-AppxPackage Microsoft.UI.Xaml* -ErrorAction SilentlyContinue | Select-Object Name, Version, Architecture | Format-Table -AutoSize | Out-String
        $runtimePkgs = Get-AppxPackage Microsoft.WindowsAppRuntime* -ErrorAction SilentlyContinue | Select-Object Name, Version, Architecture | Format-Table -AutoSize | Out-String
        
        $diagContent = @"
=== APP INSTALLER INSTALLATION FAILED ===
Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')

=== EXCEPTION DUMP ===
$exceptionDump

$appPackageLog

=== BUNDLE INFO ===
Bundle path: $wingetBundlePath
Bundle exists: $(Test-Path $wingetBundlePath)
Bundle size: $((Get-Item $wingetBundlePath -ErrorAction SilentlyContinue).Length) bytes

=== DEPENDENCY PATHS ===
$($dependencyPaths | ForEach-Object { "$_ (exists: $(Test-Path $_))" } | Out-String)

=== INSTALLED PACKAGES: Microsoft.DesktopAppInstaller* ===
$installerPkgs

=== INSTALLED PACKAGES: Microsoft.VCLibs* ===
$vclibsPkgs

=== INSTALLED PACKAGES: Microsoft.UI.Xaml* ===
$uixamlPkgs

=== INSTALLED PACKAGES: Microsoft.WindowsAppRuntime* ===
$runtimePkgs
"@
        
        # Write to ERROR.txt
        if ($script:ErrorFile) {
            $diagContent | Out-File -FilePath $script:ErrorFile -Encoding UTF8
        }
        
        # Also output to console
        Write-Host $diagContent -ForegroundColor Red
        Write-Host "[FATAL] App Installer installation failed. See ERROR.txt for details." -ForegroundColor Red
        exit 1
    }
    
    # ========================================================================
    # Step 6: Verify winget is available
    # ========================================================================
    Write-Heartbeat "Winget: verifying installation"
    # Refresh PATH - winget installs to WindowsApps which should be in PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    
    $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $wingetCmd) {
        # Try common install location directly
        $wingetExe = Join-Path $env:LOCALAPPDATA "Microsoft\WindowsApps\winget.exe"
        if (Test-Path $wingetExe) {
            Write-Pass "winget installed at: $wingetExe"
            Write-Heartbeat "Winget: found at $wingetExe"
        }
        else {
            Write-Heartbeat "Winget: NOT FOUND after install"
            throw "winget installation completed but winget command is still not available"
        }
    }
    else {
        Write-Pass "winget available at: $($wingetCmd.Source)"
        Write-Heartbeat "Winget: available" -Details "Path: $($wingetCmd.Source)"
    }
    
    # Verify winget version
    try {
        $wingetVersion = & winget --version 2>&1
        Write-Pass "winget version: $wingetVersion"
        Write-Heartbeat "Winget: version $wingetVersion"
    }
    catch {
        Write-Host "[WARN] Could not get winget version: $_" -ForegroundColor Yellow
        Write-Heartbeat "Winget: version check failed" -Details "$_"
    }
    
    # ========================================================================
    # Step 7: Best-effort winget source update
    # ========================================================================
    Write-Step "Updating winget sources (best-effort)..."
    Write-Heartbeat "Winget: updating sources"
    try {
        $sourceResult = & winget source update 2>&1
        Write-Pass "winget source update completed"
        Write-Heartbeat "Winget: sources updated"
    }
    catch {
        Write-Host "[WARN] winget source update failed (non-fatal): $_" -ForegroundColor Yellow
        Write-Heartbeat "Winget: source update failed (non-fatal)" -Details "$_"
    }
    
    Write-Pass "winget bootstrap complete"
    Write-Heartbeat "Winget: bootstrap COMPLETE"
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

# Output directory already created at top of script (for STARTED.txt)
# HeartbeatDir already set at top of script

Write-Heartbeat "Harness main: starting" -Details "WingetId: $WingetId, DryRun: $DryRun"

# Sentinel file paths
$doneFile = Join-Path $OutputDir "DONE.txt"
$errorFile = Join-Path $OutputDir "ERROR.txt"

try {
    # Step 0: Ensure winget is available (bootstrap if needed) - BEFORE snapshots
    # This must happen first because winget bootstrap modifies the filesystem
    if (-not $DryRun) {
        Write-Heartbeat "Bootstrap: starting winget bootstrap"
        Ensure-Winget
        Write-Heartbeat "Bootstrap: winget ready"
    }
    
    # Step 1: Pre-install snapshot
    Write-Heartbeat "Snapshot: capturing pre-install"
    Write-Step "Capturing pre-install snapshot..."
    $preSnapshot = Get-FilesystemSnapshot -Roots $snapshotRoots -MaxDepth 8
    Write-Info "Pre-snapshot: $($preSnapshot.Count) items"

    $preJsonPath = Join-Path $OutputDir "pre.json"
    $preSnapshot | ConvertTo-Json -Depth 10 -Compress | Out-File -FilePath $preJsonPath -Encoding UTF8
    Write-Pass "Saved: $preJsonPath"
    Write-Heartbeat "Snapshot: pre-install saved" -Details "Items: $($preSnapshot.Count)"
    
    # Step 2: Install via winget
    if ($DryRun) {
        Write-Step "DRY RUN: Skipping winget install for $WingetId"
        Write-Info "Would run: winget install --id $WingetId --silent --accept-package-agreements --accept-source-agreements"
        Write-Heartbeat "Install: DRY RUN skipped"
    } else {
        Write-Heartbeat "Install: starting $WingetId"
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
            Write-Heartbeat "Install: winget exit code $exitCode" -Details ($result -join "`n")
        } else {
            Write-Pass "Winget install completed"
            Write-Heartbeat "Install: $WingetId completed"
        }
        
        # Wait for installers to settle
        Write-Info "Waiting 5 seconds for installers to settle..."
        Start-Sleep -Seconds 5
    }

    # Step 3: Post-install snapshot
    Write-Heartbeat "Snapshot: capturing post-install"
    Write-Step "Capturing post-install snapshot..."
    $postSnapshot = Get-FilesystemSnapshot -Roots $snapshotRoots -MaxDepth 8
    Write-Info "Post-snapshot: $($postSnapshot.Count) items"

    $postJsonPath = Join-Path $OutputDir "post.json"
    $postSnapshot | ConvertTo-Json -Depth 10 -Compress | Out-File -FilePath $postJsonPath -Encoding UTF8
    Write-Pass "Saved: $postJsonPath"
    Write-Heartbeat "Snapshot: post-install saved" -Details "Items: $($postSnapshot.Count)"

    # Step 4: Compute diff inside sandbox (for immediate feedback)
    Write-Heartbeat "Diff: computing"
    Write-Step "Computing diff..."
    $diff = Compare-FilesystemSnapshots -PreSnapshot $preSnapshot -PostSnapshot $postSnapshot
    Write-Info "Added: $($diff.added.Count) items"
    Write-Info "Modified: $($diff.modified.Count) items"

    $diffJsonPath = Join-Path $OutputDir "diff.json"
    $diff | ConvertTo-Json -Depth 10 | Out-File -FilePath $diffJsonPath -Encoding UTF8
    Write-Pass "Saved: $diffJsonPath"
    Write-Heartbeat "Diff: saved" -Details "Added: $($diff.added.Count), Modified: $($diff.modified.Count)"

    # Write DONE.txt sentinel on success
    "SUCCESS" | Out-File -FilePath $doneFile -Encoding UTF8
    Write-Pass "Wrote sentinel: $doneFile"
    Write-Heartbeat "Harness: SUCCESS"

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
    # Write ERROR.txt with comprehensive exception details
    $appxErrorDetails = Get-AppxErrorDetails -Exception $_
    $errorContent = @"
Exception: $($_.Exception.Message)
ScriptStackTrace: $($_.ScriptStackTrace)
InvocationInfo: $($_.InvocationInfo.PositionMessage)

Appx Error Details:
$appxErrorDetails

Installed AppxPackages (relevant):
VCLibs:
$(Get-AppxPackage -Name "*VCLibs*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
UI.Xaml:
$(Get-AppxPackage -Name "*UI.Xaml*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
WindowsAppRuntime:
$(Get-AppxPackage -Name "*WindowsAppRuntime*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
DesktopAppInstaller:
$(Get-AppxPackage -Name "*DesktopAppInstaller*" -ErrorAction SilentlyContinue | ForEach-Object { "  $($_.Name) v$($_.Version)" } | Out-String)
"@
    $errorContent | Out-File -FilePath $errorFile -Encoding UTF8
    Write-Heartbeat "Harness: FAILED" -Details $_.Exception.Message
    Write-Host "[ERROR] $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "[ERROR] Details written to: $errorFile" -ForegroundColor Red
    exit 1
}
