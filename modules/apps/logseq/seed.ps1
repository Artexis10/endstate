<#
.SYNOPSIS
    Seeds meaningful Logseq configuration for curation testing.

.DESCRIPTION
    Sets up Logseq configuration files with representative non-default values.
    Used by the curation workflow to generate representative config files
    for module validation.

    Configures:
    - config.edn (app-level EDN config)
    - preferences.json (UI preferences)
    - custom.css (custom styling)

    DOES NOT configure:
    - Graph data (pages, journals)
    - Plugin data
    - Sync tokens

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
Write-Host " Logseq Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$logseqDir = Join-Path $env:USERPROFILE ".logseq"

if (-not (Test-Path $logseqDir)) {
    Write-Step "Creating .logseq directory..."
    New-Item -ItemType Directory -Path $logseqDir -Force | Out-Null
}

# ============================================================================
# CONFIG.EDN
# ============================================================================
Write-Step "Writing config.edn..."

$configPath = Join-Path $logseqDir "config.edn"
$config = @'
{;; Logseq App-Level Config - Endstate Seed

 ;; Preferred date format
 :journal/page-title-format "yyyy-MM-dd"

 ;; Preferred file format
 :preferred-format :markdown

 ;; Feature flags
 :feature/enable-journals? true
 :feature/enable-whiteboards? true

 ;; Editor settings
 :editor/logical-outdenting? true
 :editor/preferred-pasting-file? true

 ;; UI preferences
 :ui/show-brackets? true
 :ui/enable-tooltip? true

 ;; Default home page
 :default-home {:page "Contents"}

 ;; Shortcuts
 :shortcuts
 {:editor/new-block "enter"
  :editor/indent "tab"
  :editor/outdent "shift+tab"
  :ui/toggle-left-sidebar "ctrl+\\"
  :ui/toggle-right-sidebar "ctrl+shift+\\"
  :go/search "ctrl+k"
  :go/journals "alt+j"}

 ;; Export settings
 :export/bullet-indentation :four-spaces

 ;; Graph view
 :graph/settings
 {:builtin-pages? false
  :journal? false
  :orphan-pages? true}}
'@
$config | Set-Content -Path $configPath -Encoding UTF8
Write-Pass "config.edn written to: $configPath"

# ============================================================================
# PREFERENCES.JSON
# ============================================================================
Write-Step "Writing preferences.json..."

$prefsPath = Join-Path $logseqDir "preferences.json"
$prefs = @{
    theme = "dark"
    ui_font_family = "Inter"
    ui_font_size = 16
    editor_font_family = "JetBrains Mono"
    editor_font_size = 14
    show_brackets = $true
    enable_tooltip = $true
    preferred_language = "en"
    preferred_workflow = "now"
    preferred_format = "markdown"
    enable_journals = $true
    enable_all_pages_public = $false
    auto_expand_block_refs = $true
    enable_git_auto_push = $false
}
$prefs | ConvertTo-Json -Depth 5 | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "preferences.json written to: $prefsPath"

# ============================================================================
# CUSTOM.CSS
# ============================================================================
Write-Step "Writing custom.css..."

$cssPath = Join-Path $logseqDir "custom.css"
$css = @'
/* Logseq Custom CSS - Endstate Seed */

/* Wider main content area */
.cp__sidebar-main-content {
    max-width: 900px;
    margin: 0 auto;
}

/* Custom heading sizes */
.editor-inner .h1 {
    font-size: 2em;
    font-weight: 700;
}

.editor-inner .h2 {
    font-size: 1.5em;
    font-weight: 600;
}

/* Subtle block reference styling */
.block-ref {
    border-bottom: 1px dashed var(--ls-link-ref-text-color);
    padding: 2px 4px;
    border-radius: 3px;
}

/* Tag styling */
a.tag {
    background-color: var(--ls-tertiary-background-color);
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 0.9em;
}

/* Code block font */
.CodeMirror {
    font-family: 'JetBrains Mono', 'Cascadia Code', monospace;
    font-size: 13px;
}
'@
$css | Set-Content -Path $cssPath -Encoding UTF8
Write-Pass "custom.css written to: $cssPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($configPath, $prefsPath, $cssPath)
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
Write-Host " Logseq Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
