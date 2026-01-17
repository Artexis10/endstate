<#
.SYNOPSIS
    [DEPRECATED] Automated Git curation workflow for generating/validating the apps.git module.

.DESCRIPTION
    *** DEPRECATED: Use the generic data-driven curate.ps1 instead ***
    
    New usage:
      .\curate.ps1 -ModuleId apps.git
      .\curate.ps1 -ModuleId apps.git -Mode local -AllowHostMutation -Seed
    
    This script is kept for reference but should not be used for new curation work.
    
    ---
    
    Runs a complete curation workflow for Git (apps.git) that:
    1. Ensures Git is installed (via winget id Git.Git)
    2. Seeds meaningful Git user state (aliases, default branch, editor, diff tool)
    3. Runs capture/discovery diff
    4. Emits a draft module folder and human-readable report

    Can run in Windows Sandbox (full isolation) or locally (harness mode).
    
    SECURITY: Does NOT create or store any credentials.

.PARAMETER Mode
    Execution mode:
    - 'sandbox' (default): Full Windows Sandbox isolation
    - 'local': Run directly on host (use with caution - modifies user .gitconfig)

.PARAMETER OutDir
    Output directory for artifacts. Default: sandbox-tests/curation/git/<timestamp>

.PARAMETER SkipInstall
    Skip Git installation (assumes Git is already installed).

.PARAMETER DryRun
    Skip winget install and seeding (validates wiring only).

.PARAMETER WriteModule
    Write the draft module to modules/apps/git/module.jsonc.

.PARAMETER Promote
    Alias for -WriteModule. Promotes curated module to modules/apps/git/.

.EXAMPLE
    .\curate-git.ps1
    # Runs full curation in Windows Sandbox

.EXAMPLE
    .\curate-git.ps1 -Mode local -SkipInstall
    # Runs curation locally (assumes Git installed)

.EXAMPLE
    .\curate-git.ps1 -DryRun
    # Validates wiring without actual changes
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet('sandbox', 'local')]
    [string]$Mode = 'sandbox',
    
    [Parameter(Mandatory = $false)]
    [string]$OutDir,
    
    [Parameter(Mandatory = $false)]
    [switch]$SkipInstall,
    
    [Parameter(Mandatory = $false)]
    [switch]$DryRun,
    
    [Parameter(Mandatory = $false)]
    [switch]$WriteModule,
    
    [Parameter(Mandatory = $false)]
    [switch]$Promote
)

$ErrorActionPreference = 'Stop'

# Resolve paths
$script:HarnessDir = $PSScriptRoot
$script:RepoRoot = (Resolve-Path (Join-Path $script:HarnessDir "..\..")).Path
$script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
$script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"
$script:SeedScript = Join-Path $script:HarnessDir "seed-git-config.ps1"

# Load snapshot module
if (-not (Test-Path $script:SnapshotModule)) {
    Write-Error "Snapshot module not found: $script:SnapshotModule"
    exit 1
}
. $script:SnapshotModule

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

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor DarkYellow
}

function New-CurationReport {
    <#
    .SYNOPSIS
        Generate a human-readable curation report.
    #>
    param(
        [string]$WingetId,
        [string]$DiffJsonPath,
        [string]$OutputPath,
        [hashtable]$SensitiveFiles
    )
    
    $diff = Get-Content -Path $DiffJsonPath -Raw | ConvertFrom-Json
    
    # Combine added and modified
    $allChanges = @()
    if ($diff.added) { $allChanges += $diff.added }
    if ($diff.modified) { $allChanges += $diff.modified }
    
    $files = @($allChanges | Where-Object { -not $_.isDirectory })
    
    # Categorize files
    $configFiles = @()
    $cacheFiles = @()
    $logFiles = @()
    $sensitiveFiles = @()
    $otherFiles = @()
    
    foreach ($file in $files) {
        $path = $file.path.ToLower()
        
        # Check sensitive patterns
        if ($path -match '\.git-credentials' -or $path -match 'credentials' -or $path -match '\.ssh' -or $path -match '\.gnupg') {
            $sensitiveFiles += $file
        }
        # Check cache patterns
        elseif ($path -match '\\cache\\' -or $path -match '\\temp\\' -or $path -match '\.tmp$') {
            $cacheFiles += $file
        }
        # Check log patterns
        elseif ($path -match '\\logs?\\' -or $path -match '\.log$') {
            $logFiles += $file
        }
        # Check config patterns
        elseif ($path -match '\.gitconfig' -or $path -match '\\git\\config' -or $path -match '\.gitattributes' -or $path -match '\\git\\ignore') {
            $configFiles += $file
        }
        else {
            $otherFiles += $file
        }
    }
    
    # Build report
    $report = @"
================================================================================
                         GIT CURATION REPORT
================================================================================
Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Winget ID: $WingetId

--------------------------------------------------------------------------------
SUMMARY
--------------------------------------------------------------------------------
Total files changed:     $($files.Count)
  - Config files:        $($configFiles.Count)
  - Cache/temp files:    $($cacheFiles.Count)
  - Log files:           $($logFiles.Count)
  - Sensitive files:     $($sensitiveFiles.Count)
  - Other files:         $($otherFiles.Count)

--------------------------------------------------------------------------------
TOUCHED FILE PATHS
--------------------------------------------------------------------------------
"@

    foreach ($file in $files) {
        $report += "`n  $($file.path)"
    }

    $report += @"

--------------------------------------------------------------------------------
RECOMMENDED CAPTURE LIST
--------------------------------------------------------------------------------
These files should be captured by the module:

"@

    foreach ($file in $configFiles) {
        $tokenized = ConvertTo-LogicalToken -Path $file.path
        $report += "  [CAPTURE] $tokenized`n"
    }
    
    if ($configFiles.Count -eq 0) {
        $report += "  (no config files detected)`n"
    }

    $report += @"

--------------------------------------------------------------------------------
RECOMMENDED EXCLUDES
--------------------------------------------------------------------------------
These patterns should be excluded from capture:

"@

    $excludePatterns = @(
        "**\Cache\**",
        "**\Temp\**",
        "**\Logs\**",
        "**\*.log",
        "**\*.tmp"
    )
    
    foreach ($pattern in $excludePatterns) {
        $report += "  $pattern`n"
    }

    $report += @"

--------------------------------------------------------------------------------
SENSITIVE CANDIDATES (DO NOT AUTO-RESTORE)
--------------------------------------------------------------------------------
"@

    $knownSensitive = @(
        "~/.git-credentials",
        "~/.config/git/credentials",
        "%APPDATA%/git/credentials",
        "~/.ssh/*",
        "~/.gnupg/*"
    )
    
    $report += "Known sensitive paths (always excluded):`n"
    foreach ($s in $knownSensitive) {
        $report += "  [SENSITIVE] $s`n"
    }
    
    if ($sensitiveFiles.Count -gt 0) {
        $report += "`nDetected sensitive files in this run:`n"
        foreach ($file in $sensitiveFiles) {
            $report += "  [DETECTED] $($file.path)`n"
        }
    } else {
        $report += "`n  (no sensitive files detected in diff - credentials properly excluded)`n"
    }

    $report += @"

--------------------------------------------------------------------------------
MODULE RECOMMENDATIONS
--------------------------------------------------------------------------------
1. Verify: Use 'command-exists' for 'git' (config files are optional)
2. Restore: Copy config files with 'optional: true'
3. Sensitive: Mark credential files as 'restorer: warn-only'
4. Notes: Document that users must re-authenticate after restore

================================================================================
                              END OF REPORT
================================================================================
"@

    # Write report
    $report | Out-File -FilePath $OutputPath -Encoding UTF8
    return $OutputPath
}

function Invoke-LocalCuration {
    <#
    .SYNOPSIS
        Run curation workflow locally (no sandbox).
    #>
    param(
        [string]$OutDir,
        [switch]$SkipInstall,
        [switch]$DryRun
    )
    
    Write-Header "Git Curation (Local Mode)"
    Write-Warn "Running locally - will modify your .gitconfig!"
    
    # Step 1: Ensure Git is installed
    if (-not $SkipInstall -and -not $DryRun) {
        Write-Step "Checking Git installation..."
        $gitCmd = Get-Command git -ErrorAction SilentlyContinue
        if (-not $gitCmd) {
            Write-Step "Installing Git via winget..."
            $result = & winget install --id Git.Git --silent --accept-package-agreements --accept-source-agreements 2>&1
            if ($LASTEXITCODE -ne 0) {
                Write-Error "Failed to install Git: $result"
                exit 1
            }
            # Refresh PATH
            $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
        }
        Write-Pass "Git is installed"
    }
    
    # Step 2: Capture pre-state snapshot (just config files)
    Write-Step "Capturing pre-seeding state..."
    $preSnapshot = @{}
    $gitConfigPath = "$env:USERPROFILE\.gitconfig"
    $gitConfigDir = "$env:USERPROFILE\.config\git"
    
    if (Test-Path $gitConfigPath) {
        $preSnapshot[$gitConfigPath] = Get-FileHash -Path $gitConfigPath -Algorithm SHA256 -ErrorAction SilentlyContinue
    }
    if (Test-Path $gitConfigDir) {
        Get-ChildItem -Path $gitConfigDir -File -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
            $preSnapshot[$_.FullName] = Get-FileHash -Path $_.FullName -Algorithm SHA256 -ErrorAction SilentlyContinue
        }
    }
    Write-Info "Pre-state: $($preSnapshot.Count) files tracked"
    
    # Step 3: Seed Git config
    if (-not $DryRun) {
        Write-Step "Seeding Git configuration..."
        & $script:SeedScript -Scope global
        if ($LASTEXITCODE -ne 0) {
            Write-Error "Seeding failed"
            exit 1
        }
    } else {
        Write-Info "DRY RUN: Skipping seed"
    }
    
    # Step 4: Capture post-state
    Write-Step "Capturing post-seeding state..."
    $postSnapshot = @{}
    
    if (Test-Path $gitConfigPath) {
        $postSnapshot[$gitConfigPath] = Get-FileHash -Path $gitConfigPath -Algorithm SHA256 -ErrorAction SilentlyContinue
    }
    if (Test-Path $gitConfigDir) {
        Get-ChildItem -Path $gitConfigDir -File -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
            $postSnapshot[$_.FullName] = Get-FileHash -Path $_.FullName -Algorithm SHA256 -ErrorAction SilentlyContinue
        }
    }
    Write-Info "Post-state: $($postSnapshot.Count) files tracked"
    
    # Step 5: Compute diff
    Write-Step "Computing diff..."
    $added = @()
    $modified = @()
    
    foreach ($path in $postSnapshot.Keys) {
        if (-not $preSnapshot.ContainsKey($path)) {
            $added += @{ path = $path; isDirectory = $false }
        } elseif ($preSnapshot[$path].Hash -ne $postSnapshot[$path].Hash) {
            $modified += @{ path = $path; isDirectory = $false }
        }
    }
    
    $diff = @{
        added = $added
        modified = $modified
    }
    
    # Write diff
    $diffPath = Join-Path $OutDir "diff.json"
    $diff | ConvertTo-Json -Depth 5 | Out-File -FilePath $diffPath -Encoding UTF8
    Write-Pass "Diff saved: $diffPath"
    Write-Info "Added: $($added.Count), Modified: $($modified.Count)"
    
    # Step 6: Generate report
    Write-Step "Generating curation report..."
    $reportPath = Join-Path $OutDir "CURATION_REPORT.txt"
    New-CurationReport -WingetId "Git.Git" -DiffJsonPath $diffPath -OutputPath $reportPath | Out-Null
    Write-Pass "Report saved: $reportPath"
    
    # Step 7: Copy current module as reference
    $moduleSource = Join-Path $script:ModulesDir "git\module.jsonc"
    $moduleDest = Join-Path $OutDir "module.jsonc"
    if (Test-Path $moduleSource) {
        Copy-Item -Path $moduleSource -Destination $moduleDest -Force
        Write-Pass "Module copied: $moduleDest"
    }
    
    return @{
        DiffPath = $diffPath
        ReportPath = $reportPath
        ModulePath = $moduleDest
    }
}

function Invoke-SandboxCuration {
    <#
    .SYNOPSIS
        Run curation workflow in Windows Sandbox.
    #>
    param(
        [string]$OutDir,
        [switch]$DryRun
    )
    
    Write-Header "Git Curation (Sandbox Mode)"
    
    # Use existing sandbox-discovery.ps1 with Git.Git
    $discoveryScript = Join-Path $script:RepoRoot "scripts\sandbox-discovery.ps1"
    
    if (-not (Test-Path $discoveryScript)) {
        Write-Error "sandbox-discovery.ps1 not found: $discoveryScript"
        exit 1
    }
    
    # Run discovery with Git.Git
    Write-Step "Launching sandbox discovery for Git.Git..."
    $discoveryArgs = @(
        "-WingetId", "Git.Git",
        "-OutDir", $OutDir
    )
    if ($DryRun) { $discoveryArgs += "-DryRun" }
    
    & $discoveryScript @discoveryArgs
    $exitCode = $LASTEXITCODE
    
    if ($exitCode -ne 0) {
        Write-Error "Sandbox discovery failed with exit code $exitCode"
        exit $exitCode
    }
    
    # Generate curation report on top of discovery
    $diffPath = Join-Path $OutDir "diff.json"
    if (Test-Path $diffPath) {
        Write-Step "Generating curation report..."
        $reportPath = Join-Path $OutDir "CURATION_REPORT.txt"
        New-CurationReport -WingetId "Git.Git" -DiffJsonPath $diffPath -OutputPath $reportPath | Out-Null
        Write-Pass "Report saved: $reportPath"
    }
    
    return @{
        DiffPath = $diffPath
        ReportPath = (Join-Path $OutDir "CURATION_REPORT.txt")
        ModulePath = (Join-Path $OutDir "module.jsonc")
    }
}

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Git Curation Workflow"

# Setup output directory
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
if (-not $OutDir) {
    $OutDir = Join-Path $script:RepoRoot "sandbox-tests\curation\git\$timestamp"
}

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}

Write-Info "Mode: $Mode"
Write-Info "Output: $OutDir"
Write-Info "Skip Install: $SkipInstall"
Write-Info "Dry Run: $DryRun"
Write-Info "Write Module: $WriteModule"

# Run appropriate mode
if ($Mode -eq 'local') {
    $result = Invoke-LocalCuration -OutDir $OutDir -SkipInstall:$SkipInstall -DryRun:$DryRun
} else {
    $result = Invoke-SandboxCuration -OutDir $OutDir -DryRun:$DryRun
}

# Write module if requested (-Promote is alias for -WriteModule)
$shouldWriteModule = $WriteModule -or $Promote
if ($shouldWriteModule -and $result.ModulePath -and (Test-Path $result.ModulePath)) {
    Write-Step "Writing module to modules/apps/git/..."
    $targetDir = Join-Path $script:ModulesDir "git"
    $targetPath = Join-Path $targetDir "module.jsonc"
    
    if (-not (Test-Path $targetDir)) {
        New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
    }
    
    Copy-Item -Path $result.ModulePath -Destination $targetPath -Force
    Write-Pass "Module written: $targetPath"
}

# Summary
Write-Header "Curation Complete"

Write-Host "  Output directory: $OutDir" -ForegroundColor Green
Write-Host ""

if (Test-Path $result.ReportPath) {
    Write-Host "  Report:" -ForegroundColor White
    Write-Host ""
    Get-Content $result.ReportPath | Select-Object -First 40 | ForEach-Object { Write-Host "  $_" -ForegroundColor Gray }
    Write-Host "  ..." -ForegroundColor Gray
    Write-Host ""
    Write-Host "  Full report: $($result.ReportPath)" -ForegroundColor Green
}

Write-Host ""
exit 0
