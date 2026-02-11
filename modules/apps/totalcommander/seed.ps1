<#
.SYNOPSIS
    Seeds meaningful Total Commander configuration for curation testing.

.DESCRIPTION
    Sets up Total Commander configuration with representative non-default values
    WITHOUT creating any credentials. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - wincmd.ini (main configuration)

    DOES NOT configure:
    - wcx_ftp.ini (FTP credentials - SENSITIVE, excluded)
    - Plugin binaries

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
Write-Host " Total Commander Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$ghislerDir = Join-Path $env:APPDATA "GHISLER"
$wincmdPath = Join-Path $ghislerDir "wincmd.ini"

# Ensure directory exists
if (-not (Test-Path $ghislerDir)) {
    Write-Step "Creating GHISLER directory..."
    New-Item -ItemType Directory -Path $ghislerDir -Force | Out-Null
}

# ============================================================================
# WINCMD.INI
# ============================================================================
Write-Step "Writing wincmd.ini..."

$wincmdIni = @"
[Configuration]
ShowHiddenSystem=1
UseRightButton=1
Savepath=1
ShowParentDirInRoot=1
ShowToolTips=1
AlwaysToRoot=0
SortUpper=0
SizeStyle=3
SizeFooter=1
ShowDotFiles=1
AltSearch=2
DirTabOptions=1023
DirTabLimit=32
ActiveRight=0
DirBrackets=0
ShowCentury=1
MarkDirectories=1
SmallFileListFont=0
TabStops=116,101,101
ExtendedSelection=1
InplaceRename=1

[Layout]
ButtonBar=1
DriveBar1=1
DriveBar2=1
DriveBarFlat=1
InterfaceFlat=1
DriveCombo=1
DirectoryTabs=1
CurDir=1
TabHeader=1
StatusBar=1
CmdLine=1
KeyButtons=0
FlatOld=0
FlatInterface=1
BreadCrumbBar=1

[Colors]
InverseCursor=1
ThemedCursor=1
BackColor=0
BackColor2=-1
ForeColor=0
MarkColor=255
CursorColor=8421504
CursorText=-1

[Tabstops]
AdjustWidth=1
0=116
1=101
2=101

[Shortcuts]
F2=cm_RenameOnly
F5=cm_Copy
F6=cm_RenMove
F7=cm_MkDir
F8=cm_Delete
Ctrl+F=cm_SearchFor
Ctrl+M=cm_MultiRenameFiles
Ctrl+Q=cm_ToggleQuickView
Ctrl+U=cm_Exchange
Ctrl+Shift+Enter=cm_CopyNamesToClip

[Packer]
ZIP=1
ARJ=0
LHA=0
RAR=0
UC2=0
InternalZip=7z.dll
InternalZipParam=-mx=5
ZIPlikeDirectory=1
OpenLastUsedPacker=0

[Lister]
Multimedia=1
HTMLAsText=0
StartupMode=1
Font1Name=Consolas
Font1Size=11
Font1Style=0
"@

$wincmdIni | Set-Content -Path $wincmdPath -Encoding UTF8

Write-Pass "wincmd.ini written to: $wincmdPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($wincmdPath)
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
Write-Host " Total Commander Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $wincmdPath" -ForegroundColor Gray
Write-Host ""
Write-Step "Excluded (sensitive):"
Write-Host "  - wcx_ftp.ini (FTP credentials)" -ForegroundColor DarkYellow
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
