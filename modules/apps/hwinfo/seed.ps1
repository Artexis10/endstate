<#
.SYNOPSIS
    Seeds meaningful HWiNFO configuration for curation testing.

.DESCRIPTION
    Sets up HWiNFO configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - HWiNFO64.INI (sensor config, layout, alerts)

    DOES NOT configure:
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
Write-Host " HWiNFO Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$hwinfoDir = Join-Path $env:APPDATA "HWiNFO64"

if (-not (Test-Path $hwinfoDir)) {
    Write-Step "Creating directory: $hwinfoDir"
    New-Item -ItemType Directory -Path $hwinfoDir -Force | Out-Null
}

# ============================================================================
# HWiNFO64.INI
# ============================================================================
Write-Step "Writing HWiNFO64.INI..."

$iniPath = Join-Path $hwinfoDir "HWiNFO64.INI"
$ini = @'
[Settings]
SensorsOnly=0
MinimizeToTray=1
MinimizeOnClose=1
AutoStart=0
ShowSysTrayIcon=1
UpdateInterval=2000
LoggingInterval=2000
SharedMemorySupport=1

[Sensors]
ShowCPUTemp=1
ShowGPUTemp=1
ShowFanSpeed=1
ShowVoltages=1
ShowPower=1
ShowClocks=1
ShowUsage=1

[Layout]
SensorWindowX=100
SensorWindowY=100
SensorWindowWidth=800
SensorWindowHeight=600
ColumnOrder=0,1,2,3,4,5
SortColumn=0
SortAscending=1

[Alerts]
CPUTempWarning=85
CPUTempCritical=95
GPUTempWarning=80
GPUTempCritical=90
AlertSound=1
AlertPopup=1

[Logging]
LogToFile=0
LogFilePath=
CSVSeparator=,
AppendToLog=1
'@
$ini | Set-Content -Path $iniPath -Encoding UTF8
Write-Pass "HWiNFO64.INI written"

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
Write-Host " HWiNFO Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
