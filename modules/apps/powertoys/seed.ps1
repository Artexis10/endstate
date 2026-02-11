<#
.SYNOPSIS
    Seeds meaningful PowerToys configuration for curation testing.

.DESCRIPTION
    Sets up PowerToys configuration that represents real-world user preferences.
    Used by the curation workflow to generate representative config files for
    module validation.

    Configures:
    - Main settings.json (enabled modules)
    - FancyZones layout settings
    - PowerRename preferences
    - File Explorer add-ons

    DOES NOT configure:
    - Keyboard Manager remappings (machine-specific)
    - Run plugin API keys

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
Write-Host " PowerToys Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$powerToysDir = Join-Path $env:LOCALAPPDATA "Microsoft\PowerToys"
$settingsPath = Join-Path $powerToysDir "settings.json"

# Ensure directory exists
if (-not (Test-Path $powerToysDir)) {
    Write-Step "Creating PowerToys directory..."
    New-Item -ItemType Directory -Path $powerToysDir -Force | Out-Null
}

# ============================================================================
# MAIN SETTINGS.JSON
# ============================================================================
Write-Step "Writing main settings.json..."

$mainSettings = @{
    general = @{
        startup = $true
        enabled = @{
            "Always On Top" = $true
            "Awake" = $false
            "Color Picker" = $true
            "CropAndLock" = $false
            "Environment Variables" = $true
            "FancyZones" = $true
            "File Explorer" = $true
            "File Locksmith" = $true
            "Hosts File Editor" = $false
            "Image Resizer" = $true
            "Keyboard Manager" = $true
            "Mouse Without Borders" = $false
            "Mouse Utilities" = $true
            "Paste As Plain Text" = $true
            "Peek" = $true
            "PowerRename" = $true
            "PowerToys Run" = $true
            "Quick Accent" = $false
            "Registry Preview" = $false
            "Screen Ruler" = $true
            "Shortcut Guide" = $true
            "Text Extractor" = $true
            "Video Conference Mute" = $false
        }
        theme = "system"
        run_elevated = $false
        download_updates_automatically = $true
        show_new_updates_toast_notification = $true
    }
}

$mainSettingsJson = $mainSettings | ConvertTo-Json -Depth 10
$mainSettingsJson | Set-Content -Path $settingsPath -Encoding UTF8

Write-Pass "Main settings written to: $settingsPath"

# ============================================================================
# FANCYZONES SETTINGS
# ============================================================================
Write-Step "Writing FancyZones settings..."

$fancyZonesDir = Join-Path $powerToysDir "FancyZones"
if (-not (Test-Path $fancyZonesDir)) {
    New-Item -ItemType Directory -Path $fancyZonesDir -Force | Out-Null
}

$fancyZonesSettings = @{
    properties = @{
        "fancyzones_shiftDrag" = @{ value = $true }
        "fancyzones_mouseSwitch" = @{ value = $false }
        "fancyzones_overrideSnapHotkeys" = @{ value = $false }
        "fancyzones_moveWindowAcrossMonitors" = @{ value = $false }
        "fancyzones_moveWindowsBasedOnPosition" = @{ value = $true }
        "fancyzones_overlappingZonesAlgorithm" = @{ value = 0 }
        "fancyzones_displayChange_moveWindows" = @{ value = $true }
        "fancyzones_zoneSetChange_flashZones" = @{ value = $false }
        "fancyzones_zoneSetChange_moveWindows" = @{ value = $true }
        "fancyzones_appLastZone_moveWindows" = @{ value = $true }
        "fancyzones_openWindowOnActiveMonitor" = @{ value = $false }
        "fancyzones_restoreSize" = @{ value = $false }
        "fancyzones_quickLayoutSwitch" = @{ value = $true }
        "fancyzones_flashZonesOnQuickSwitch" = @{ value = $true }
        "fancyzones_use_cursorpos_editor_startupscreen" = @{ value = $true }
        "fancyzones_show_on_all_monitors" = @{ value = $false }
        "fancyzones_span_zones_across_monitors" = @{ value = $false }
        "fancyzones_makeDraggedWindowTransparent" = @{ value = $true }
        "fancyzones_allowPopupWindowSnap" = @{ value = $false }
        "fancyzones_allowChildWindowSnap" = @{ value = $false }
        "fancyzones_disableRoundCornersOnSnap" = @{ value = $false }
        "fancyzones_zoneHighlightColor" = @{ value = "#0078D4" }
        "fancyzones_zoneColor" = @{ value = "#F5FCFF" }
        "fancyzones_zoneBorderColor" = @{ value = "#FFFFFF" }
        "fancyzones_zoneNumberColor" = @{ value = "#000000" }
        "fancyzones_editor_hotkey" = @{ value = @{ win = $true; ctrl = $false; alt = $false; shift = $false; code = 192; key = "~" } }
        "fancyzones_windowSwitching" = @{ value = $true }
        "fancyzones_nextTab_hotkey" = @{ value = @{ win = $true; ctrl = $false; alt = $false; shift = $false; code = 34; key = "Page Down" } }
        "fancyzones_prevTab_hotkey" = @{ value = @{ win = $true; ctrl = $false; alt = $false; shift = $false; code = 33; key = "Page Up" } }
        "fancyzones_excluded_apps" = @{ value = "" }
        "fancyzones_highlight_opacity" = @{ value = 50 }
    }
    name = "FancyZones"
    version = "1.0"
}

$fancyZonesPath = Join-Path $fancyZonesDir "settings.json"
$fancyZonesSettings | ConvertTo-Json -Depth 10 | Set-Content -Path $fancyZonesPath -Encoding UTF8

Write-Pass "FancyZones settings written to: $fancyZonesPath"

# ============================================================================
# POWERRENAME SETTINGS
# ============================================================================
Write-Step "Writing PowerRename settings..."

$powerRenameDir = Join-Path $powerToysDir "PowerRename"
if (-not (Test-Path $powerRenameDir)) {
    New-Item -ItemType Directory -Path $powerRenameDir -Force | Out-Null
}

$powerRenameSettings = @{
    properties = @{
        "powerrename_use_boost_lib" = @{ value = $false }
        "powerrename_mru_enabled" = @{ value = $true }
        "powerrename_mru_max_size" = @{ value = 10 }
        "powerrename_show_icon_on_menu" = @{ value = $true }
        "powerrename_extended_menu_only" = @{ value = $true }
        "powerrename_persistent_input_enabled" = @{ value = $true }
    }
    name = "PowerRename"
    version = "1.0"
}

$powerRenamePath = Join-Path $powerRenameDir "settings.json"
$powerRenameSettings | ConvertTo-Json -Depth 10 | Set-Content -Path $powerRenamePath -Encoding UTF8

Write-Pass "PowerRename settings written to: $powerRenamePath"

# ============================================================================
# IMAGE RESIZER SETTINGS
# ============================================================================
Write-Step "Writing Image Resizer settings..."

$imageResizerDir = Join-Path $powerToysDir "ImageResizer"
if (-not (Test-Path $imageResizerDir)) {
    New-Item -ItemType Directory -Path $imageResizerDir -Force | Out-Null
}

$imageResizerSettings = @{
    properties = @{
        "imageresizer_selectedSizeIndex" = @{ value = 1 }
        "imageresizer_shrinkOnly" = @{ value = $false }
        "imageresizer_replace" = @{ value = $false }
        "imageresizer_ignoreOrientation" = @{ value = $true }
        "imageresizer_jpegQualityLevel" = @{ value = 90 }
        "imageresizer_pngInterlaceOption" = @{ value = 0 }
        "imageresizer_tiffCompressOption" = @{ value = 0 }
        "imageresizer_fileName" = @{ value = "%1 (%2)" }
        "imageresizer_keepDateModified" = @{ value = $false }
        "imageresizer_fallbackEncoder" = @{ value = "19e4a5aa-5662-4fc5-a0c0-1758028e1057" }
        "imageresizer_customSize" = @{ 
            value = @{
                fit = 2
                width = 1024
                height = 768
                unit = 0
            }
        }
        "imageresizer_sizes" = @{
            value = @(
                @{ name = "Small"; fit = 2; width = 854; height = 480; unit = 0 }
                @{ name = "Medium"; fit = 2; width = 1366; height = 768; unit = 0 }
                @{ name = "Large"; fit = 2; width = 1920; height = 1080; unit = 0 }
                @{ name = "Phone"; fit = 2; width = 320; height = 568; unit = 0 }
            )
        }
    }
    name = "Image Resizer"
    version = "1.0"
}

$imageResizerPath = Join-Path $imageResizerDir "settings.json"
$imageResizerSettings | ConvertTo-Json -Depth 10 | Set-Content -Path $imageResizerPath -Encoding UTF8

Write-Pass "Image Resizer settings written to: $imageResizerPath"

# ============================================================================
# POWERTOYS RUN SETTINGS
# ============================================================================
Write-Step "Writing PowerToys Run settings..."

$ptRunDir = Join-Path $powerToysDir "PowerToys Run"
if (-not (Test-Path $ptRunDir)) {
    New-Item -ItemType Directory -Path $ptRunDir -Force | Out-Null
}

$ptRunSettings = @{
    properties = @{
        "open_powerlauncher" = @{ value = @{ win = $true; ctrl = $false; alt = $true; shift = $false; code = 32; key = "Space" } }
        "search_result_preference" = @{ value = "most_recently_used" }
        "search_type_preference" = @{ value = "application_name" }
        "maximum_number_of_results" = @{ value = 8 }
        "open_file_location" = @{ value = @{ win = $false; ctrl = $true; alt = $false; shift = $true; code = 69; key = "E" } }
        "copy_path_location" = @{ value = @{ win = $false; ctrl = $true; alt = $false; shift = $false; code = 67; key = "C" } }
        "clear_input_on_launch" = @{ value = $true }
        "theme" = @{ value = 0 }
        "position" = @{ value = 0 }
        "use_centralized_keyboard_hook" = @{ value = $false }
        "search_query_results_with_delay" = @{ value = $false }
        "search_input_delay" = @{ value = 150 }
        "plugin_search_delay" = @{ value = 100 }
        "search_click_on_keyboard_focus" = @{ value = $false }
        "search_query_tuning_enabled" = @{ value = $false }
        "search_wait_for_slow_results" = @{ value = $false }
        "generate_thumbnails_from_files" = @{ value = $true }
        "generate_thumbnails_from_shell" = @{ value = $true }
    }
    name = "PowerToys Run"
    version = "1.0"
}

$ptRunPath = Join-Path $ptRunDir "settings.json"
$ptRunSettings | ConvertTo-Json -Depth 10 | Set-Content -Path $ptRunPath -Encoding UTF8

Write-Pass "PowerToys Run settings written to: $ptRunPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath, $fancyZonesPath, $powerRenamePath, $imageResizerPath, $ptRunPath)
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
Write-Host " PowerToys Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) {
    Write-Host "  - $f" -ForegroundColor Gray
}
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
