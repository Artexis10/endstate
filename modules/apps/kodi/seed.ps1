<#
.SYNOPSIS
    Seeds meaningful Kodi configuration for curation testing.

.DESCRIPTION
    Sets up Kodi configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - userdata/guisettings.xml (GUI settings)
    - userdata/sources.xml (media sources)
    - userdata/keymaps/endstate.xml (custom keymaps)
    - userdata/advancedsettings.xml (advanced settings)

    DOES NOT configure:
    - Database files
    - Addon binaries
    - Thumbnails

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
Write-Host " Kodi Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$kodiDir = Join-Path $env:APPDATA "Kodi"
$userdataDir = Join-Path $kodiDir "userdata"
$keymapsDir = Join-Path $userdataDir "keymaps"

foreach ($dir in @($userdataDir, $keymapsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# GUISETTINGS.XML
# ============================================================================
Write-Step "Writing userdata/guisettings.xml..."

$guiPath = Join-Path $userdataDir "guisettings.xml"
$gui = @'
<settings version="2">
  <setting id="locale.language">resource.language.en_gb</setting>
  <setting id="locale.country">225</setting>
  <setting id="locale.timezonecountry">41</setting>
  <setting id="locale.timezone">15</setting>
  <setting id="locale.use24hourclock">true</setting>
  <setting id="locale.temperatureunit">celsius</setting>
  <setting id="lookandfeel.skin">skin.estuary</setting>
  <setting id="lookandfeel.skincolors">SKINDEFAULT</setting>
  <setting id="lookandfeel.font">Default</setting>
  <setting id="lookandfeel.enablerssfeeds">false</setting>
  <setting id="videoplayer.adjustrefreshrate">0</setting>
  <setting id="videoplayer.usedisplayasclock">false</setting>
  <setting id="videoplayer.rendermethod">0</setting>
  <setting id="audioplayer.crossfade">3</setting>
  <setting id="audioplayer.replaygaintype">1</setting>
  <setting id="services.webserver">true</setting>
  <setting id="services.webserverport">8080</setting>
  <setting id="services.upnp">true</setting>
  <setting id="services.zeroconf">true</setting>
  <setting id="filelists.showextensions">true</setting>
  <setting id="filelists.showparentdiritems">true</setting>
  <setting id="screensaver.mode">screensaver.xbmc.builtin.dim</setting>
  <setting id="screensaver.time">5</setting>
</settings>
'@
$gui | Set-Content -Path $guiPath -Encoding UTF8
Write-Pass "guisettings.xml written"

# ============================================================================
# SOURCES.XML
# ============================================================================
Write-Step "Writing userdata/sources.xml..."

$sourcesPath = Join-Path $userdataDir "sources.xml"
$sources = @'
<sources>
  <video>
    <default pathversion="1"></default>
    <source>
      <name>Movies</name>
      <path pathversion="1">D:\Media\Movies\</path>
      <allowsharing>true</allowsharing>
    </source>
    <source>
      <name>TV Shows</name>
      <path pathversion="1">D:\Media\TV\</path>
      <allowsharing>true</allowsharing>
    </source>
  </video>
  <music>
    <default pathversion="1"></default>
    <source>
      <name>Music Library</name>
      <path pathversion="1">D:\Media\Music\</path>
      <allowsharing>true</allowsharing>
    </source>
  </music>
  <pictures>
    <default pathversion="1"></default>
    <source>
      <name>Photos</name>
      <path pathversion="1">D:\Media\Photos\</path>
      <allowsharing>true</allowsharing>
    </source>
  </pictures>
</sources>
'@
$sources | Set-Content -Path $sourcesPath -Encoding UTF8
Write-Pass "sources.xml written"

# ============================================================================
# KEYMAPS
# ============================================================================
Write-Step "Writing userdata/keymaps/endstate.xml..."

$keymapPath = Join-Path $keymapsDir "endstate.xml"
$keymap = @'
<keymap>
  <global>
    <keyboard>
      <backspace>Back</backspace>
      <escape>PreviousMenu</escape>
      <return>Select</return>
      <space>Pause</space>
      <f>ToggleFullscreen</f>
      <m>Mute</m>
      <s>Screenshot</s>
      <i>Info</i>
    </keyboard>
  </global>
  <FullscreenVideo>
    <keyboard>
      <left>StepBack</left>
      <right>StepForward</right>
      <up>BigStepForward</up>
      <down>BigStepBack</down>
      <a>AudioNextLanguage</a>
      <t>NextSubtitle</t>
    </keyboard>
  </FullscreenVideo>
</keymap>
'@
$keymap | Set-Content -Path $keymapPath -Encoding UTF8
Write-Pass "Keymap written"

# ============================================================================
# ADVANCEDSETTINGS.XML
# ============================================================================
Write-Step "Writing userdata/advancedsettings.xml..."

$advPath = Join-Path $userdataDir "advancedsettings.xml"
$adv = @'
<advancedsettings>
  <cache>
    <buffermode>1</buffermode>
    <memorysize>104857600</memorysize>
    <readfactor>4</readfactor>
  </cache>
  <network>
    <curlclienttimeout>30</curlclienttimeout>
    <curllowspeedtime>30</curllowspeedtime>
    <curlretries>3</curlretries>
  </network>
  <gui>
    <algorithmdirtyregions>3</algorithmdirtyregions>
    <nofliptimeout>0</nofliptimeout>
  </gui>
</advancedsettings>
'@
$adv | Set-Content -Path $advPath -Encoding UTF8
Write-Pass "advancedsettings.xml written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($guiPath, $sourcesPath, $keymapPath, $advPath)
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
Write-Host " Kodi Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
