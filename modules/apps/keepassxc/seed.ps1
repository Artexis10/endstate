<#
.SYNOPSIS
    Seeds meaningful KeePassXC configuration for curation testing.

.DESCRIPTION
    Sets up KeePassXC application settings with representative non-default values
    WITHOUT creating any database or key files. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - keepassxc.ini (application settings only)

    DOES NOT configure:
    - Password databases (.kdbx)
    - Key files (.keyx, .key)
    - Any credential material

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
Write-Host " KeePassXC Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$keepassDir = Join-Path $env:APPDATA "KeePassXC"
$iniPath = Join-Path $keepassDir "keepassxc.ini"

if (-not (Test-Path $keepassDir)) {
    Write-Step "Creating KeePassXC directory..."
    New-Item -ItemType Directory -Path $keepassDir -Force | Out-Null
}

# ============================================================================
# KEEPASSXC.INI
# ============================================================================
Write-Step "Writing keepassxc.ini..."

$ini = @"
[General]
AutoSaveAfterEveryChange=true
AutoSaveOnExit=true
BackupBeforeSave=true
MinimizeOnClose=true
MinimizeOnStartup=false
ShowTrayIcon=true
StartMinimized=false
UseAtomicSaves=true
CheckForUpdates=true
Language=en_US

[Browser]
Enabled=true
SearchInAllDatabases=false
SupportBrowserProxy=true
ShowNotification=true
BestMatchOnly=false
UnlockDatabase=true
AlwaysAllowAccess=false

[GUI]
ApplicationTheme=dark
CompactMode=false
HidePasswords=true
HideUsernames=false
MinimizeOnCopy=false
ShowExpiredEntriesOnDatabaseUnlock=true
TrayIconAppearance=monochrome
MonospaceNotes=true

[Security]
ClearClipboardTimeout=10
ClearSearchTimeout=5
LockDatabaseIdleSeconds=300
LockDatabaseMinimize=false
LockDatabaseScreenLock=true
PasswordsHidden=true
RelockAutoType=true

[AutoType]
AutoTypeDelay=25
AutoTypeStartDelay=500
GlobalAutoTypeKey=0
GlobalAutoTypeModifiers=0

[PasswordGenerator]
AdditionalChars=
ExcludedChars=
Length=20
LowerCase=true
UpperCase=true
Numbers=true
SpecialChars=true
Braces=false
Punctuation=false
Quotes=false
Dashes=true
Math=false
Logograms=false
ExtendedASCII=false
ExcludeLookAlike=true
EnsureEvery=true
"@

$ini | Set-Content -Path $iniPath -Encoding UTF8

Write-Pass "keepassxc.ini written to: $iniPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($iniPath)
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
Write-Host " KeePassXC Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $iniPath" -ForegroundColor Gray
Write-Host ""
Write-Step "Excluded (sensitive):"
Write-Host "  - *.kdbx (password databases)" -ForegroundColor DarkYellow
Write-Host "  - *.keyx, *.key (key files)" -ForegroundColor DarkYellow
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
