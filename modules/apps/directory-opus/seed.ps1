<#
.SYNOPSIS
    Seeds meaningful Directory Opus configuration for curation testing.

.DESCRIPTION
    Sets up Directory Opus configuration files with representative non-default values
    WITHOUT creating any license data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - ConfigFiles/preferences.oxc (app preferences)
    - ConfigFiles/toolbar_endstate.dop (sample toolbar)
    - ConfigFiles/hotkeys.oxc (keyboard shortcuts)

    DOES NOT configure:
    - License certificate
    - MRU/state data

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
Write-Host " Directory Opus Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$dopusDir = Join-Path $env:APPDATA "GPSoftware\Directory Opus"
$configDir = Join-Path $dopusDir "ConfigFiles"

if (-not (Test-Path $configDir)) {
    Write-Step "Creating ConfigFiles directory..."
    New-Item -ItemType Directory -Path $configDir -Force | Out-Null
}

# ============================================================================
# PREFERENCES
# ============================================================================
Write-Step "Writing preferences.oxc..."

$prefsPath = Join-Path $configDir "preferences.oxc"
$prefs = @'
<?xml version="1.0" encoding="UTF-8"?>
<oxc version="1" type="preferences">
    <Display>
        <Theme>dark</Theme>
        <FontName>Segoe UI</FontName>
        <FontSize>9</FontSize>
        <ShowGridLines>true</ShowGridLines>
        <ShowFolderSizes>true</ShowFolderSizes>
        <ShowFileExtensions>true</ShowFileExtensions>
        <ShowHiddenFiles>true</ShowHiddenFiles>
        <ShowSystemFiles>false</ShowSystemFiles>
        <SortFoldersFirst>true</SortFoldersFirst>
    </Display>
    <FileOperations>
        <ConfirmDelete>true</ConfirmDelete>
        <ConfirmOverwrite>true</ConfirmOverwrite>
        <UseRecycleBin>true</UseRecycleBin>
        <CopyBufferSize>1048576</CopyBufferSize>
        <PreserveTimestamps>true</PreserveTimestamps>
    </FileOperations>
    <Layout>
        <DualDisplay>true</DualDisplay>
        <TreePanel>left</TreePanel>
        <MetadataPanel>bottom</MetadataPanel>
        <ViewerPane>right</ViewerPane>
        <StatusBar>true</StatusBar>
        <ToolbarLock>false</ToolbarLock>
    </Layout>
    <Tabs>
        <OpenNewTabOnDoubleClick>true</OpenNewTabOnDoubleClick>
        <CloseTabOnMiddleClick>true</CloseTabOnMiddleClick>
        <ShowTabBar>always</ShowTabBar>
    </Tabs>
    <Misc>
        <CheckForUpdates>true</CheckForUpdates>
        <SingleClickOpen>false</SingleClickOpen>
        <FlatView>off</FlatView>
    </Misc>
</oxc>
'@
$prefs | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "Preferences written to: $prefsPath"

# ============================================================================
# TOOLBAR
# ============================================================================
Write-Step "Writing sample toolbar..."

$toolbarPath = Join-Path $configDir "toolbar_endstate.dop"
$toolbar = @'
<?xml version="1.0" encoding="UTF-8"?>
<oxc version="1" type="toolbar" name="Endstate">
    <Buttons>
        <Button label="Back" icon="back" command="Go BACK" />
        <Button label="Forward" icon="forward" command="Go FORWARD" />
        <Button label="Up" icon="up" command="Go UP" />
        <Separator />
        <Button label="New Folder" icon="newfolder" command="CreateFolder" />
        <Button label="New File" icon="newfile" command="FileType NEW=.txt" />
        <Separator />
        <Button label="Terminal" icon="terminal" command="CLI DOSPROMPT=powershell" />
        <Button label="Properties" icon="properties" command="Properties" />
        <Separator />
        <Button label="Flat View" icon="flatview" command="Set FLATVIEW=Toggle" />
        <Button label="Show Hidden" icon="hidden" command="Set SHOWFILTERATTR=~h" />
    </Buttons>
</oxc>
'@
$toolbar | Set-Content -Path $toolbarPath -Encoding UTF8
Write-Pass "Toolbar written to: $toolbarPath"

# ============================================================================
# HOTKEYS
# ============================================================================
Write-Step "Writing hotkeys.oxc..."

$hotkeysPath = Join-Path $configDir "hotkeys.oxc"
$hotkeys = @'
<?xml version="1.0" encoding="UTF-8"?>
<oxc version="1" type="hotkeys">
    <Hotkey key="F2" command="Rename INLINE" description="Inline rename" />
    <Hotkey key="F5" command="Copy" description="Copy files" />
    <Hotkey key="F6" command="Move" description="Move files" />
    <Hotkey key="F7" command="CreateFolder" description="New folder" />
    <Hotkey key="F8" command="Delete" description="Delete" />
    <Hotkey key="Ctrl+F" command="Find" description="Find files" />
    <Hotkey key="Ctrl+G" command="Go PATH" description="Go to path" />
    <Hotkey key="Ctrl+T" command="Go NEW" description="New tab" />
    <Hotkey key="Ctrl+W" command="Go TABCLOSE" description="Close tab" />
    <Hotkey key="Ctrl+Shift+N" command="CreateFolder" description="New folder" />
    <Hotkey key="Alt+Enter" command="Properties" description="Properties" />
    <Hotkey key="Ctrl+L" command="Go ADDRESSBAR" description="Focus address bar" />
</oxc>
'@
$hotkeys | Set-Content -Path $hotkeysPath -Encoding UTF8
Write-Pass "Hotkeys written to: $hotkeysPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath, $toolbarPath, $hotkeysPath)
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
Write-Host " Directory Opus Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""
Write-Step "Excluded (sensitive):"
Write-Host "  - dopus.cert (license)" -ForegroundColor DarkYellow
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
