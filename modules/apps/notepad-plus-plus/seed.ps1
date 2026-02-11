<#
.SYNOPSIS
    Seeds meaningful Notepad++ configuration for curation testing.

.DESCRIPTION
    Sets up Notepad++ configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - config.xml (main settings)
    - shortcuts.xml (custom keyboard shortcuts)

    DOES NOT configure:
    - Session data
    - Recent file lists with real paths

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
Write-Host " Notepad++ Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$nppDir = Join-Path $env:APPDATA "Notepad++"
$configPath = Join-Path $nppDir "config.xml"
$shortcutsPath = Join-Path $nppDir "shortcuts.xml"

# Ensure directory exists
if (-not (Test-Path $nppDir)) {
    Write-Step "Creating Notepad++ config directory..."
    New-Item -ItemType Directory -Path $nppDir -Force | Out-Null
}

# ============================================================================
# CONFIG.XML
# ============================================================================
Write-Step "Writing config.xml..."

$configXml = @'
<?xml version="1.0" encoding="UTF-8" ?>
<NotepadPlus>
    <GUIConfigs>
        <GUIConfig name="ToolBar" visible="yes">standard</GUIConfig>
        <GUIConfig name="StatusBar">show</GUIConfig>
        <GUIConfig name="TabBar" dragAndDrop="yes" drawTopBar="yes" drawInactiveTab="yes" reduce="yes" closeButton="yes" doubleClick2Close="no" vertical="no" multiLine="no" hide="no" quitOnEmpty="no" iconSetNumber="0" />
        <GUIConfig name="ScintillaGlobalSettings" enableMultiSelection="yes" />
        <GUIConfig name="NewDocDefaultSettings" format="0" encoding="4" lang="0" codepage="-1" openAnsiAsUTF8="yes" />
        <GUIConfig name="Auto-detection">yes</GUIConfig>
        <GUIConfig name="TabSetting" replaceBySpace="yes" size="4" />
        <GUIConfig name="AppPosition" x="100" y="100" width="1200" height="800" isMaximized="no" />
        <GUIConfig name="noUpdate" intervalDays="15" nextUpdateDate="2099-01-01">no</GUIConfig>
        <GUIConfig name="Print" lineNumber="yes" printOption="0" headerLeft="" headerMiddle="" headerRight="" footerLeft="" footerMiddle="" footerRight="" headerFontName="" headerFontStyle="0" headerFontSize="0" footerFontName="" footerFontStyle="0" footerFontSize="0" margeLeft="0" margeTop="0" margeRight="0" margeBottom="0" />
        <GUIConfig name="Backup" action="0" useCustumDir="no" dir="" isSnapshotMode="yes" snapshotBackupTiming="7000" />
        <GUIConfig name="DarkMode" enable="yes" darkThemeName="DarkModeDefault.xml" darkToolBarIconSet="4" />
        <GUIConfig name="MiscSettings" fileSwitcherWithoutExtColumn="yes" fileSwitcherExtWidth="50" fileSwitcherWithoutPathColumn="yes" fileSwitcherPathWidth="50" backSlashIsEscapeCharacterForSql="yes" newStyleSaveDlg="yes" isFolderDroppedOpenFiles="no" docPeekOnTab="no" docPeekOnMap="no" sortFunctionList="yes" saveDlgExtFilterToAllTypes="no" muteSounds="no" enableTagsMatchHilite="yes" enableTagAttrsHilite="yes" enableHiliteNonHTMLZone="no" styleMRU="yes" shortTitlebar="no" />
        <GUIConfig name="WordCharList" useDefault="yes" charsAdded="" />
    </GUIConfigs>
    <FindHistory nbMaxFindHistoryPath="10" nbMaxFindHistoryFilter="10" nbMaxFindHistoryFind="10" nbMaxFindHistoryReplace="10" matchWord="no" matchCase="no" wrap="yes" directionDown="yes" fifRecupsive="yes" fifInHiddenFolder="no" fifFilterFollowsDoc="no" fifFolderFollowsDoc="no" searchMode="0" transparencyMode="1" transparency="150" dotMatchesNewline="no" isSearch2ButtonsMode="no" regexBackward4PowerUser="no">
    </FindHistory>
</NotepadPlus>
'@

$configXml | Set-Content -Path $configPath -Encoding UTF8

Write-Pass "Config written to: $configPath"

# ============================================================================
# SHORTCUTS.XML
# ============================================================================
Write-Step "Writing shortcuts.xml..."

$shortcutsXml = @'
<?xml version="1.0" encoding="UTF-8" ?>
<NotepadPlus>
    <InternalCommands>
    </InternalCommands>
    <Macros>
        <Macro name="Trim Trailing and save" Ctrl="no" Alt="no" Shift="no" Key="0">
            <Action type="2" str="0" />
            <Action type="0" str="41006" />
        </Macro>
    </Macros>
    <UserDefinedCommands>
        <Command name="Open in Explorer" Ctrl="no" Alt="yes" Shift="no" Key="69">explorer /select,&quot;$(FULL_CURRENT_PATH)&quot;</Command>
        <Command name="Open Terminal Here" Ctrl="no" Alt="yes" Shift="no" Key="84">cmd /K cd /d &quot;$(CURRENT_DIRECTORY)&quot;</Command>
    </UserDefinedCommands>
    <PluginCommands>
    </PluginCommands>
    <ScintillaKeys>
    </ScintillaKeys>
</NotepadPlus>
'@

$shortcutsXml | Set-Content -Path $shortcutsPath -Encoding UTF8

Write-Pass "Shortcuts written to: $shortcutsPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($configPath, $shortcutsPath)
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
Write-Host " Notepad++ Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $configPath" -ForegroundColor Gray
Write-Host "  - $shortcutsPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
