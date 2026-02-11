<#
.SYNOPSIS
    Seeds meaningful ShareX configuration for curation testing.

.DESCRIPTION
    Sets up ShareX configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - ApplicationConfig.json (general settings)
    - HotkeysConfig.json (hotkey assignments)
    - UploadersConfig.json (uploader structure with placeholder values)

    DOES NOT configure:
    - API keys or OAuth tokens
    - FTP/SFTP credentials
    - Upload destination secrets

.EXAMPLE
    .\seed.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Write-Step {
    param([string]$Message)
    Write-Host "[SEED] $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " ShareX Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$sharexDir = Join-Path $env:USERPROFILE "Documents\ShareX"
$appConfigPath = Join-Path $sharexDir "ApplicationConfig.json"
$hotkeysConfigPath = Join-Path $sharexDir "HotkeysConfig.json"
$uploadersConfigPath = Join-Path $sharexDir "UploadersConfig.json"

# Ensure directory exists
if (-not (Test-Path $sharexDir)) {
    Write-Step "Creating ShareX directory..."
    New-Item -ItemType Directory -Path $sharexDir -Force | Out-Null
}

# ============================================================================
# APPLICATION CONFIG
# ============================================================================
Write-Step "Writing ApplicationConfig.json..."

$appConfig = @{
    ShowTray = $true
    SilentRun = $false
    PlaySoundAfterCapture = $true
    PlaySoundAfterUpload = $true
    ShowToastNotificationAfterTaskCompleted = $true
    ShowAfterCaptureTasksForm = $false
    ShowAfterUploadForm = $false
    TaskbarProgressEnabled = $true
    RememberMainFormPosition = $true
    RememberMainFormSize = $true
    Theme = "DarkTheme"
    UseCustomScreenshotsPath = $false
    CustomScreenshotsPath = ""
    SaveImageSubFolderPattern = "%y-%mo"
    AutoCheckUpdate = $true
}

$appConfig | ConvertTo-Json -Depth 5 | Set-Content -Path $appConfigPath -Encoding UTF8

Write-Pass "ApplicationConfig written to: $appConfigPath"

# ============================================================================
# HOTKEYS CONFIG
# ============================================================================
Write-Step "Writing HotkeysConfig.json..."

$hotkeysConfig = @{
    Hotkeys = @(
        @{
            Description = "Capture region"
            HotkeyInfo = @{ Hotkey = "PrintScreen" }
            TaskSettings = @{ Job = "CaptureRegion" }
        }
        @{
            Description = "Capture entire screen"
            HotkeyInfo = @{ Hotkey = "Ctrl+PrintScreen" }
            TaskSettings = @{ Job = "CaptureFullscreen" }
        }
        @{
            Description = "Capture active window"
            HotkeyInfo = @{ Hotkey = "Alt+PrintScreen" }
            TaskSettings = @{ Job = "CaptureActiveWindow" }
        }
        @{
            Description = "Screen recording"
            HotkeyInfo = @{ Hotkey = "Shift+PrintScreen" }
            TaskSettings = @{ Job = "ScreenRecorder" }
        }
        @{
            Description = "Screen recording (GIF)"
            HotkeyInfo = @{ Hotkey = "Ctrl+Shift+PrintScreen" }
            TaskSettings = @{ Job = "ScreenRecorderGIF" }
        }
    )
}

$hotkeysConfig | ConvertTo-Json -Depth 5 | Set-Content -Path $hotkeysConfigPath -Encoding UTF8

Write-Pass "HotkeysConfig written to: $hotkeysConfigPath"

# ============================================================================
# UPLOADERS CONFIG (placeholder - no real credentials)
# ============================================================================
Write-Step "Writing UploadersConfig.json (placeholder values only)..."

$uploadersConfig = @{
    ImgurAccountType = "Anonymous"
    ImgurAlbumID = ""
    DropboxAccountInfo = $null
    FTPAccountList = @()
    CustomUploadersList = @()
    FileDestination = "Dropbox"
    ImageDestination = "Imgur"
    TextDestination = "Pastebin"
}

$uploadersConfig | ConvertTo-Json -Depth 5 | Set-Content -Path $uploadersConfigPath -Encoding UTF8

Write-Pass "UploadersConfig written to: $uploadersConfigPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($appConfigPath, $hotkeysConfigPath, $uploadersConfigPath)
foreach ($f in $seededFiles) {
    $exists = Test-Path $f
    $size = if ($exists) { (Get-Item $f).Length } else { 'N/A' }
    Write-Host "  $f exists=$exists size=$size" -ForegroundColor Gray
}

# ============================================================================
# SUMMARY
# ============================================================================
Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " ShareX Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $appConfigPath" -ForegroundColor Gray
Write-Host "  - $hotkeysConfigPath" -ForegroundColor Gray
Write-Host "  - $uploadersConfigPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
