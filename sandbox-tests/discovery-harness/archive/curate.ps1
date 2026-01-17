<#
================================================================================
ARCHIVED: Legacy Unified Curation Runner
================================================================================

This file is archived and retained for historical and design reference purposes.

It was superseded by the current discovery harness implementation which provides
improved module curation workflows with better separation of concerns.

Do not use this file for active development. Refer to the current harness scripts
in the parent directory for the active implementation.

Archived: 2026-01-17
================================================================================
#>

<#
.SYNOPSIS
    Unified curation runner for Endstate config modules.

.DESCRIPTION
    Single entrypoint for running curation workflows across different apps.
    Supports both local mode (direct execution) and sandbox mode (Windows Sandbox isolation).
    
    This script:
    1. Auto-scaffolds modules/apps/<app>/module.jsonc if missing
    2. Locates and runs per-app runner scripts (curate-<app>.ps1)
    3. Passes through DI params for download/URL resolution
    4. Optionally runs targeted unit tests

.PARAMETER App
    The app to curate (e.g., 'git', 'vscodium'). Mandatory.

.PARAMETER Mode
    Execution mode: 'sandbox' (default) or 'local'.

.PARAMETER ScaffoldOnly
    Only scaffold the module file, do not run curation.

.PARAMETER SkipInstall
    Skip app installation (assumes app is already installed).

.PARAMETER Promote
    Promote the curated module to modules/apps/<app>/.

.PARAMETER RunTests
    Run targeted unit tests after curation.

.PARAMETER ResolveFinalUrlFn
    Optional scriptblock for redirect resolution (DI for testing).

.PARAMETER DownloadFn
    Optional scriptblock for downloading (DI for testing).

.EXAMPLE
    .\curate.ps1 -App git -Mode local
    # Curates Git locally

.EXAMPLE
    .\curate.ps1 -App git -Mode sandbox
    # Curates Git in Windows Sandbox

.EXAMPLE
    .\curate.ps1 -App vscodium -Mode sandbox -Promote -RunTests
    # Curates VSCodium in sandbox, promotes module, runs tests

.EXAMPLE
    .\curate.ps1 -App git -ScaffoldOnly
    # Only scaffolds the module file
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$App,
    
    [Parameter(Mandatory = $false)]
    [ValidateSet('sandbox', 'local')]
    [string]$Mode = 'sandbox',
    
    [Parameter(Mandatory = $false)]
    [switch]$ScaffoldOnly,
    
    [Parameter(Mandatory = $false)]
    [switch]$SkipInstall,
    
    [Parameter(Mandatory = $false)]
    [switch]$Promote,
    
    [Parameter(Mandatory = $false)]
    [switch]$RunTests,
    
    [Parameter(Mandatory = $false)]
    [scriptblock]$ResolveFinalUrlFn,
    
    [Parameter(Mandatory = $false)]
    [scriptblock]$DownloadFn
)

$ErrorActionPreference = 'Stop'

# ============================================================================
# PATH RESOLUTION
# ============================================================================

function Get-RepoRoot {
    <#
    .SYNOPSIS
        Resolves the repository root from the script location.
    #>
    $scriptDir = $PSScriptRoot
    if (-not $scriptDir) {
        $scriptDir = Split-Path -Parent $PSCommandPath
    }
    $repoRoot = (Resolve-Path (Join-Path $scriptDir "..\..")).Path
    return $repoRoot
}

$script:RepoRoot = Get-RepoRoot
$script:HarnessDir = $PSScriptRoot
$script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"

# ============================================================================
# OUTPUT HELPERS
# ============================================================================

function Write-Header {
    param([string]$Message)
    Write-Host ""
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host " $Message" -ForegroundColor Cyan
    Write-Host "=" * 60 -ForegroundColor Cyan
    Write-Host ""
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Gray
}

function Write-Step {
    param([string]$Message)
    Write-Host "[STEP] $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

# ============================================================================
# SCAFFOLDING
# ============================================================================

function New-AppScaffold {
    <#
    .SYNOPSIS
        Ensures the module directory and module.jsonc exist for an app.
    .OUTPUTS
        The path to the module.jsonc file.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$AppName
    )
    
    $appDir = Join-Path $script:ModulesDir $AppName
    $modulePath = Join-Path $appDir "module.jsonc"
    
    # Create directory if needed
    if (-not (Test-Path $appDir)) {
        Write-Step "Creating module directory: $appDir"
        New-Item -ItemType Directory -Path $appDir -Force | Out-Null
    }
    
    # Create minimal module.jsonc if missing
    if (-not (Test-Path $modulePath)) {
        Write-Step "Scaffolding module: $modulePath"
        
        $template = @"
{
  // Config Module: $AppName
  // Auto-scaffolded by curate.ps1 - replace with curated content
  
  "id": "apps.$AppName",
  "displayName": "$AppName",
  "sensitivity": "medium",
  
  "matches": {
    "winget": [],
    "exe": []
  },
  
  "verify": [],
  
  "restore": [],
  
  "capture": {
    "files": [],
    "excludeGlobs": []
  },
  
  "notes": "Auto-scaffolded module. Run curation workflow to populate."
}
"@
        
        try {
            $template | Set-Content -Path $modulePath -Encoding UTF8 -NoNewline
            Write-Pass "Scaffolded: $modulePath"
        }
        catch {
            Write-Fail "Failed to write module file: $_"
            throw "Could not scaffold module at $modulePath"
        }
    }
    else {
        Write-Info "Module exists: $modulePath"
    }
    
    return $modulePath
}

# ============================================================================
# RUNNER RESOLUTION
# ============================================================================

function Get-AppRunner {
    <#
    .SYNOPSIS
        Finds the per-app curation runner script.
    .OUTPUTS
        The path to curate-<app>.ps1.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$AppName
    )
    
    $runnerPath = Join-Path $script:HarnessDir "curate-$AppName.ps1"
    
    if (-not (Test-Path $runnerPath)) {
        throw "Runner script not found: $runnerPath`nCreate curate-$AppName.ps1 to support curation for '$AppName'."
    }
    
    return $runnerPath
}

# ============================================================================
# RUNNER EXECUTION
# ============================================================================

function Invoke-AppRunner {
    <#
    .SYNOPSIS
        Executes the per-app curation runner with appropriate arguments.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$RunnerPath,
        
        [Parameter(Mandatory = $true)]
        [string]$Mode,
        
        [switch]$SkipInstall,
        [switch]$Promote,
        [scriptblock]$ResolveFinalUrlFn,
        [scriptblock]$DownloadFn
    )
    
    Write-Step "Invoking runner: $RunnerPath"
    
    # Build arguments hashtable for splatting
    $runnerArgs = @{
        Mode = $Mode
    }
    
    if ($SkipInstall) {
        $runnerArgs['SkipInstall'] = $true
    }
    
    # Check if runner supports -Promote (inspect script)
    $runnerContent = Get-Content -Path $RunnerPath -Raw -ErrorAction SilentlyContinue
    
    if ($Promote -and $runnerContent -match '\$Promote|\-Promote') {
        $runnerArgs['Promote'] = $true
    }
    elseif ($Promote -and $runnerContent -match '\$WriteModule|\-WriteModule') {
        # Fallback: some runners use -WriteModule instead of -Promote
        $runnerArgs['WriteModule'] = $true
    }
    elseif ($Promote) {
        Write-Info "Runner does not support -Promote, skipping promotion flag"
    }
    
    # Pass through DI params if runner supports them
    if ($ResolveFinalUrlFn -and $runnerContent -match '\$ResolveFinalUrlFn') {
        $runnerArgs['ResolveFinalUrlFn'] = $ResolveFinalUrlFn
    }
    
    if ($DownloadFn -and $runnerContent -match '\$DownloadFn') {
        $runnerArgs['DownloadFn'] = $DownloadFn
    }
    
    # Execute runner
    & $RunnerPath @runnerArgs
    $exitCode = $LASTEXITCODE
    
    if ($exitCode -ne 0) {
        throw "Runner exited with code $exitCode"
    }
    
    return $exitCode
}

# ============================================================================
# TEST EXECUTION
# ============================================================================

function Invoke-TargetedTests {
    <#
    .SYNOPSIS
        Runs targeted Pester tests for the specified app.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$AppName
    )
    
    Write-Step "Running targeted tests for: $AppName"
    
    # Look for app-specific test file
    $testDir = Join-Path $script:RepoRoot "tests\unit"
    
    # Try different naming conventions
    $testCandidates = @(
        (Join-Path $testDir "$($AppName)Module.Tests.ps1"),
        (Join-Path $testDir "$(($AppName.Substring(0,1).ToUpper() + $AppName.Substring(1)))Module.Tests.ps1"),
        (Join-Path $testDir "Curate.Tests.ps1")
    )
    
    $testFile = $null
    foreach ($candidate in $testCandidates) {
        if (Test-Path $candidate) {
            $testFile = $candidate
            break
        }
    }
    
    if (-not $testFile) {
        Write-Info "No targeted test file found for '$AppName', skipping tests"
        return 0
    }
    
    Write-Info "Test file: $testFile"
    
    # Run Pester
    $pesterModule = Join-Path $script:RepoRoot "tools\pester\Pester"
    if (Test-Path $pesterModule) {
        Import-Module $pesterModule -Force
    }
    else {
        Import-Module Pester -MinimumVersion 5.0 -ErrorAction Stop
    }
    
    $config = New-PesterConfiguration
    $config.Run.Path = $testFile
    $config.Run.Exit = $false
    $config.Output.Verbosity = 'Detailed'
    
    $result = Invoke-Pester -Configuration $config
    
    if ($result.FailedCount -gt 0) {
        Write-Fail "Tests failed: $($result.FailedCount) failures"
        return 1
    }
    
    Write-Pass "Tests passed: $($result.PassedCount) passed"
    return 0
}

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Endstate Curation Runner"

# Normalize app name to lowercase
$App = $App.ToLower()

Write-Info "Repo Root:    $script:RepoRoot"
Write-Info "Harness Dir:  $script:HarnessDir"
Write-Info "Modules Dir:  $script:ModulesDir"
Write-Info ""
Write-Info "App:          $App"
Write-Info "Mode:         $Mode"
Write-Info "ScaffoldOnly: $ScaffoldOnly"
Write-Info "SkipInstall:  $SkipInstall"
Write-Info "Promote:      $Promote"
Write-Info "RunTests:     $RunTests"

# Step 1: Ensure module scaffold exists
Write-Header "Step 1: Module Scaffold"
$modulePath = New-AppScaffold -AppName $App
Write-Info "Module path: $modulePath"

if ($ScaffoldOnly) {
    Write-Header "Scaffold Complete"
    Write-Pass "Module scaffolded at: $modulePath"
    Write-Host ""
    exit 0
}

# Step 2: Locate runner
Write-Header "Step 2: Locate Runner"
try {
    $runnerPath = Get-AppRunner -AppName $App
    Write-Info "Runner path: $runnerPath"
}
catch {
    Write-Fail $_
    exit 1
}

# Step 3: Execute runner
Write-Header "Step 3: Execute Curation"
try {
    $null = Invoke-AppRunner `
        -RunnerPath $runnerPath `
        -Mode $Mode `
        -SkipInstall:$SkipInstall `
        -Promote:$Promote `
        -ResolveFinalUrlFn $ResolveFinalUrlFn `
        -DownloadFn $DownloadFn
    
    Write-Pass "Curation completed successfully"
}
catch {
    Write-Fail "Curation failed: $_"
    exit 1
}

# Step 4: Run tests if requested
if ($RunTests) {
    Write-Header "Step 4: Run Tests"
    $testExitCode = Invoke-TargetedTests -AppName $App
    
    if ($testExitCode -ne 0) {
        Write-Fail "Tests failed"
        exit $testExitCode
    }
}

# Summary
Write-Header "Curation Complete"
Write-Host "  App:        $App" -ForegroundColor Green
Write-Host "  Mode:       $Mode" -ForegroundColor Green
Write-Host "  Module:     $modulePath" -ForegroundColor Green
Write-Host ""

exit 0
