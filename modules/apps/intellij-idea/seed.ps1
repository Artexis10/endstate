<#
.SYNOPSIS
    Seeds meaningful IntelliJ IDEA configuration for curation testing.

.DESCRIPTION
    Sets up IntelliJ IDEA configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - options/ide.general.xml (general IDE settings)
    - options/editor.xml (editor settings)
    - keymaps/Endstate.xml (custom keymap)
    - codestyles/Endstate.xml (code style)
    - templates/Endstate.xml (live template)

    DOES NOT configure:
    - GitHub/VCS credentials
    - Database passwords
    - License information

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
Write-Host " IntelliJ IDEA Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS (use a representative version folder)
# ============================================================================
$ideaDir = Join-Path $env:APPDATA "JetBrains\IntelliJIdea2024.3"
$optionsDir = Join-Path $ideaDir "options"
$keymapsDir = Join-Path $ideaDir "keymaps"
$codestylesDir = Join-Path $ideaDir "codestyles"
$templatesDir = Join-Path $ideaDir "templates"

foreach ($dir in @($optionsDir, $keymapsDir, $codestylesDir, $templatesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# IDE GENERAL SETTINGS
# ============================================================================
Write-Step "Writing options/ide.general.xml..."

$ideGeneralPath = Join-Path $optionsDir "ide.general.xml"
$ideGeneral = @'
<application>
  <component name="GeneralSettings">
    <option name="autoSaveIfInactive" value="true" />
    <option name="inactiveTimeout" value="15" />
    <option name="confirmExit" value="true" />
    <option name="showTipsOnStartup" value="false" />
    <option name="reopenLastProject" value="true" />
    <option name="defaultProjectDirectory" value="$USER_HOME$/Projects" />
  </component>
</application>
'@
$ideGeneral | Set-Content -Path $ideGeneralPath -Encoding UTF8
Write-Pass "ide.general.xml written"

# ============================================================================
# EDITOR SETTINGS
# ============================================================================
Write-Step "Writing options/editor.xml..."

$editorPath = Join-Path $optionsDir "editor.xml"
$editor = @'
<application>
  <component name="EditorSettings">
    <option name="IS_VIRTUAL_SPACE" value="false" />
    <option name="LINE_NUMBERS_SHOWN" value="true" />
    <option name="FOLD_IMPORTS" value="true" />
    <option name="SHOW_BREADCRUMBS" value="true" />
    <option name="IS_CARET_BLINKING" value="true" />
    <option name="CARET_BLINKING_PERIOD" value="500" />
    <option name="IS_RIGHT_MARGIN_SHOWN" value="true" />
    <option name="RIGHT_MARGIN" value="120" />
    <option name="IS_WHITESPACES_SHOWN" value="false" />
    <option name="STRIP_TRAILING_SPACES" value="Changed" />
    <option name="ENSURE_NEWLINE_AT_EOF" value="true" />
    <option name="SHOW_INTENTION_BULB" value="true" />
    <option name="REFORMAT_ON_PASTE" value="1" />
    <option name="FONT_SIZE" value="14" />
    <option name="FONT_FAMILY" value="JetBrains Mono" />
    <option name="ENABLE_LIGATURES" value="true" />
    <option name="LINE_SPACING" value="1.2" />
  </component>
</application>
'@
$editor | Set-Content -Path $editorPath -Encoding UTF8
Write-Pass "editor.xml written"

# ============================================================================
# CUSTOM KEYMAP
# ============================================================================
Write-Step "Writing keymaps/Endstate.xml..."

$keymapPath = Join-Path $keymapsDir "Endstate.xml"
$keymap = @'
<keymap version="1" name="Endstate" parent="$default">
  <action id="EditorDuplicate">
    <keyboard-shortcut first-keystroke="ctrl shift D" />
  </action>
  <action id="EditorDeleteLine">
    <keyboard-shortcut first-keystroke="ctrl shift K" />
  </action>
  <action id="CommentByLineComment">
    <keyboard-shortcut first-keystroke="ctrl SLASH" />
  </action>
  <action id="ReformatCode">
    <keyboard-shortcut first-keystroke="ctrl alt L" />
  </action>
  <action id="FindInPath">
    <keyboard-shortcut first-keystroke="ctrl shift F" />
  </action>
  <action id="ReplaceInPath">
    <keyboard-shortcut first-keystroke="ctrl shift R" />
  </action>
  <action id="ActivateTerminalToolWindow">
    <keyboard-shortcut first-keystroke="alt F12" />
  </action>
</keymap>
'@
$keymap | Set-Content -Path $keymapPath -Encoding UTF8
Write-Pass "Keymap written"

# ============================================================================
# CODE STYLE
# ============================================================================
Write-Step "Writing codestyles/Endstate.xml..."

$codestylePath = Join-Path $codestylesDir "Endstate.xml"
$codestyle = @'
<code_scheme name="Endstate" version="173">
  <option name="RIGHT_MARGIN" value="120" />
  <option name="FORMATTER_TAGS_ENABLED" value="true" />
  <JavaCodeStyleSettings>
    <option name="CLASS_COUNT_TO_USE_IMPORT_ON_DEMAND" value="99" />
    <option name="NAMES_COUNT_TO_USE_IMPORT_ON_DEMAND" value="99" />
    <option name="IMPORT_LAYOUT_TABLE">
      <value>
        <package name="" withSubpackages="true" static="false" />
        <emptyLine />
        <package name="javax" withSubpackages="true" static="false" />
        <package name="java" withSubpackages="true" static="false" />
        <emptyLine />
        <package name="" withSubpackages="true" static="true" />
      </value>
    </option>
  </JavaCodeStyleSettings>
  <codeStyleSettings language="JAVA">
    <option name="KEEP_BLANK_LINES_IN_CODE" value="1" />
    <option name="ALIGN_MULTILINE_PARAMETERS" value="false" />
    <indentOptions>
      <option name="INDENT_SIZE" value="4" />
      <option name="TAB_SIZE" value="4" />
      <option name="USE_TAB_CHARACTER" value="false" />
    </indentOptions>
  </codeStyleSettings>
</code_scheme>
'@
$codestyle | Set-Content -Path $codestylePath -Encoding UTF8
Write-Pass "Code style written"

# ============================================================================
# LIVE TEMPLATE
# ============================================================================
Write-Step "Writing templates/Endstate.xml..."

$templatePath = Join-Path $templatesDir "Endstate.xml"
$template = @'
<templateSet group="Endstate">
  <template name="psvm" value="public static void main(String[] args) {&#10;    $END$&#10;}" description="main method" toReformat="true" toShortenFQNames="true">
    <context>
      <option name="JAVA_DECLARATION" value="true" />
    </context>
  </template>
  <template name="sout" value="System.out.println($EXPR$);" description="Print to stdout" toReformat="true" toShortenFQNames="true">
    <variable name="EXPR" expression="" defaultValue="&quot;&quot;" alwaysStopAt="true" />
    <context>
      <option name="JAVA_STATEMENT" value="true" />
    </context>
  </template>
  <template name="todo" value="// TODO: $TODO$" description="TODO comment" toReformat="false" toShortenFQNames="false">
    <variable name="TODO" expression="" defaultValue="" alwaysStopAt="true" />
    <context>
      <option name="JAVA_COMMENT" value="true" />
    </context>
  </template>
</templateSet>
'@
$template | Set-Content -Path $templatePath -Encoding UTF8
Write-Pass "Live template written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($ideGeneralPath, $editorPath, $keymapPath, $codestylePath, $templatePath)
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
Write-Host " IntelliJ IDEA Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
