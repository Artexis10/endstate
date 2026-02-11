<#
.SYNOPSIS
    Seeds meaningful foobar2000 configuration for curation testing.

.DESCRIPTION
    Sets up foobar2000 configuration files with representative non-default values.
    Used by the curation workflow to generate representative config files
    for module validation.

    Configures:
    - configuration/ (layout and component settings as INI-like files)
    - playlists-v1.4/ (sample playlist)

    DOES NOT configure:
    - Media library index (rebuilt on scan)
    - Component DLLs

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
Write-Host " foobar2000 Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$fb2kDir = Join-Path $env:APPDATA "foobar2000"
$configDir = Join-Path $fb2kDir "configuration"
$playlistsDir = Join-Path $fb2kDir "playlists-v1.4"

foreach ($dir in @($configDir, $playlistsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# CORE CONFIGURATION
# ============================================================================
Write-Step "Writing core configuration..."

$coreCfgPath = Join-Path $configDir "core.cfg"
$coreCfg = @"
[Core]
output_device=WASAPI (Exclusive): Speakers
buffer_length=1000
thread_priority=above_normal

[Playback]
replaygain_mode=track
replaygain_preamp=0
replaygain_preamp_norg=-6
crossfade_on_seek=0
fade_in=0
fade_out=0

[Display]
show_systray=1
minimize_to_tray=1
always_on_top=0
snap_to_edges=1

[Library]
music_folders=C:\Users\placeholder\Music
"@
$coreCfg | Set-Content -Path $coreCfgPath -Encoding UTF8
Write-Pass "Core config written to: $coreCfgPath"

# ============================================================================
# DSP CONFIGURATION
# ============================================================================
Write-Step "Writing DSP configuration..."

$dspCfgPath = Join-Path $configDir "dsp.cfg"
$dspCfg = @"
[DSP Chain]
active=1
name=Endstate Default

[DSP.0]
type=Resampler (PPHS)
target_rate=48000
quality=high

[DSP.1]
type=Equalizer
enabled=1
band_count=10
band_0=0.0
band_1=0.5
band_2=1.0
band_3=1.5
band_4=0.5
band_5=0.0
band_6=-0.5
band_7=-1.0
band_8=0.0
band_9=0.5
"@
$dspCfg | Set-Content -Path $dspCfgPath -Encoding UTF8
Write-Pass "DSP config written to: $dspCfgPath"

# ============================================================================
# SAMPLE PLAYLIST
# ============================================================================
Write-Step "Writing sample playlist..."

$playlistPath = Join-Path $playlistsDir "00000000.fpl"
# Create a minimal text-based placeholder (real .fpl is binary)
$playlist = @"
# foobar2000 Playlist Placeholder - Endstate Seed
# Real playlists are binary format; this is a representative placeholder
# Playlist: Endstate Favorites
# Track count: 0 (placeholder)
"@
$playlist | Set-Content -Path $playlistPath -Encoding UTF8
Write-Pass "Playlist placeholder written to: $playlistPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($coreCfgPath, $dspCfgPath, $playlistPath)
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
Write-Host " foobar2000 Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
