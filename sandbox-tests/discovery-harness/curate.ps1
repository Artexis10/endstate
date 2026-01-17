<#
.SYNOPSIS
    Data-driven curation runner for Endstate config modules.

.DESCRIPTION
    Generic, data-driven curation workflow that reads module.jsonc for all
    configuration. Supports sandbox-first execution with explicit safety gates
    for local mode operations.
    
    Pipeline stages:
    1. Load module by -ModuleId
    2. Resolve install source (winget)
    3. Install app (sandbox/local)
    4. Run seed (per curation.seed config)
    5. Snapshot filesystem (pre/post)
    6. Diff results
    7. Generate artifacts (CURATION_REPORT.txt, curation-report.json)
    8. Optionally validate and promote

    SAFETY: Local mode seeding requires BOTH -AllowHostMutation AND -Seed flags.

.PARAMETER ModuleId
    The module ID to curate (e.g., 'apps.git', 'apps.vscodium'). Mandatory.

.PARAMETER Mode
    Execution mode: 'sandbox' (default, safe) or 'local'.

.PARAMETER AllowHostMutation
    Required for local mode seeding. Acknowledges that local seeding will
    modify host filesystem state.

.PARAMETER Seed
    Enable seeding step. In local mode, requires -AllowHostMutation.
    In sandbox mode, seeding is always safe and enabled by default.

.PARAMETER SkipInstall
    Skip app installation (assumes app is already installed).

.PARAMETER Promote
    Promote the curated module to modules/apps/<app>/.

.PARAMETER Validate
    Run validation loop: capture -> wipe -> restore -> verify.

.PARAMETER OutDir
    Output directory for artifacts. Default: sandbox-tests/curation/<app>/<timestamp>

.EXAMPLE
    .\curate.ps1 -ModuleId apps.git
    # Curates Git in Windows Sandbox (default, safe)

.EXAMPLE
    .\curate.ps1 -ModuleId apps.git -Mode local
    # Local discovery without seeding (read-only)

.EXAMPLE
    .\curate.ps1 -ModuleId apps.git -Mode local -AllowHostMutation -Seed
    # Local discovery WITH seeding (dangerous, explicit triple opt-in)

.EXAMPLE
    .\curate.ps1 -ModuleId apps.vscodium -Promote
    # Curates VSCodium in sandbox and promotes module
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$ModuleId,
    
    [Parameter(Mandatory = $false)]
    [ValidateSet('sandbox', 'local')]
    [string]$Mode = 'sandbox',
    
    [Parameter(Mandatory = $false)]
    [switch]$AllowHostMutation,
    
    [Parameter(Mandatory = $false)]
    [switch]$Seed,
    
    [Parameter(Mandatory = $false)]
    [switch]$SkipInstall,
    
    [Parameter(Mandatory = $false)]
    [switch]$Promote,
    
    [Parameter(Mandatory = $false)]
    [switch]$Validate,
    
    [Parameter(Mandatory = $false)]
    [string]$OutDir
)

$ErrorActionPreference = 'Stop'

# ============================================================================
# PATH RESOLUTION
# ============================================================================

function Get-RepoRoot {
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
$script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"

# Load snapshot module
if (Test-Path $script:SnapshotModule) {
    . $script:SnapshotModule
}

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

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor DarkYellow
}

# ============================================================================
# MODULE LOADING
# ============================================================================

function Get-ModulePath {
    <#
    .SYNOPSIS
        Resolves module ID to module.jsonc path.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModuleId
    )
    
    # Parse module ID: apps.git -> apps/git
    $parts = $ModuleId -split '\.'
    if ($parts.Count -lt 2) {
        throw "Invalid module ID format: $ModuleId (expected: category.name, e.g., apps.git)"
    }
    
    $category = $parts[0]
    $name = ($parts[1..($parts.Count - 1)]) -join '.'
    
    # Resolve to path
    $modulePath = Join-Path $script:RepoRoot "modules\$category\$name\module.jsonc"
    
    if (-not (Test-Path $modulePath)) {
        throw "Module not found: $modulePath"
    }
    
    return $modulePath
}

function Read-ModuleConfig {
    <#
    .SYNOPSIS
        Loads and parses module.jsonc, stripping comments.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModulePath
    )
    
    $content = Get-Content -Path $ModulePath -Raw -Encoding UTF8
    
    # Strip JSONC comments (// and /* */)
    $content = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
    
    try {
        $module = $content | ConvertFrom-Json
        return $module
    }
    catch {
        throw "Failed to parse module.jsonc at $ModulePath`: $_"
    }
}

function Get-ModuleDir {
    <#
    .SYNOPSIS
        Gets the directory containing the module.jsonc.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ModulePath
    )
    
    return Split-Path -Parent $ModulePath
}

# ============================================================================
# SAFETY GATES
# ============================================================================

function Assert-LocalSeedingSafe {
    <#
    .SYNOPSIS
        Enforces triple opt-in for local seeding.
    #>
    param(
        [string]$Mode,
        [switch]$AllowHostMutation,
        [switch]$Seed
    )
    
    if ($Mode -eq 'local' -and $Seed) {
        if (-not $AllowHostMutation) {
            throw @"
SAFETY BLOCK: Local seeding requires explicit acknowledgment.

Local seeding will MODIFY your host filesystem. This is a dangerous operation
that should only be used for development/testing.

To proceed, you must specify BOTH flags:
  -AllowHostMutation -Seed

Example:
  .\curate.ps1 -ModuleId apps.git -Mode local -AllowHostMutation -Seed

If you only want to observe without seeding, omit -Seed:
  .\curate.ps1 -ModuleId apps.git -Mode local
"@
        }
        
        Write-Warn "LOCAL SEEDING ENABLED - Host filesystem will be modified!"
        Write-Host ""
    }
}

# ============================================================================
# INSTALL HELPERS
# ============================================================================

function Get-WingetId {
    <#
    .SYNOPSIS
        Extracts winget ID from module config.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Module
    )
    
    if ($Module.matches -and $Module.matches.winget -and $Module.matches.winget.Count -gt 0) {
        return $Module.matches.winget[0]
    }
    
    throw "Module does not specify a winget ID in matches.winget"
}

function Install-ViaWinget {
    <#
    .SYNOPSIS
        Installs an app via winget.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$WingetId
    )
    
    Write-Step "Installing $WingetId via winget..."
    
    $result = & winget install --id $WingetId --silent --accept-package-agreements --accept-source-agreements 2>&1
    
    if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne -1978335189) {
        # -1978335189 = already installed
        throw "winget install failed: $result"
    }
    
    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    
    Write-Pass "Installed: $WingetId"
}

# ============================================================================
# SEEDING
# ============================================================================

function Invoke-Seed {
    <#
    .SYNOPSIS
        Runs the seed script defined in module curation config.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Module,
        
        [Parameter(Mandatory = $true)]
        [string]$ModuleDir
    )
    
    if (-not $Module.curation -or -not $Module.curation.seed) {
        Write-Info "No seed configuration in module, skipping seeding"
        return
    }
    
    $seedConfig = $Module.curation.seed
    
    if ($seedConfig.type -eq 'script') {
        $seedScript = Join-Path $ModuleDir $seedConfig.script
        
        if (-not (Test-Path $seedScript)) {
            throw "Seed script not found: $seedScript"
        }
        
        Write-Step "Running seed script: $seedScript"
        & $seedScript
        
        if ($LASTEXITCODE -ne 0) {
            throw "Seed script failed with exit code $LASTEXITCODE"
        }
        
        Write-Pass "Seeding complete"
    }
    elseif ($seedConfig.type -eq 'inline') {
        Write-Warn "Inline seeding not yet implemented"
    }
    else {
        throw "Unknown seed type: $($seedConfig.type)"
    }
}

# ============================================================================
# SNAPSHOT & DIFF
# ============================================================================

function Get-SnapshotRoots {
    <#
    .SYNOPSIS
        Resolves snapshot roots from module curation config.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Module
    )
    
    $defaultRoots = @(
        $env:USERPROFILE,
        $env:APPDATA,
        $env:LOCALAPPDATA
    )
    
    if ($Module.curation -and $Module.curation.snapshotRoots) {
        $roots = @()
        foreach ($root in $Module.curation.snapshotRoots) {
            # Expand environment variables
            $expanded = [System.Environment]::ExpandEnvironmentVariables($root)
            if (Test-Path $expanded) {
                $roots += $expanded
            }
        }
        return $roots
    }
    
    return $defaultRoots
}

function New-CurationReport {
    <#
    .SYNOPSIS
        Generate human-readable and machine-readable curation reports.
    #>
    param(
        [string]$ModuleId,
        [string]$WingetId,
        [string]$DiffJsonPath,
        [string]$OutputDir,
        $Module
    )
    
    $diff = Get-Content -Path $DiffJsonPath -Raw | ConvertFrom-Json
    
    # Combine added and modified
    $allChanges = @()
    if ($diff.added) { $allChanges += $diff.added }
    if ($diff.modified) { $allChanges += $diff.modified }
    
    $files = @($allChanges | Where-Object { -not $_.isDirectory })
    
    # Build human-readable report
    $reportPath = Join-Path $OutputDir "CURATION_REPORT.txt"
    $report = @"
================================================================================
                         CURATION REPORT
================================================================================
Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
Module ID: $ModuleId
Winget ID: $WingetId

--------------------------------------------------------------------------------
SUMMARY
--------------------------------------------------------------------------------
Total files changed: $($files.Count)
  - Added:    $(if ($diff.added) { $diff.added.Count } else { 0 })
  - Modified: $(if ($diff.modified) { $diff.modified.Count } else { 0 })

--------------------------------------------------------------------------------
CHANGED FILES
--------------------------------------------------------------------------------
"@
    
    foreach ($file in $files) {
        $tokenized = if (Get-Command ConvertTo-LogicalToken -ErrorAction SilentlyContinue) {
            ConvertTo-LogicalToken -Path $file.path
        } else {
            $file.path
        }
        $report += "  $tokenized`n"
    }
    
    $report += @"

================================================================================
                              END OF REPORT
================================================================================
"@
    
    $report | Out-File -FilePath $reportPath -Encoding UTF8
    
    # Build machine-readable report
    $jsonReportPath = Join-Path $OutputDir "curation-report.json"
    $jsonReport = @{
        moduleId = $ModuleId
        wingetId = $WingetId
        timestamp = (Get-Date -Format 'o')
        summary = @{
            totalFiles = $files.Count
            added = if ($diff.added) { $diff.added.Count } else { 0 }
            modified = if ($diff.modified) { $diff.modified.Count } else { 0 }
        }
        files = @($files | ForEach-Object { $_.path })
    }
    
    $jsonReport | ConvertTo-Json -Depth 5 | Out-File -FilePath $jsonReportPath -Encoding UTF8
    
    return @{
        TextReport = $reportPath
        JsonReport = $jsonReportPath
    }
}

# ============================================================================
# LOCAL CURATION
# ============================================================================

function Invoke-LocalCuration {
    <#
    .SYNOPSIS
        Run curation workflow locally.
    #>
    param(
        [string]$ModuleId,
        $Module,
        [string]$ModuleDir,
        [string]$OutDir,
        [switch]$SkipInstall,
        [switch]$Seed
    )
    
    Write-Header "Local Curation: $ModuleId"
    
    $wingetId = Get-WingetId -Module $Module
    Write-Info "Winget ID: $wingetId"
    
    # Step 1: Install (if not skipped)
    if (-not $SkipInstall) {
        Install-ViaWinget -WingetId $wingetId
    } else {
        Write-Info "Skipping installation"
    }
    
    # Step 2: Capture pre-state
    Write-Step "Capturing pre-seeding state..."
    $roots = Get-SnapshotRoots -Module $Module
    Write-Info "Snapshot roots: $($roots -join ', ')"
    
    $preSnapshot = @()
    if (Get-Command Get-FilesystemSnapshot -ErrorAction SilentlyContinue) {
        $preSnapshot = Get-FilesystemSnapshot -Roots $roots -MaxDepth 5
    }
    Write-Info "Pre-state: $($preSnapshot.Count) entries"
    
    # Step 3: Run seed (if enabled)
    if ($Seed) {
        Invoke-Seed -Module $Module -ModuleDir $ModuleDir
    } else {
        Write-Info "Seeding skipped (use -Seed to enable)"
    }
    
    # Step 4: Capture post-state
    Write-Step "Capturing post-seeding state..."
    $postSnapshot = @()
    if (Get-Command Get-FilesystemSnapshot -ErrorAction SilentlyContinue) {
        $postSnapshot = Get-FilesystemSnapshot -Roots $roots -MaxDepth 5
    }
    Write-Info "Post-state: $($postSnapshot.Count) entries"
    
    # Step 5: Compute diff
    Write-Step "Computing diff..."
    $diff = @{ added = @(); modified = @() }
    if (Get-Command Compare-FilesystemSnapshots -ErrorAction SilentlyContinue) {
        $diff = Compare-FilesystemSnapshots -PreSnapshot $preSnapshot -PostSnapshot $postSnapshot
    }
    
    $diffPath = Join-Path $OutDir "diff.json"
    $diff | ConvertTo-Json -Depth 5 | Out-File -FilePath $diffPath -Encoding UTF8
    Write-Pass "Diff saved: $diffPath"
    
    # Step 6: Generate reports
    Write-Step "Generating reports..."
    $reports = New-CurationReport -ModuleId $ModuleId -WingetId $wingetId -DiffJsonPath $diffPath -OutputDir $OutDir -Module $Module
    Write-Pass "Reports generated"
    
    return @{
        DiffPath = $diffPath
        ReportPath = $reports.TextReport
        JsonReportPath = $reports.JsonReport
    }
}

# ============================================================================
# SANDBOX CURATION
# ============================================================================

function Invoke-SandboxCuration {
    <#
    .SYNOPSIS
        Run curation workflow in Windows Sandbox.
    #>
    param(
        [string]$ModuleId,
        $Module,
        [string]$ModuleDir,
        [string]$OutDir
    )
    
    Write-Header "Sandbox Curation: $ModuleId"
    
    $wingetId = Get-WingetId -Module $Module
    Write-Info "Winget ID: $wingetId"
    
    # Use sandbox-discovery.ps1 for sandbox execution
    $discoveryScript = Join-Path $script:RepoRoot "scripts\sandbox-discovery.ps1"
    
    if (-not (Test-Path $discoveryScript)) {
        throw "sandbox-discovery.ps1 not found: $discoveryScript"
    }
    
    Write-Step "Launching sandbox discovery..."
    & $discoveryScript -WingetId $wingetId -OutDir $OutDir
    
    if ($LASTEXITCODE -ne 0) {
        throw "Sandbox discovery failed with exit code $LASTEXITCODE"
    }
    
    # Generate curation report from discovery results
    $diffPath = Join-Path $OutDir "diff.json"
    if (Test-Path $diffPath) {
        Write-Step "Generating curation reports..."
        $reports = New-CurationReport -ModuleId $ModuleId -WingetId $wingetId -DiffJsonPath $diffPath -OutputDir $OutDir -Module $Module
        Write-Pass "Reports generated"
        
        return @{
            DiffPath = $diffPath
            ReportPath = $reports.TextReport
            JsonReportPath = $reports.JsonReport
        }
    }
    
    return @{
        DiffPath = $diffPath
        ReportPath = $null
        JsonReportPath = $null
    }
}

# ============================================================================
# PROMOTION
# ============================================================================

function Invoke-Promotion {
    <#
    .SYNOPSIS
        Promotes curated module to modules directory.
    #>
    param(
        [string]$ModuleId,
        [string]$OutDir,
        [string]$ModulePath
    )
    
    $draftPath = Join-Path $OutDir "module.jsonc"
    
    if (-not (Test-Path $draftPath)) {
        Write-Warn "No draft module found at $draftPath, skipping promotion"
        return
    }
    
    $targetDir = Split-Path -Parent $ModulePath
    $targetPath = $ModulePath
    
    Write-Step "Promoting module to: $targetPath"
    Copy-Item -Path $draftPath -Destination $targetPath -Force
    Write-Pass "Module promoted"
}

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Endstate Data-Driven Curation Runner"

# Display configuration
Write-Info "Module ID:         $ModuleId"
Write-Info "Mode:              $Mode"
Write-Info "AllowHostMutation: $AllowHostMutation"
Write-Info "Seed:              $Seed"
Write-Info "SkipInstall:       $SkipInstall"
Write-Info "Promote:           $Promote"
Write-Info "Validate:          $Validate"

# Step 1: Safety checks
Write-Header "Step 1: Safety Checks"
Assert-LocalSeedingSafe -Mode $Mode -AllowHostMutation:$AllowHostMutation -Seed:$Seed
Write-Pass "Safety checks passed"

# Step 2: Load module
Write-Header "Step 2: Load Module"
try {
    $modulePath = Get-ModulePath -ModuleId $ModuleId
    Write-Info "Module path: $modulePath"
    
    $module = Read-ModuleConfig -ModulePath $modulePath
    Write-Info "Module loaded: $($module.displayName)"
    
    $moduleDir = Get-ModuleDir -ModulePath $modulePath
    Write-Info "Module dir: $moduleDir"
    
    # Check for curation config
    if (-not $module.curation) {
        Write-Warn "Module does not have a curation block - using defaults"
    } else {
        Write-Pass "Curation config found"
    }
}
catch {
    Write-Fail "Failed to load module: $_"
    exit 1
}

# Step 3: Setup output directory
Write-Header "Step 3: Setup Output"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$appName = ($ModuleId -split '\.')[-1]

if (-not $OutDir) {
    $OutDir = Join-Path $script:RepoRoot "sandbox-tests\curation\$appName\$timestamp"
}

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}
Write-Info "Output directory: $OutDir"

# Step 4: Run curation
Write-Header "Step 4: Execute Curation"
try {
    if ($Mode -eq 'local') {
        $result = Invoke-LocalCuration `
            -ModuleId $ModuleId `
            -Module $module `
            -ModuleDir $moduleDir `
            -OutDir $OutDir `
            -SkipInstall:$SkipInstall `
            -Seed:$Seed
    } else {
        $result = Invoke-SandboxCuration `
            -ModuleId $ModuleId `
            -Module $module `
            -ModuleDir $moduleDir `
            -OutDir $OutDir
    }
    
    Write-Pass "Curation completed"
}
catch {
    Write-Fail "Curation failed: $_"
    exit 1
}

# Step 5: Promote (if requested)
if ($Promote) {
    Write-Header "Step 5: Promote Module"
    Invoke-Promotion -ModuleId $ModuleId -OutDir $OutDir -ModulePath $modulePath
}

# Step 6: Validate (if requested)
if ($Validate) {
    Write-Header "Step 6: Validate"
    Write-Warn "Validation loop not yet implemented"
    # TODO: capture -> wipe -> restore -> verify
}

# Summary
Write-Header "Curation Complete"

Write-Host "  Module ID:   $ModuleId" -ForegroundColor Green
Write-Host "  Mode:        $Mode" -ForegroundColor Green
Write-Host "  Output:      $OutDir" -ForegroundColor Green
Write-Host ""

if ($result.ReportPath -and (Test-Path $result.ReportPath)) {
    Write-Host "  Reports:" -ForegroundColor White
    Write-Host "    - $($result.ReportPath)" -ForegroundColor Gray
    Write-Host "    - $($result.JsonReportPath)" -ForegroundColor Gray
    Write-Host ""
}

exit 0
