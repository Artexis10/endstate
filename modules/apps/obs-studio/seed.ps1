<#
.SYNOPSIS
    Seeds meaningful OBS Studio configuration for curation testing.

.DESCRIPTION
    Sets up OBS Studio configuration files with representative non-default values
    WITHOUT creating any credentials or stream keys. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - global.ini (global settings)
    - Profile: basic.ini (encoding/output settings)
    - Scene collection (sample scene)

    DOES NOT configure:
    - Stream keys (Twitch, YouTube, etc.)
    - Plugin-specific credentials
    - OAuth tokens

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
Write-Host " OBS Studio Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$obsDir = Join-Path $env:APPDATA "obs-studio"
$globalIniPath = Join-Path $obsDir "global.ini"
$profileDir = Join-Path $obsDir "basic\profiles\Endstate"
$profileIniPath = Join-Path $profileDir "basic.ini"
$scenesDir = Join-Path $obsDir "basic\scenes"
$sceneJsonPath = Join-Path $scenesDir "Endstate.json"

# Ensure directories exist
foreach ($dir in @($obsDir, $profileDir, $scenesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# GLOBAL.INI
# ============================================================================
Write-Step "Writing global.ini..."

$globalIni = @"
[General]
Pre23Defaults=true
CurrentTheme3=System
EnableAutoUpdates=true
FirstRun=false
LastVersion=30.0.0

[BasicWindow]
geometry=AdnQywADAAAAAABkAAAAMgAABX8AAANHAAAAZAAAADIAAAVfAAADJwAAAAACAAAABaA=
DockState=placeholder
AlwaysOnTop=false

[Video]
BaseCX=1920
BaseCY=1080
OutputCX=1920
OutputCY=1080

[Audio]
ChannelSetup=Stereo
SampleRate=48000
"@

$globalIni | Set-Content -Path $globalIniPath -Encoding UTF8

Write-Pass "global.ini written to: $globalIniPath"

# ============================================================================
# PROFILE (basic.ini)
# ============================================================================
Write-Step "Writing profile basic.ini..."

$profileIni = @"
[General]
Name=Endstate

[Video]
BaseCX=1920
BaseCY=1080
OutputCX=1920
OutputCY=1080
FPSType=0
FPSCommon=60

[Output]
Mode=Advanced
RecType=Standard
RecFormat2=mkv
RecEncoder=obs_x264
RecFilePath=C:\Users\placeholder\Videos

[AdvOut]
RecRB=false
TrackIndex=1
RecTracks=1
FFOutputToFile=true
Encoder=obs_x264
FFVBitrate=2500
FFABitrate=160
FFVEncoder=libx264
FFAEncoder=aac

[SimpleOutput]
FilePath=C:\Users\placeholder\Videos
RecFormat2=mkv
StreamEncoder=x264
VBitrate=2500
ABitrate=160
RecQuality=Small
RecEncoder=x264

[Stream1]
ServiceType=rtmp_common
"@

$profileIni | Set-Content -Path $profileIniPath -Encoding UTF8

Write-Pass "Profile basic.ini written to: $profileIniPath"

# ============================================================================
# SCENE COLLECTION
# ============================================================================
Write-Step "Writing sample scene collection..."

$sceneCollection = @{
    current_scene = "Main Scene"
    current_program_scene = "Main Scene"
    scene_order = @(
        @{ name = "Main Scene" }
        @{ name = "BRB Screen" }
    )
    name = "Endstate"
    sources = @(
        @{
            prev_ver = 30
            name = "Display Capture"
            uuid = "placeholder-uuid-0000-0000-000000000001"
            id = "monitor_capture"
            versioned_id = "monitor_capture"
            settings = @{
                monitor = 0
                capture_cursor = $true
            }
        }
        @{
            prev_ver = 30
            name = "Audio Input"
            uuid = "placeholder-uuid-0000-0000-000000000002"
            id = "wasapi_input_capture"
            versioned_id = "wasapi_input_capture_v2"
            settings = @{
                device_id = "default"
            }
        }
    )
}

$sceneCollection | ConvertTo-Json -Depth 10 | Set-Content -Path $sceneJsonPath -Encoding UTF8

Write-Pass "Scene collection written to: $sceneJsonPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($globalIniPath, $profileIniPath, $sceneJsonPath)
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
Write-Host " OBS Studio Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $globalIniPath" -ForegroundColor Gray
Write-Host "  - $profileIniPath" -ForegroundColor Gray
Write-Host "  - $sceneJsonPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
