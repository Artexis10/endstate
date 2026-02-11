<#
.SYNOPSIS
    Seeds meaningful Ableton Live configuration for curation testing.

.DESCRIPTION
    Sets up Ableton Live configuration files with representative non-default values
    WITHOUT creating any license data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Preferences.cfg (application preferences)
    - User Library/Presets/ (sample preset)
    - User Library/Defaults/ (sample default settings)

    DOES NOT configure:
    - Authorization/unlock files
    - Audio samples
    - Crash recovery data

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
Write-Host " Ableton Live Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS (use representative version folder)
# ============================================================================
$abletonDir = Join-Path $env:APPDATA "Ableton\Live 12"
$prefsPath = Join-Path $abletonDir "Preferences.cfg"
$userLibDir = Join-Path $env:USERPROFILE "Documents\Ableton\User Library"
$presetsDir = Join-Path $userLibDir "Presets\Audio Effects\Endstate"
$defaultsDir = Join-Path $userLibDir "Defaults"

foreach ($dir in @($abletonDir, $presetsDir, $defaultsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# PREFERENCES
# ============================================================================
Write-Step "Writing Preferences.cfg..."

$prefs = @'
<?xml version="1.0" encoding="UTF-8"?>
<Ableton MajorVersion="5" MinorVersion="12.0" Creator="Endstate Seed">
    <Preferences>
        <LookFeel>
            <SkinName Value="Dark" />
            <ZoomLevel Value="100" />
            <BrowserZoomLevel Value="100" />
            <FollowBehavior Value="1" />
        </LookFeel>
        <Audio>
            <DriverType Value="ASIO" />
            <BufferSize Value="512" />
            <SampleRate Value="48000" />
        </Audio>
        <Record>
            <FileType Value="WAV" />
            <BitDepth Value="24" />
            <CountIn Value="1" />
        </Record>
        <CPU>
            <MultiCore Value="1" />
        </CPU>
        <Misc>
            <CreateAnalysisFiles Value="1" />
            <SendUsageData Value="0" />
            <ShowSplash Value="0" />
        </Misc>
    </Preferences>
</Ableton>
'@
$prefs | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "Preferences written to: $prefsPath"

# ============================================================================
# SAMPLE PRESET
# ============================================================================
Write-Step "Writing sample audio effect preset..."

$presetPath2 = Join-Path $presetsDir "Endstate Warm Reverb.adv"
$preset = @'
<?xml version="1.0" encoding="UTF-8"?>
<Ableton MajorVersion="5" MinorVersion="12.0" Creator="Endstate Seed">
    <Reverb>
        <DecayTime Value="3.5" />
        <RoomSize Value="70" />
        <PreDelay Value="20" />
        <HighCut Value="8000" />
        <LowCut Value="200" />
        <DryWet Value="25" />
        <EarlyReflections Value="40" />
        <Diffusion Value="80" />
        <Quality Value="High" />
    </Reverb>
</Ableton>
'@
$preset | Set-Content -Path $presetPath2 -Encoding UTF8
Write-Pass "Preset written to: $presetPath2"

# ============================================================================
# DEFAULT SETTINGS
# ============================================================================
Write-Step "Writing default settings..."

$defaultPath = Join-Path $defaultsDir "Dropping Samples.txt"
$default = @"
Ableton Live Default Settings - Endstate Seed
Warp Mode: Complex Pro
Gain: 0 dB
Transpose: 0 st
Detune: 0 ct
RAM Mode: Off
HiQ: On
"@
$default | Set-Content -Path $defaultPath -Encoding UTF8
Write-Pass "Defaults written to: $defaultPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath, $presetPath2, $defaultPath)
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
Write-Host " Ableton Live Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
