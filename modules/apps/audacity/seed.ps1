<#
.SYNOPSIS
    Seeds meaningful Audacity configuration for curation testing.

.DESCRIPTION
    Sets up Audacity configuration files with representative non-default values.
    Used by the curation workflow to generate representative config files
    for module validation.

    Configures:
    - audacity.cfg (application preferences)
    - Macros/ (sample macro chain)

    DOES NOT configure:
    - Plugin registry (rebuilt on scan)
    - Session/AutoSave data

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
Write-Host " Audacity Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$audacityDir = Join-Path $env:APPDATA "audacity"
$cfgPath = Join-Path $audacityDir "audacity.cfg"
$macrosDir = Join-Path $audacityDir "Macros"

foreach ($dir in @($audacityDir, $macrosDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# AUDACITY.CFG
# ============================================================================
Write-Step "Writing audacity.cfg..."

$cfg = @"
[AudioIO]
PlaybackDevice=Windows WASAPI: Speakers
RecordingDevice=Windows WASAPI: Microphone
RecordChannels=1
SoundActivatedRecord=0

[SamplingRate]
DefaultProjectSampleRate=48000
DefaultProjectSampleFormat=32-bit float

[Quality]
DefaultSampleRate=48000
DefaultSampleFormat=32-bit float
HighQualityDither=Shaped
LowQualityDither=Rectangle

[GUI]
Theme=dark
Language=en
ShowSplashScreen=0
ShowExtraMenus=0

[Directories]
TempDir=C:\Users\placeholder\AppData\Local\Temp\audacity_temp

[FileFormats]
ExportDownMix=1
DefaultExportRate=48000

[Tracks]
DefaultViewMode=Waveform
TracksFitVerticallyZoomed=0

[Noise]
WindowSize=2048
StepsPerWindow=4

[Spectrum]
FFTSize=2048
WindowType=Hanning
MaxFreq=22050
MinFreq=0

[Effects]
RealtimeEffects=1

[Recording]
Latency=100
SoundActivatedLevel=-26

[Update]
DefaultUpdatesChecking=0
"@
$cfg | Set-Content -Path $cfgPath -Encoding UTF8
Write-Pass "audacity.cfg written to: $cfgPath"

# ============================================================================
# SAMPLE MACRO
# ============================================================================
Write-Step "Writing sample macro..."

$macroPath = Join-Path $macrosDir "Endstate Podcast Cleanup.txt"
$macro = @"
Normalize:PeakLevel=-1.0
TruncateSilence:Threshold=-40 Action=0 Minimum=0.3 Truncate=0.3
Compressor:Threshold=-18 NoiseFloor=-40 Ratio=3 AttackTime=0.2 ReleaseTime=1.0
Equalization:FilterLength=4001 CurveName=Endstate Voice
Normalize:PeakLevel=-3.0
ExportAsMP3:
"@
$macro | Set-Content -Path $macroPath -Encoding UTF8
Write-Pass "Macro written to: $macroPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($cfgPath, $macroPath)
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
Write-Host " Audacity Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
