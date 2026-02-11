<#
.SYNOPSIS
    Seeds meaningful mpv configuration for curation testing.

.DESCRIPTION
    Sets up mpv configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - mpv.conf (player configuration)
    - input.conf (input bindings)
    - scripts/ (user scripts placeholder)

    DOES NOT configure:
    - Watch-later resume data
    - Cache files

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
Write-Host " mpv Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$mpvDir = Join-Path $env:APPDATA "mpv"
$scriptsDir = Join-Path $mpvDir "scripts"

foreach ($dir in @($mpvDir, $scriptsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# MPV.CONF
# ============================================================================
Write-Step "Writing mpv.conf..."

$mpvConfPath = Join-Path $mpvDir "mpv.conf"
$mpvConf = @'
# mpv configuration - Endstate seed
# Video
profile=gpu-hq
gpu-api=vulkan
hwdec=auto-copy
vo=gpu-next

# Audio
volume=80
volume-max=150
audio-file-auto=fuzzy

# Subtitles
sub-auto=fuzzy
sub-font="Segoe UI"
sub-font-size=42
sub-border-size=2
sub-shadow-offset=1
sub-shadow-color="#33000000"

# OSD
osd-font="Segoe UI"
osd-font-size=32
osd-duration=2000
osd-bar=yes

# Window
keep-open=yes
autofit-larger=90%x90%
cursor-autohide=1000
force-window=immediate

# Screenshot
screenshot-format=png
screenshot-png-compression=7
screenshot-directory=~~desktop/

# Cache
cache=yes
demuxer-max-bytes=512MiB
demuxer-max-back-bytes=128MiB

# Misc
save-position-on-quit=yes
watch-later-options-clr=
'@
$mpvConf | Set-Content -Path $mpvConfPath -Encoding UTF8
Write-Pass "mpv.conf written"

# ============================================================================
# INPUT.CONF
# ============================================================================
Write-Step "Writing input.conf..."

$inputConfPath = Join-Path $mpvDir "input.conf"
$inputConf = @'
# mpv input bindings - Endstate seed

# Seek
RIGHT seek  5
LEFT  seek -5
UP    seek  60
DOWN  seek -60
Shift+RIGHT seek  1 exact
Shift+LEFT  seek -1 exact

# Volume
WHEEL_UP    add volume  2
WHEEL_DOWN  add volume -2

# Playback speed
[ multiply speed 0.9091
] multiply speed 1.1
BS set speed 1.0

# Subtitle
v cycle sub-visibility
j cycle sub
J cycle sub down

# Audio
a cycle audio

# Screenshot
s screenshot
S screenshot video

# Playlist
< playlist-prev
> playlist-next

# Toggle
f cycle fullscreen
m cycle mute
i script-binding stats/display-stats-toggle
'@
$inputConf | Set-Content -Path $inputConfPath -Encoding UTF8
Write-Pass "input.conf written"

# ============================================================================
# SCRIPTS (placeholder)
# ============================================================================
Write-Step "Writing scripts/autoload.lua (example user script)..."

$scriptPath = Join-Path $scriptsDir "autoload.lua"
$script = @'
-- autoload.lua - Endstate seed placeholder
-- Auto-loads files in the same directory for playlist
-- This is a minimal placeholder for curation testing

local msg = require 'mp.msg'

mp.register_event("file-loaded", function()
    msg.info("autoload: file loaded")
end)
'@
$script | Set-Content -Path $scriptPath -Encoding UTF8
Write-Pass "autoload.lua written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($mpvConfPath, $inputConfPath, $scriptPath)
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
Write-Host " mpv Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
