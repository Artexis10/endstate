<#
.SYNOPSIS
    Seeds meaningful Sublime Text configuration for curation testing.

.DESCRIPTION
    Sets up Sublime Text configuration files with representative non-default values
    WITHOUT creating any credentials. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Preferences.sublime-settings (user settings)
    - Default (Windows).sublime-keymap (keybindings)
    - Package Control.sublime-settings (installed packages list)
    - Sample snippet

    DOES NOT configure:
    - License key
    - SFTP/server credentials

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
Write-Host " Sublime Text Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$userDir = Join-Path $env:APPDATA "Sublime Text\Packages\User"

if (-not (Test-Path $userDir)) {
    Write-Step "Creating Sublime Text User directory..."
    New-Item -ItemType Directory -Path $userDir -Force | Out-Null
}

# ============================================================================
# PREFERENCES
# ============================================================================
Write-Step "Writing Preferences.sublime-settings..."

$prefsPath = Join-Path $userDir "Preferences.sublime-settings"
$prefs = @'
{
    "theme": "Default Dark.sublime-theme",
    "color_scheme": "Monokai.sublime-color-scheme",
    "font_face": "Cascadia Code",
    "font_size": 13,
    "tab_size": 4,
    "translate_tabs_to_spaces": true,
    "detect_indentation": true,
    "trim_trailing_white_space_on_save": "all",
    "ensure_newline_at_eof_on_save": true,
    "rulers": [80, 120],
    "word_wrap": false,
    "highlight_line": true,
    "show_encoding": true,
    "show_line_endings": true,
    "draw_white_space": ["selection"],
    "save_on_focus_lost": true,
    "hot_exit": true,
    "remember_open_files": true,
    "scroll_past_end": true,
    "minimap_enabled": true,
    "folder_exclude_patterns": [".git", "node_modules", "__pycache__", ".venv"],
    "file_exclude_patterns": ["*.pyc", "*.pyo", "*.exe", "*.dll", "*.obj", "*.o"],
    "ignored_packages": ["Vintage"]
}
'@
$prefs | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "Preferences written to: $prefsPath"

# ============================================================================
# KEYBINDINGS
# ============================================================================
Write-Step "Writing Default (Windows).sublime-keymap..."

$keymapPath = Join-Path $userDir "Default (Windows).sublime-keymap"
$keymap = @'
[
    { "keys": ["ctrl+shift+d"], "command": "duplicate_line" },
    { "keys": ["ctrl+shift+k"], "command": "run_macro_file", "args": {"file": "res://Packages/Default/Delete Line.sublime-macro"} },
    { "keys": ["ctrl+shift+up"], "command": "swap_line_up" },
    { "keys": ["ctrl+shift+down"], "command": "swap_line_down" },
    { "keys": ["ctrl+shift+p"], "command": "show_overlay", "args": {"overlay": "command_palette"} },
    { "keys": ["ctrl+`"], "command": "toggle_terminus_panel" }
]
'@
$keymap | Set-Content -Path $keymapPath -Encoding UTF8
Write-Pass "Keybindings written to: $keymapPath"

# ============================================================================
# PACKAGE CONTROL
# ============================================================================
Write-Step "Writing Package Control.sublime-settings..."

$pkgCtrlPath = Join-Path $userDir "Package Control.sublime-settings"
$pkgCtrl = @'
{
    "bootstrapped": true,
    "installed_packages": [
        "A File Icon",
        "BracketHighlighter",
        "Emmet",
        "GitGutter",
        "LSP",
        "Package Control",
        "SideBarEnhancements",
        "SublimeLinter",
        "Terminus"
    ]
}
'@
$pkgCtrl | Set-Content -Path $pkgCtrlPath -Encoding UTF8
Write-Pass "Package Control settings written to: $pkgCtrlPath"

# ============================================================================
# SAMPLE SNIPPET
# ============================================================================
Write-Step "Writing sample snippet..."

$snippetPath = Join-Path $userDir "todo-comment.sublime-snippet"
$snippet = @'
<snippet>
    <content><![CDATA[
// TODO: ${1:description}
]]></content>
    <tabTrigger>todo</tabTrigger>
    <scope>source</scope>
    <description>TODO comment</description>
</snippet>
'@
$snippet | Set-Content -Path $snippetPath -Encoding UTF8
Write-Pass "Snippet written to: $snippetPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath, $keymapPath, $pkgCtrlPath, $snippetPath)
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
Write-Host " Sublime Text Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
