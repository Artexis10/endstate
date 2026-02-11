<#
.SYNOPSIS
    Seeds meaningful VS Code user configuration for curation testing.

.DESCRIPTION
    Sets up VS Code configuration values that represent real-world user preferences
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - User settings (settings.json)
    - Keybindings (keybindings.json)
    - Snippets (snippets/)

    DOES NOT configure:
    - Account sync tokens
    - API keys
    - Extension-specific secrets

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
Write-Host " VS Code Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$vscodeUserDir = Join-Path $env:APPDATA "Code\User"
$settingsPath = Join-Path $vscodeUserDir "settings.json"
$keybindingsPath = Join-Path $vscodeUserDir "keybindings.json"

# Ensure directory exists
if (-not (Test-Path $vscodeUserDir)) {
    Write-Step "Creating VS Code user directory..."
    New-Item -ItemType Directory -Path $vscodeUserDir -Force | Out-Null
}

# ============================================================================
# SETTINGS.JSON
# ============================================================================
Write-Step "Writing settings.json..."

$settings = @{
    # Editor settings
    "editor.fontSize" = 14
    "editor.fontFamily" = "'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace"
    "editor.fontLigatures" = $true
    "editor.tabSize" = 2
    "editor.insertSpaces" = $true
    "editor.detectIndentation" = $true
    "editor.wordWrap" = "off"
    "editor.minimap.enabled" = $true
    "editor.minimap.scale" = 1
    "editor.renderWhitespace" = "selection"
    "editor.rulers" = @(80, 120)
    "editor.formatOnSave" = $true
    "editor.formatOnPaste" = $false
    "editor.bracketPairColorization.enabled" = $true
    "editor.guides.bracketPairs" = "active"
    "editor.stickyScroll.enabled" = $true
    "editor.cursorBlinking" = "smooth"
    "editor.cursorSmoothCaretAnimation" = "on"
    "editor.smoothScrolling" = $true
    
    # Workbench settings
    "workbench.colorTheme" = "Default Dark+"
    "workbench.iconTheme" = "vs-seti"
    "workbench.startupEditor" = "welcomePage"
    "workbench.editor.enablePreview" = $true
    "workbench.editor.showTabs" = "multiple"
    "workbench.tree.indent" = 16
    "workbench.tree.renderIndentGuides" = "always"
    
    # Files settings
    "files.autoSave" = "afterDelay"
    "files.autoSaveDelay" = 1000
    "files.trimTrailingWhitespace" = $true
    "files.insertFinalNewline" = $true
    "files.trimFinalNewlines" = $true
    "files.encoding" = "utf8"
    "files.eol" = "`n"
    "files.exclude" = @{
        "**/.git" = $true
        "**/.DS_Store" = $true
        "**/node_modules" = $true
        "**/Thumbs.db" = $true
    }
    
    # Terminal settings
    "terminal.integrated.fontSize" = 13
    "terminal.integrated.cursorBlinking" = $true
    "terminal.integrated.defaultProfile.windows" = "PowerShell"
    
    # Explorer settings
    "explorer.confirmDelete" = $false
    "explorer.confirmDragAndDrop" = $false
    "explorer.compactFolders" = $true
    
    # Search settings
    "search.exclude" = @{
        "**/node_modules" = $true
        "**/bower_components" = $true
        "**/*.code-search" = $true
    }
    
    # Git settings
    "git.enableSmartCommit" = $true
    "git.autofetch" = $true
    "git.confirmSync" = $false
    
    # Telemetry
    "telemetry.telemetryLevel" = "off"
    
    # Update settings
    "update.mode" = "default"
}

$settingsJson = $settings | ConvertTo-Json -Depth 5
$settingsJson | Set-Content -Path $settingsPath -Encoding UTF8

Write-Pass "Settings written to: $settingsPath"

# ============================================================================
# KEYBINDINGS.JSON
# ============================================================================
Write-Step "Writing keybindings.json..."

$keybindings = @(
    @{
        key = "ctrl+shift+d"
        command = "editor.action.copyLinesDownAction"
    }
    @{
        key = "ctrl+shift+k"
        command = "editor.action.deleteLines"
    }
    @{
        key = "ctrl+/"
        command = "editor.action.commentLine"
        when = "editorTextFocus && !editorReadonly"
    }
    @{
        key = 'ctrl+`'
        command = "workbench.action.terminal.toggleTerminal"
    }
    @{
        key = "ctrl+b"
        command = "workbench.action.toggleSidebarVisibility"
    }
    @{
        key = "ctrl+shift+e"
        command = "workbench.view.explorer"
    }
    @{
        key = "ctrl+shift+f"
        command = "workbench.view.search"
    }
    @{
        key = "ctrl+shift+g"
        command = "workbench.view.scm"
    }
)

$keybindingsJson = $keybindings | ConvertTo-Json -Depth 5
$keybindingsJson | Set-Content -Path $keybindingsPath -Encoding UTF8

Write-Pass "Keybindings written to: $keybindingsPath"

# ============================================================================
# SNIPPETS
# ============================================================================
Write-Step "Writing snippets..."

$snippetsDir = Join-Path $vscodeUserDir "snippets"
if (-not (Test-Path $snippetsDir)) {
    New-Item -ItemType Directory -Path $snippetsDir -Force | Out-Null
}

$powershellSnippets = @{
    "Function" = @{
        prefix = "func"
        body = @(
            "function `${1:FunctionName} {"
            "    param("
            "        [`${2:string}]`$`${3:Parameter}"
            "    )"
            "    `$0"
            "}"
        )
        description = "PowerShell function with param block"
    }
    "Try-Catch" = @{
        prefix = "trycatch"
        body = @(
            "try {"
            "    `$0"
            "} catch {"
            "    Write-Error `$_.Exception.Message"
            "}"
        )
        description = "Try-catch block"
    }
    "Pester Test" = @{
        prefix = "describe"
        body = @(
            "Describe '`${1:Subject}' {"
            "    It '`${2:should do something}' {"
            "        `$0"
            "    }"
            "}"
        )
        description = "Pester Describe/It block"
    }
}

$psSnippetsPath = Join-Path $snippetsDir "powershell.json"
$powershellSnippets | ConvertTo-Json -Depth 5 | Set-Content -Path $psSnippetsPath -Encoding UTF8
Write-Pass "PowerShell snippets written to: $psSnippetsPath"

$globalSnippets = @{
    "TODO Comment" = @{
        prefix = "todo"
        body = @("// TODO: `$0")
        description = "TODO comment"
    }
    "Console Log" = @{
        prefix = "clog"
        body = @("console.log('`${1:label}:', `${2:value});`$0")
        description = "Console log with label"
    }
}

$globalSnippetsPath = Join-Path $snippetsDir "global.code-snippets"
$globalSnippets | ConvertTo-Json -Depth 5 | Set-Content -Path $globalSnippetsPath -Encoding UTF8
Write-Pass "Global snippets written to: $globalSnippetsPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath, $keybindingsPath, $psSnippetsPath, $globalSnippetsPath)
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
Write-Host " VS Code Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $settingsPath" -ForegroundColor Gray
Write-Host "  - $keybindingsPath" -ForegroundColor Gray
Write-Host "  - $psSnippetsPath" -ForegroundColor Gray
Write-Host "  - $globalSnippetsPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
