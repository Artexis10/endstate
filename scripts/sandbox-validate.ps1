<#
.SYNOPSIS
    Host-side entry script for Sandbox-based module validation.

.DESCRIPTION
    Launches Windows Sandbox to validate a module's capture/restore cycle:
    1. Install app via winget
    2. Run seed script (if present)
    3. Capture config using module definition
    4. Wipe (simulate loss by moving files to .bak)
    5. Restore using module definition
    6. Verify using engine/verify.ps1 logic

    Produces PASS/FAIL output with artifacts in a timestamped directory.

.PARAMETER AppId
    Module app ID (folder name under modules/apps/, e.g., "git").

.PARAMETER WingetId
    Winget package ID. If not provided, looked up from module.jsonc.

.PARAMETER Seed
    Run seed script if present. Default: true.

.PARAMETER OutDir
    Output directory for artifacts. Default: sandbox-tests/validation/<appId>/<timestamp>/

.PARAMETER NoLaunch
    Generate .wsb file but don't launch sandbox (for manual testing).

.EXAMPLE
    .\scripts\sandbox-validate.ps1 -AppId git

.EXAMPLE
    .\scripts\sandbox-validate.ps1 -AppId vscodium -Seed:$false

.EXAMPLE
    .\scripts\sandbox-validate.ps1 -WingetId "Git.Git"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$AppId,
    
    [Parameter(Mandatory = $false)]
    [string]$WingetId,
    
    [Parameter(Mandatory = $false)]
    [bool]$Seed = $true,
    
    [Parameter(Mandatory = $false)]
    [string]$OutDir,
    
    [Parameter(Mandatory = $false)]
    [switch]$NoLaunch,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerPath,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerArgs,
    
    [Parameter(Mandatory = $false)]
    [string]$InstallerExePath
)

$ErrorActionPreference = 'Stop'

# Resolve paths
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"
$script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"

function Write-Header {
    param([string]$Message)
    Write-Host ""
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host " $Message" -ForegroundColor Cyan
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host ""
}

function Write-Step {
    param([string]$Message)
    Write-Host "[STEP] $Message" -ForegroundColor Yellow
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

# Load manifest.ps1 for JSONC parsing
$manifestModule = Join-Path $script:RepoRoot "engine\manifest.ps1"
if (Test-Path $manifestModule) {
    . $manifestModule
}

function Read-ModuleJsonc {
    param([string]$Path)
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    # Use Read-JsoncFile if available, otherwise basic parsing
    if (Get-Command Read-JsoncFile -ErrorAction SilentlyContinue) {
        return Read-JsoncFile -Path $Path
    }
    
    # Fallback: strip comments and parse
    $content = Get-Content -Path $Path -Raw
    $content = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
    return $content | ConvertFrom-Json -AsHashtable
}

# ============================================================================
# VALIDATION
# ============================================================================

# Must have either AppId or WingetId
if (-not $AppId -and -not $WingetId) {
    Write-Host "[ERROR] Either -AppId or -WingetId is required." -ForegroundColor Red
    exit 1
}

# If only WingetId provided, try to find matching module
if (-not $AppId -and $WingetId) {
    $moduleDirs = Get-ChildItem -Path $script:ModulesDir -Directory -ErrorAction SilentlyContinue
    foreach ($dir in $moduleDirs) {
        $modulePath = Join-Path $dir.FullName "module.jsonc"
        if (Test-Path $modulePath) {
            $module = Read-ModuleJsonc -Path $modulePath
            if ($module -and $module.matches -and $module.matches.winget) {
                if ($WingetId -in $module.matches.winget) {
                    $AppId = $dir.Name
                    Write-Info "Resolved WingetId '$WingetId' to AppId '$AppId'"
                    break
                }
            }
        }
    }
    
    if (-not $AppId) {
        Write-Host "[ERROR] Could not find module for WingetId '$WingetId'" -ForegroundColor Red
        exit 1
    }
}

# Load module
$moduleDir = Join-Path $script:ModulesDir $AppId
$modulePath = Join-Path $moduleDir "module.jsonc"

if (-not (Test-Path $modulePath)) {
    Write-Host "[ERROR] Module not found: $modulePath" -ForegroundColor Red
    exit 1
}

$module = Read-ModuleJsonc -Path $modulePath
if (-not $module) {
    Write-Host "[ERROR] Failed to parse module: $modulePath" -ForegroundColor Red
    exit 1
}

# Get WingetId from module if not provided
if (-not $WingetId) {
    if ($module.matches -and $module.matches.winget -and $module.matches.winget.Count -gt 0) {
        $WingetId = $module.matches.winget[0]
    } else {
        Write-Host "[ERROR] Module has no winget ID and -WingetId not provided" -ForegroundColor Red
        exit 1
    }
}

# Check for seed script
$seedScript = Join-Path $moduleDir "seed.ps1"
$hasSeed = Test-Path $seedScript

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Sandbox Validation: $AppId"

# Create output directory
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
if (-not $OutDir) {
    $OutDir = Join-Path $script:RepoRoot "sandbox-tests\validation\$AppId\$timestamp"
}

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}

Write-Info "App ID: $AppId"
Write-Info "Winget ID: $WingetId"
Write-Info "Module: $modulePath"
Write-Info "Seed: $Seed (available: $hasSeed)"
Write-Info "Output: $OutDir"

# ============================================================================
# Generate .wsb configuration
# ============================================================================
Write-Step "Generating sandbox configuration..."

$sandboxRepoPath = "C:\Endstate"
$sandboxArtifactPath = $OutDir -replace [regex]::Escape($script:RepoRoot), $sandboxRepoPath

# Build command line - call powershell.exe directly (no cmd.exe /c start)
$scriptPath = "$sandboxRepoPath\sandbox-tests\discovery-harness\sandbox-validate.ps1"

# Build command string for direct powershell.exe invocation in LogonCommand
# Quote paths (scriptPath, OutputDir, InstallerPath, InstallerExePath) for spaces
# Do not quote simple identifiers (AppId, WingetId)
$cmdParts = @(
    "powershell.exe",
    "-ExecutionPolicy Bypass",
    "-NoExit",
    "-File `"$scriptPath`"",
    "-AppId $AppId",
    "-WingetId $WingetId",
    "-OutputDir `"$sandboxArtifactPath`""
)
if (-not $Seed -or -not $hasSeed) {
    $cmdParts += "-NoSeed"
}
if ($InstallerPath) {
    $cmdParts += "-InstallerPath `"$InstallerPath`""
}
if ($InstallerArgs) {
    # Escape double quotes within InstallerArgs for the command line
    $escapedArgs = $InstallerArgs -replace '"', '\"'
    $cmdParts += "-InstallerArgs `"$escapedArgs`""
}
if ($InstallerExePath) {
    $cmdParts += "-InstallerExePath `"$InstallerExePath`""
}

$command = $cmdParts -join " "

$wsbContent = @"
<Configuration>
  <Networking>Default</Networking>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>$($script:RepoRoot)</HostFolder>
      <SandboxFolder>$sandboxRepoPath</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>$command</Command>
  </LogonCommand>
</Configuration>
"@

$wsbPath = Join-Path $OutDir "validate.wsb"
$wsbContent | Out-File -FilePath $wsbPath -Encoding UTF8
Write-Pass "Created: $wsbPath"

if ($NoLaunch) {
    Write-Info "NoLaunch specified - sandbox not started"
    Write-Info "To run manually: Start-Process `"$wsbPath`""
    Write-Host ""
    Write-Host "Output directory: $OutDir" -ForegroundColor Green
    exit 0
}

# ============================================================================
# Launch Sandbox
# ============================================================================
Write-Step "Launching Windows Sandbox..."

$wsExe = Join-Path $env:WINDIR 'System32\WindowsSandbox.exe'
if (-not (Test-Path $wsExe)) {
    Write-Host "[ERROR] WindowsSandbox.exe not found at: $wsExe" -ForegroundColor Red
    Write-Host ""
    Write-Host "[HINT] Windows Sandbox is not installed. To enable it:" -ForegroundColor Yellow
    Write-Host "  1. Open 'Turn Windows features on or off'" -ForegroundColor Yellow
    Write-Host "  2. Check 'Windows Sandbox'" -ForegroundColor Yellow
    Write-Host "  3. Restart your computer" -ForegroundColor Yellow
    exit 1
}

Write-Info "Sandbox will validate: install -> seed -> capture -> wipe -> restore -> verify"
Write-Host ""

Start-Process -FilePath $wsExe -ArgumentList "`"$wsbPath`"" -Wait

# ============================================================================
# Wait for sentinel and check results
# ============================================================================
Write-Step "Checking for results..."

$doneFile = Join-Path $OutDir "DONE.txt"
$errorFile = Join-Path $OutDir "ERROR.txt"
$resultFile = Join-Path $OutDir "result.json"

# Poll for sentinel files
$timeoutSeconds = 900
$pollIntervalMs = 500
$elapsed = 0

while ($elapsed -lt ($timeoutSeconds * 1000)) {
    if ((Test-Path $doneFile) -or (Test-Path $errorFile)) {
        break
    }
    Start-Sleep -Milliseconds $pollIntervalMs
    $elapsed += $pollIntervalMs
}

# Check results
if (Test-Path $errorFile) {
    Write-Fail "Validation FAILED"
    Write-Host ""
    Write-Host "Error details:" -ForegroundColor Red
    Get-Content -Path $errorFile | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
    Write-Host ""
    Write-Host "Artifacts: $OutDir" -ForegroundColor Yellow
    exit 1
}

if (-not (Test-Path $doneFile)) {
    Write-Fail "Validation TIMEOUT"
    Write-Host ""
    Write-Host "[ERROR] Sandbox did not complete within ${timeoutSeconds}s" -ForegroundColor Red
    Write-Host "Artifacts: $OutDir" -ForegroundColor Yellow
    exit 1
}

# Parse result
if (Test-Path $resultFile) {
    $result = Get-Content -Path $resultFile -Raw | ConvertFrom-Json
    
    Write-Host ""
    if ($result.status -eq "PASS") {
        Write-Pass "Validation PASSED"
        Write-Host ""
        Write-Host "  App:      $AppId" -ForegroundColor White
        Write-Host "  Winget:   $WingetId" -ForegroundColor White
        Write-Host "  Seed:     $($result.seedRan)" -ForegroundColor White
        Write-Host "  Captured: $($result.capturedFiles) files" -ForegroundColor White
        Write-Host "  Wiped:    $($result.wipedFiles) files" -ForegroundColor White
        Write-Host "  Restored: $($result.restoredFiles) files" -ForegroundColor White
        Write-Host "  Verified: $($result.verifyPass)/$($result.verifyTotal) checks passed" -ForegroundColor White
    } else {
        Write-Fail "Validation FAILED"
        Write-Host ""
        Write-Host "  App:      $AppId" -ForegroundColor White
        Write-Host "  Stage:    $($result.failedStage)" -ForegroundColor Red
        Write-Host "  Reason:   $($result.failReason)" -ForegroundColor Red
    }
} else {
    # No result.json but DONE.txt exists - check DONE.txt content
    $doneContent = Get-Content -Path $doneFile -Raw
    if ($doneContent -match "PASS") {
        Write-Pass "Validation PASSED"
    } else {
        Write-Fail "Validation completed with unknown status"
    }
}

Write-Host ""
Write-Host "Artifacts: $OutDir" -ForegroundColor Green
Write-Host ""

# Exit with appropriate code
if (Test-Path $resultFile) {
    $result = Get-Content -Path $resultFile -Raw | ConvertFrom-Json
    if ($result.status -eq "PASS") {
        exit 0
    } else {
        exit 1
    }
}

exit 0
