<#
.SYNOPSIS
    Seeds meaningful Capture One configuration for curation testing.

.DESCRIPTION
    Sets up Capture One configuration files with representative non-default values
    WITHOUT creating any license data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Preferences.plist (app preferences)
    - Styles/ (sample processing style)
    - KeyboardShortcuts/ (sample shortcut set)
    - Workspaces/ (sample workspace layout)

    DOES NOT configure:
    - License files
    - Catalog databases
    - Cache data

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
Write-Host " Capture One Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$c1Dir = Join-Path $env:LOCALAPPDATA "CaptureOne"
$stylesDir = Join-Path $c1Dir "Styles"
$shortcutsDir = Join-Path $c1Dir "KeyboardShortcuts"
$workspacesDir = Join-Path $c1Dir "Workspaces"
$prefsPath = Join-Path $c1Dir "Preferences.plist"

foreach ($dir in @($c1Dir, $stylesDir, $shortcutsDir, $workspacesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# PREFERENCES
# ============================================================================
Write-Step "Writing Preferences.plist..."

$prefs = @'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>General.Language</key>
    <string>en</string>
    <key>General.CheckForUpdates</key>
    <true/>
    <key>General.ShowWelcomeScreen</key>
    <false/>
    <key>Appearance.Theme</key>
    <string>Dark</string>
    <key>Capture.DefaultFormat</key>
    <string>IIQ</string>
    <key>Export.DefaultFormat</key>
    <string>JPEG</string>
    <key>Export.DefaultQuality</key>
    <integer>85</integer>
    <key>Export.DefaultColorProfile</key>
    <string>sRGB</string>
    <key>Performance.HardwareAcceleration</key>
    <true/>
    <key>Performance.CacheSize</key>
    <integer>10240</integer>
</dict>
</plist>
'@
$prefs | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "Preferences written to: $prefsPath"

# ============================================================================
# SAMPLE STYLE
# ============================================================================
Write-Step "Writing sample style..."

$stylePath = Join-Path $stylesDir "Endstate Natural.costyle"
$style = @'
<?xml version="1.0" encoding="UTF-8"?>
<CaptureOneStyle version="1">
    <Name>Endstate Natural</Name>
    <Description>Clean natural look with subtle adjustments</Description>
    <Adjustments>
        <Exposure>0.30</Exposure>
        <Contrast>5</Contrast>
        <Brightness>3</Brightness>
        <Saturation>-5</Saturation>
        <Clarity>10</Clarity>
        <HighlightRecovery>15</HighlightRecovery>
        <ShadowRecovery>10</ShadowRecovery>
        <Sharpening>
            <Amount>120</Amount>
            <Radius>0.8</Radius>
            <Threshold>1</Threshold>
        </Sharpening>
    </Adjustments>
</CaptureOneStyle>
'@
$style | Set-Content -Path $stylePath -Encoding UTF8
Write-Pass "Style written to: $stylePath"

# ============================================================================
# SAMPLE KEYBOARD SHORTCUTS
# ============================================================================
Write-Step "Writing sample keyboard shortcuts..."

$shortcutPath = Join-Path $shortcutsDir "Endstate.coshortcuts"
$shortcuts = @'
<?xml version="1.0" encoding="UTF-8"?>
<KeyboardShortcuts name="Endstate" version="1">
    <Shortcut action="ToggleFullScreen" key="F11" />
    <Shortcut action="ZoomToFit" key="Ctrl+0" />
    <Shortcut action="Zoom100" key="Ctrl+1" />
    <Shortcut action="ToggleBeforeAfter" key="Y" />
    <Shortcut action="AutoAdjust" key="Ctrl+Shift+A" />
    <Shortcut action="ResetAdjustments" key="Ctrl+R" />
    <Shortcut action="ExportSelected" key="Ctrl+Shift+E" />
    <Shortcut action="NextImage" key="Right" />
    <Shortcut action="PrevImage" key="Left" />
</KeyboardShortcuts>
'@
$shortcuts | Set-Content -Path $shortcutPath -Encoding UTF8
Write-Pass "Keyboard shortcuts written to: $shortcutPath"

# ============================================================================
# SAMPLE WORKSPACE
# ============================================================================
Write-Step "Writing sample workspace..."

$workspacePath = Join-Path $workspacesDir "Endstate Editing.coworkspace"
$workspace = @'
<?xml version="1.0" encoding="UTF-8"?>
<Workspace name="Endstate Editing" version="1">
    <Layout>
        <Panel name="Browser" position="bottom" height="200" />
        <Panel name="Tools" position="right" width="300" />
        <Panel name="Viewer" position="center" />
    </Layout>
    <ToolTabs>
        <Tab name="Exposure" />
        <Tab name="Color" />
        <Tab name="Details" />
        <Tab name="Lens" />
    </ToolTabs>
</Workspace>
'@
$workspace | Set-Content -Path $workspacePath -Encoding UTF8
Write-Pass "Workspace written to: $workspacePath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath, $stylePath, $shortcutPath, $workspacePath)
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
Write-Host " Capture One Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
