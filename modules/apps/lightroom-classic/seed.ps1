<#
.SYNOPSIS
    Seeds meaningful Lightroom Classic configuration for curation testing.

.DESCRIPTION
    Sets up Lightroom Classic configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Preferences (agprefs file)
    - Develop Presets (sample XMP preset)
    - Export Presets (sample export preset)
    - Filename Templates (sample template)

    DOES NOT configure:
    - Adobe account credentials
    - Catalog files (.lrcat)
    - Cached previews

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
Write-Host " Lightroom Classic Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$lrDir = Join-Path $env:APPDATA "Adobe\Lightroom"
$prefsDir = Join-Path $lrDir "Preferences"
$developPresetsDir = Join-Path $lrDir "Develop Presets\User Presets"
$exportPresetsDir = Join-Path $lrDir "Export Presets"
$filenameTemplatesDir = Join-Path $lrDir "Filename Templates"

$prefsPath = Join-Path $prefsDir "Lightroom 6 Preferences.agprefs"

# Ensure directories exist
foreach ($dir in @($prefsDir, $developPresetsDir, $exportPresetsDir, $filenameTemplatesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# PREFERENCES
# ============================================================================
Write-Step "Writing preferences..."

$prefs = @'
AgPreferences_0_2 = {
    catalog = {
        lastCatalog = "C:\\Users\\placeholder\\Pictures\\Lightroom\\Lightroom Catalog.lrcat",
    },
    develop = {
        defaultDevelopSettings = "Adobe Default",
        autoToneEnabled = true,
    },
    export = {
        lastExportDirectory = "C:\\Users\\placeholder\\Pictures\\Exports",
    },
    general = {
        language = "en",
        showSplashScreen = false,
        checkForUpdates = true,
    },
    import = {
        showImportDialog = true,
    },
    performance = {
        useGraphicsProcessor = true,
        cacheSize = 20,
    },
}
'@

$prefs | Set-Content -Path $prefsPath -Encoding UTF8

Write-Pass "Preferences written to: $prefsPath"

# ============================================================================
# DEVELOP PRESET
# ============================================================================
Write-Step "Writing sample develop preset..."

$developPresetPath = Join-Path $developPresetsDir "Endstate Sample Preset.xmp"

$developPreset = @'
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="Adobe XMP Core 7.0-c000 1.000000, 0000/00/00-00:00:00">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
    crs:PresetType="Normal"
    crs:Cluster=""
    crs:UUID="placeholder-uuid-0000-0000-000000000001"
    crs:SupportsAmount="False"
    crs:SupportsColor="True"
    crs:SupportsMonochrome="True"
    crs:SupportsHighDynamicRange="True"
    crs:SupportsNormalDynamicRange="True"
    crs:SupportsSceneReferred="True"
    crs:SupportsOutputReferred="True"
    crs:CameraModelRestriction=""
    crs:Copyright=""
    crs:ContactInfo=""
    crs:Version="15.0"
    crs:ProcessVersion="11.0"
    crs:Exposure2012="+0.50"
    crs:Contrast2012="+10"
    crs:Highlights2012="-20"
    crs:Shadows2012="+15"
    crs:Whites2012="+5"
    crs:Blacks2012="-5"
    crs:Clarity2012="+10"
    crs:Vibrance="+15"
    crs:Saturation="0">
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
'@

$developPreset | Set-Content -Path $developPresetPath -Encoding UTF8

Write-Pass "Develop preset written to: $developPresetPath"

# ============================================================================
# EXPORT PRESET
# ============================================================================
Write-Step "Writing sample export preset..."

$exportPresetPath = Join-Path $exportPresetsDir "Endstate Web Export.lrtemplate"

$exportPreset = @'
s = {
    id = "placeholder-export-0000-0000-000000000001",
    internalName = "Endstate Web Export",
    title = "Endstate Web Export",
    type = "Export",
    value = {
        collisionHandling = "ask",
        export_colorSpace = "sRGB",
        export_compression = 80,
        export_destinationType = "specificFolder",
        export_format = "JPEG",
        export_postProcessing = "doNothing",
        export_reimportExportedPhoto = false,
        export_selectedTextFontSize = 12,
        export_useSubfolder = false,
        export_videoFormat = "4dbf4aef-564d-4938-a4f2-f5d7f4e38f4f",
        size_maxHeight = 2048,
        size_maxWidth = 2048,
        size_resolution = 72,
        size_resolutionUnits = "inch",
        size_units = "pixels",
    },
}
'@

$exportPreset | Set-Content -Path $exportPresetPath -Encoding UTF8

Write-Pass "Export preset written to: $exportPresetPath"

# ============================================================================
# FILENAME TEMPLATE
# ============================================================================
Write-Step "Writing sample filename template..."

$filenameTemplatePath = Join-Path $filenameTemplatesDir "Endstate Date-Name.lrtemplate"

$filenameTemplate = @'
s = {
    id = "placeholder-filename-0000-0000-000000000001",
    internalName = "Endstate Date-Name",
    title = "Endstate Date-Name",
    type = "Filename",
    value = {
        tokens = "{{date_YYYY}}-{{date_MM}}-{{date_DD}}_{{image_name}}",
    },
}
'@

$filenameTemplate | Set-Content -Path $filenameTemplatePath -Encoding UTF8

Write-Pass "Filename template written to: $filenameTemplatePath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath, $developPresetPath, $exportPresetPath, $filenameTemplatePath)
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
Write-Host " Lightroom Classic Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $prefsPath" -ForegroundColor Gray
Write-Host "  - $developPresetPath" -ForegroundColor Gray
Write-Host "  - $exportPresetPath" -ForegroundColor Gray
Write-Host "  - $filenameTemplatePath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
