<#
.SYNOPSIS
    Seeds meaningful REAPER configuration for curation testing.

.DESCRIPTION
    Sets up REAPER configuration files with representative non-default values
    WITHOUT creating any license keys or credentials. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - reaper.ini (main configuration)
    - reaper-kb.ini (keybindings)
    - Effects/ (sample JSFX effect)
    - Scripts/ (sample ReaScript)

    DOES NOT configure:
    - License/registration files
    - Plugin scan caches
    - Auto-save files

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
Write-Host " REAPER Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$reaperDir = Join-Path $env:APPDATA "REAPER"
$reaperIniPath = Join-Path $reaperDir "reaper.ini"
$reaperKbPath = Join-Path $reaperDir "reaper-kb.ini"
$effectsDir = Join-Path $reaperDir "Effects\Endstate"
$scriptsDir = Join-Path $reaperDir "Scripts\Endstate"

# Ensure directories exist
foreach ($dir in @($reaperDir, $effectsDir, $scriptsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# REAPER.INI
# ============================================================================
Write-Step "Writing reaper.ini..."

$reaperIni = @"
[REAPER]
verchk=1
autosave=1
autosaveint=300
autosaveonrender=1
showlastfn=1
maxrecentfn=20
defrecpath=C:\Users\placeholder\Music\REAPER Media
defrenderpath=C:\Users\placeholder\Music\REAPER Renders
templatefn=
lastproject=C:\Users\placeholder\Music\REAPER\placeholder.RPP
openalikealikepos=1
peakscl=1

[audioconfig]
driver=ASIO
asio_driver=placeholder ASIO Driver
asio_bsize=512
srate=48000
bps=24

[projdefaults]
defpitchcfg=elastique 3.3.3 Pro
samplerate=48000
bpm=120
tsnum=4
tsden=4

[midi]
midieditor_zoom_h=0.5
midieditor_zoom_v=0.5
midieditor_defnotesize=1
midieditor_quantize=0.25

[appearance]
theme=Default_6.0
ui_scale=1.0
tint_amount=0
track_height_default=60
mixer_visible=1
transport_visible=1

[render]
format=wav
wavfmt=3
srate=48000
channels=2
dither=1
normalize=0
tail=0
tailms=1000
"@

$reaperIni | Set-Content -Path $reaperIniPath -Encoding UTF8

Write-Pass "reaper.ini written to: $reaperIniPath"

# ============================================================================
# REAPER-KB.INI (keybindings)
# ============================================================================
Write-Step "Writing reaper-kb.ini..."

$reaperKb = @"
# REAPER Key Bindings - Endstate Seed
# Format: KEY <flags> <key> <command_id> <section>
# Common custom keybindings

# Transport
KEY 0 32 40044 0
# Space = Play/Stop

KEY 0 82 40015 0
# R = Toggle Record

KEY 8 83 40026 0
# Ctrl+S = Save project

KEY 8 90 40029 0
# Ctrl+Z = Undo

KEY 9 90 40030 0
# Ctrl+Shift+Z = Redo

KEY 8 68 40697 0
# Ctrl+D = Duplicate items

KEY 0 83 40135 0
# S = Split items at cursor

KEY 8 65 40296 0
# Ctrl+A = Select all items

KEY 8 77 40917 0
# Ctrl+M = Toggle metronome

KEY 8 84 40218 0
# Ctrl+T = Insert new track
"@

$reaperKb | Set-Content -Path $reaperKbPath -Encoding UTF8

Write-Pass "reaper-kb.ini written to: $reaperKbPath"

# ============================================================================
# SAMPLE JSFX EFFECT
# ============================================================================
Write-Step "Writing sample JSFX effect..."

$jsfxPath = Join-Path $effectsDir "endstate_gain.jsfx"

$jsfx = @"
desc:Endstate Simple Gain
//tags: utility gain volume
//author: Endstate Seed

slider1:0<-60,12,0.1>Gain (dB)

@init
gain = 1;

@slider
gain = 10^(slider1/20);

@sample
spl0 *= gain;
spl1 *= gain;
"@

$jsfx | Set-Content -Path $jsfxPath -Encoding UTF8

Write-Pass "JSFX effect written to: $jsfxPath"

# ============================================================================
# SAMPLE REASCRIPT
# ============================================================================
Write-Step "Writing sample ReaScript..."

$scriptPath = Join-Path $scriptsDir "endstate_select_all_items.lua"

$script = @"
-- Endstate Sample ReaScript
-- Selects all items in the current project

reaper.Main_OnCommand(40296, 0) -- Select all items
reaper.UpdateArrange()

local count = reaper.CountSelectedMediaItems(0)
reaper.ShowConsoleMsg("Selected " .. count .. " items\n")
"@

$script | Set-Content -Path $scriptPath -Encoding UTF8

Write-Pass "ReaScript written to: $scriptPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($reaperIniPath, $reaperKbPath, $jsfxPath, $scriptPath)
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
Write-Host " REAPER Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $reaperIniPath" -ForegroundColor Gray
Write-Host "  - $reaperKbPath" -ForegroundColor Gray
Write-Host "  - $jsfxPath" -ForegroundColor Gray
Write-Host "  - $scriptPath" -ForegroundColor Gray
Write-Host ""
Write-Step "Excluded (sensitive):"
Write-Host "  - reaper-reginfo2.ini (license)" -ForegroundColor DarkYellow
Write-Host "  - reaper-license.rk (license key)" -ForegroundColor DarkYellow
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
