<#
.SYNOPSIS
    Sandbox-based discovery for generating module drafts from winget installations.

.DESCRIPTION
    Launches Windows Sandbox to install a winget package and capture filesystem
    changes. Generates a draft module.jsonc based on discovered file changes.
    
    This is the host-side entrypoint that:
    1. Creates a timestamped output directory
    2. Generates a .wsb sandbox configuration
    3. Launches Windows Sandbox with the discovery harness
    4. After sandbox completes, processes artifacts and generates module draft

.PARAMETER WingetId
    The winget package ID to install and discover (e.g., "Microsoft.PowerToys").

.PARAMETER OutDir
    Base output directory. Default: sandbox-tests/discovery/<WingetId>/<timestamp>

.PARAMETER Roots
    Directories to snapshot (array). Default: LOCALAPPDATA, APPDATA, ProgramData, ProgramFiles.

.PARAMETER WriteModule
    If set, write the draft module to modules/apps/<id>/module.jsonc.
    Default: write to the run output directory only.

.PARAMETER DryRun
    Skip winget install inside sandbox (validates wiring only).

.PARAMETER NoLaunch
    Generate .wsb file but don't launch sandbox (for manual testing).

.EXAMPLE
    .\scripts\sandbox-discovery.ps1 -WingetId "Microsoft.PowerToys"
    
.EXAMPLE
    .\scripts\sandbox-discovery.ps1 -WingetId "Guru3D.Afterburner" -DryRun
    
.EXAMPLE
    .\scripts\sandbox-discovery.ps1 -WingetId "Git.Git" -WriteModule
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$WingetId,
    
    [Parameter(Mandatory = $false)]
    [string]$OutDir,
    
    [Parameter(Mandatory = $false)]
    [string[]]$Roots,
    
    [Parameter(Mandatory = $false)]
    [switch]$WriteModule,
    
    [Parameter(Mandatory = $false)]
    [switch]$DryRun,
    
    [Parameter(Mandatory = $false)]
    [switch]$NoLaunch
)

$ErrorActionPreference = 'Stop'

# Resolve paths
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
$script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
$script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"

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

function Get-SanitizedId {
    param([string]$WingetId)
    # Convert winget ID to folder-safe name: Microsoft.PowerToys -> microsoft-powertoys
    $sanitized = $WingetId.ToLower() -replace '\.', '-'
    return $sanitized
}

function New-SandboxConfig {
    <#
    .SYNOPSIS
        Generate a .wsb sandbox configuration file.
    #>
    param(
        [string]$OutputPath,
        [string]$RepoRoot,
        [string]$WingetId,
        [string]$ArtifactDir,
        [string[]]$Roots,
        [switch]$DryRun
    )
    
    # Sandbox maps repo to C:\Endstate
    $sandboxRepoPath = "C:\Endstate"
    $sandboxArtifactPath = $ArtifactDir -replace [regex]::Escape($RepoRoot), $sandboxRepoPath
    
    # Build command line using powershell.exe (Windows PowerShell 5.1) with -File invocation
    # Use cmd.exe /c start to ensure a visible console window
    $scriptPath = "$sandboxRepoPath\sandbox-tests\discovery-harness\sandbox-install.ps1"
    
    # Build argument list for -File invocation
    $argList = @("-WingetId", "`"$WingetId`"", "-OutputDir", "`"$sandboxArtifactPath`"")
    if ($Roots) {
        $argList += @("-Roots", "`"$($Roots -join ',')`"")
    }
    if ($DryRun) {
        $argList += "-DryRun"
    }
    $argsString = $argList -join " "
    
    $command = "cmd.exe /c start `"`" powershell.exe -ExecutionPolicy Bypass -NoExit -File `"$scriptPath`" $argsString"
    
    $wsb = @"
<Configuration>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>$RepoRoot</HostFolder>
      <SandboxFolder>$sandboxRepoPath</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>$command</Command>
  </LogonCommand>
</Configuration>
"@
    
    $wsb | Out-File -FilePath $OutputPath -Encoding UTF8
    return $OutputPath
}

function New-ModuleDraft {
    <#
    .SYNOPSIS
        Generate a draft module.jsonc from diff results.
    #>
    param(
        [string]$WingetId,
        [string]$DiffJsonPath,
        [string]$OutputPath
    )
    
    if (-not (Test-Path $DiffJsonPath)) {
        Write-Error "Diff file not found: $DiffJsonPath"
        return $null
    }
    
    $diff = Get-Content -Path $DiffJsonPath -Raw | ConvertFrom-Json
    
    # Combine added and modified, filter junk
    $allChanges = @()
    if ($diff.added) { $allChanges += $diff.added }
    if ($diff.modified) { $allChanges += $diff.modified }
    
    # Filter out directories and apply exclude heuristics
    $files = @($allChanges | Where-Object { -not $_.isDirectory })
    $filtered = Apply-ExcludeHeuristics -Entries $files
    
    # Group by parent directory to create restore entries
    $restoreEntries = @()
    $seenDirs = @{}
    
    foreach ($entry in $filtered) {
        $parentDir = Split-Path -Parent $entry.path
        
        if (-not $seenDirs.ContainsKey($parentDir)) {
            $seenDirs[$parentDir] = @()
        }
        $seenDirs[$parentDir] += $entry
    }
    
    # Create restore entries for directories with files
    foreach ($dir in ($seenDirs.Keys | Sort-Object)) {
        $tokenizedPath = ConvertTo-LogicalToken -Path $dir
        
        $restoreEntry = @{
            type   = "copy"
            source = "./configs/$((Get-SanitizedId -WingetId $WingetId))"
            target = $tokenizedPath
            backup = $true
        }
        $restoreEntries += $restoreEntry
    }
    
    # Deduplicate restore entries by target
    $uniqueRestores = @()
    $seenTargets = @{}
    foreach ($r in $restoreEntries) {
        if (-not $seenTargets.ContainsKey($r.target)) {
            $seenTargets[$r.target] = $true
            $uniqueRestores += $r
        }
    }
    
    # Build module structure
    $sanitizedId = Get-SanitizedId -WingetId $WingetId
    
    $module = [ordered]@{
        id          = "apps.$sanitizedId"
        displayName = $WingetId
        sensitivity = "low"
        matches     = [ordered]@{
            winget = @($WingetId)
        }
        verify      = @()
        restore     = $uniqueRestores
        capture     = [ordered]@{
            files        = @()
            excludeGlobs = @(
                "**\Logs\**",
                "**\Temp\**",
                "**\Cache\**",
                "**\GPUCache\**",
                "**\Crashpad\**"
            )
        }
        notes       = "Auto-generated draft module for $WingetId. Review and customize before use."
    }
    
    # Add verify entries for each unique target
    foreach ($r in $uniqueRestores) {
        $module.verify += @{
            type = "file-exists"
            path = $r.target
        }
    }
    
    # Add capture entries
    foreach ($r in $uniqueRestores) {
        $module.capture.files += @{
            source   = $r.target
            dest     = "apps/$sanitizedId"
            optional = $true
        }
    }
    
    # Convert to JSON with comments header
    $jsonContent = $module | ConvertTo-Json -Depth 10
    
    # Add JSONC header comment
    $header = @"
{
  // Config Module: $WingetId (Auto-generated draft)
  // Generated by sandbox-discovery.ps1
  // Review and customize before committing
  
"@
    
    # Remove opening brace from JSON and prepend header
    $jsonBody = $jsonContent.Substring(1)
    $jsoncContent = $header + $jsonBody
    
    # Ensure parent directory exists
    $parentDir = Split-Path -Parent $OutputPath
    if (-not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    $jsoncContent | Out-File -FilePath $OutputPath -Encoding UTF8
    
    return $OutputPath
}

# ============================================================================
# MAIN
# ============================================================================

Write-Header "Sandbox Discovery: $WingetId"

# Create output directory
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$sanitizedId = Get-SanitizedId -WingetId $WingetId

if (-not $OutDir) {
    $OutDir = Join-Path $script:RepoRoot "sandbox-tests\discovery\$sanitizedId\$timestamp"
}

if (-not (Test-Path $OutDir)) {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
}

Write-Info "Winget ID: $WingetId"
Write-Info "Output directory: $OutDir"
Write-Info "Dry run: $DryRun"
Write-Info "Write module: $WriteModule"

# Step 1: Generate .wsb file
Write-Step "Generating sandbox configuration..."

$wsbPath = Join-Path $OutDir "discovery.wsb"
New-SandboxConfig -OutputPath $wsbPath -RepoRoot $script:RepoRoot -WingetId $WingetId -ArtifactDir $OutDir -Roots $Roots -DryRun:$DryRun | Out-Null
Write-Pass "Created: $wsbPath"

if ($NoLaunch) {
    Write-Info "NoLaunch specified - sandbox not started"
    Write-Info "To run manually: Start-Process `"$wsbPath`""
    Write-Host ""
    Write-Host "Output directory: $OutDir" -ForegroundColor Green
    exit 0
}

# Step 2: Launch sandbox
Write-Step "Launching Windows Sandbox..."
Write-Info "The sandbox will install $WingetId and capture filesystem changes."
Write-Info "Close the sandbox window when installation is complete."
Write-Host ""

Start-Process -FilePath $wsbPath -Wait

# Step 3: Check for artifacts
Write-Step "Checking for artifacts..."

$preJsonPath = Join-Path $OutDir "pre.json"
$postJsonPath = Join-Path $OutDir "post.json"
$diffJsonPath = Join-Path $OutDir "diff.json"
$doneFile = Join-Path $OutDir "DONE.txt"
$errorFile = Join-Path $OutDir "ERROR.txt"

$artifactsExist = (Test-Path $preJsonPath) -and (Test-Path $postJsonPath) -and (Test-Path $diffJsonPath)

if (-not $artifactsExist) {
    Write-Host "[ERROR] Artifacts not found. Sandbox may not have completed successfully." -ForegroundColor Red
    Write-Info "Expected files:"
    Write-Info "  - $preJsonPath"
    Write-Info "  - $postJsonPath"
    Write-Info "  - $diffJsonPath"
    
    # Check for ERROR.txt from sandbox-install.ps1
    if (Test-Path $errorFile) {
        Write-Host ""
        Write-Host "[ERROR] sandbox-install.ps1 reported an error:" -ForegroundColor Red
        Get-Content -Path $errorFile | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
    } else {
        Write-Host ""
        Write-Host "[HINT] LogonCommand likely failed to execute. Common causes:" -ForegroundColor Yellow
        Write-Host "  - powershell.exe not available or blocked" -ForegroundColor Yellow
        Write-Host "  - Execution policy preventing script execution" -ForegroundColor Yellow
        Write-Host "  - Script path or parameters malformed in .wsb" -ForegroundColor Yellow
        Write-Host "  - Windows Sandbox closed before script completed" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "  Check the generated .wsb file: $wsbPath" -ForegroundColor Yellow
    }
    exit 1
}

Write-Pass "Artifacts found"

# Step 4: Load and summarize diff
Write-Step "Processing diff..."

$diff = Get-Content -Path $diffJsonPath -Raw | ConvertFrom-Json
$addedCount = if ($diff.added) { $diff.added.Count } else { 0 }
$modifiedCount = if ($diff.modified) { $diff.modified.Count } else { 0 }

Write-Info "Added files: $addedCount"
Write-Info "Modified files: $modifiedCount"

# Apply exclude heuristics for summary
$allChanges = @()
if ($diff.added) { $allChanges += $diff.added }
if ($diff.modified) { $allChanges += $diff.modified }

$files = @($allChanges | Where-Object { -not $_.isDirectory })
$filtered = Apply-ExcludeHeuristics -Entries $files
$filteredCount = $filtered.Count

Write-Info "After filtering junk: $filteredCount files"

# Step 5: Generate module draft
Write-Step "Generating module draft..."

$draftPath = Join-Path $OutDir "module.jsonc"
New-ModuleDraft -WingetId $WingetId -DiffJsonPath $diffJsonPath -OutputPath $draftPath | Out-Null
Write-Pass "Created: $draftPath"

# Step 6: Optionally write to modules directory
if ($WriteModule) {
    Write-Step "Writing module to modules/apps/..."
    
    $moduleDir = Join-Path $script:ModulesDir $sanitizedId
    $modulePath = Join-Path $moduleDir "module.jsonc"
    
    if (-not (Test-Path $moduleDir)) {
        New-Item -ItemType Directory -Path $moduleDir -Force | Out-Null
    }
    
    Copy-Item -Path $draftPath -Destination $modulePath -Force
    Write-Pass "Created: $modulePath"
}

# Summary
Write-Header "Discovery Complete"

Write-Host "  Winget ID:      $WingetId" -ForegroundColor White
Write-Host "  Added files:    $addedCount" -ForegroundColor White
Write-Host "  Modified files: $modifiedCount" -ForegroundColor White
Write-Host "  Filtered:       $filteredCount (after excluding junk)" -ForegroundColor White
Write-Host ""
Write-Host "  Output folder:  $OutDir" -ForegroundColor Green
Write-Host "  Draft module:   $draftPath" -ForegroundColor Green

if ($WriteModule) {
    Write-Host "  Installed to:   $modulePath" -ForegroundColor Green
}

Write-Host ""
exit 0
