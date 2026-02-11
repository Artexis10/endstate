<#
.SYNOPSIS
    Seeds meaningful Ditto configuration for curation testing.

.DESCRIPTION
    Sets up Ditto configuration with representative non-default values
    WITHOUT creating any clipboard data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Ditto.settings (application preferences)

    DOES NOT configure:
    - Clipboard database (Ditto.db - contains sensitive copied content)

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
Write-Host " Ditto Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$dittoDir = Join-Path $env:APPDATA "Ditto"
$settingsPath = Join-Path $dittoDir "Ditto.settings"

if (-not (Test-Path $dittoDir)) {
    Write-Step "Creating Ditto directory..."
    New-Item -ItemType Directory -Path $dittoDir -Force | Out-Null
}

# ============================================================================
# SETTINGS
# ============================================================================
Write-Step "Writing Ditto.settings..."

$settings = @{
    General = @{
        MaxClips = 500
        ExpireAfterDays = 30
        ShowIconInSystemTray = $true
        StartWithWindows = $true
        ShowStartupMessage = $false
        PlaySoundOnCopy = $false
    }
    Hotkeys = @{
        ActivateDitto = 'Ctrl+`'
        PasteClip1 = "Ctrl+Shift+1"
        PasteClip2 = "Ctrl+Shift+2"
        PasteClip3 = "Ctrl+Shift+3"
    }
    Display = @{
        Theme = "Dark"
        Transparency = 95
        MaxDescriptionLength = 200
        ShowThumbnails = $true
        MaxThumbnailWidth = 200
        MaxThumbnailHeight = 100
        Position = "Cursor"
        AlwaysShowDescription = $true
    }
    Database = @{
        CompactOnExit = $true
        CompactDate = 7
    }
    Types = @{
        CaptureText = $true
        CaptureImages = $true
        CaptureFiles = $false
        ExcludePasswords = $true
    }
}

$settings | ConvertTo-Json -Depth 5 | Set-Content -Path $settingsPath -Encoding UTF8

Write-Pass "Settings written to: $settingsPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath)
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
Write-Host " Ditto Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $settingsPath" -ForegroundColor Gray
Write-Host ""
Write-Step "Excluded (sensitive):"
Write-Host "  - Ditto.db (clipboard database)" -ForegroundColor DarkYellow
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
