<#
.SYNOPSIS
    Provisioning CLI - Machine provisioning and configuration management.

.DESCRIPTION
    Transforms a machine from an unknown state into a known, verified desired state.
    Installs software, restores configuration, applies system preferences, and verifies outcomes.

.PARAMETER Command
    The command to execute: capture, plan, apply, verify, doctor, report

.PARAMETER Manifest
    Path to the manifest file (JSONC/JSON/YAML) describing desired state.

.PARAMETER OutManifest
    Output path for the captured manifest (used with capture command).

.PARAMETER Profile
    Profile name for capture. Writes to manifests/<profile>.jsonc by default.

.PARAMETER IncludeRuntimes
    Include runtime/framework packages in capture (vcredist, .NET, etc.). Default: false.

.PARAMETER IncludeStoreApps
    Include Microsoft Store apps in capture. Default: false.

.PARAMETER Minimize
    Drop entries without stable refs during capture.

.PARAMETER IncludeRestoreTemplate
    Generate restore template file during capture (requires -Profile).

.PARAMETER IncludeVerifyTemplate
    Generate verify template file during capture (requires -Profile).

.PARAMETER Discover
    Enable discovery mode during capture: detect software present but not winget-managed.

.PARAMETER DiscoverWriteManualInclude
    Generate manual include file with commented suggestions (requires -Profile).
    Default: true when -Discover is enabled.

.PARAMETER DryRun
    Preview changes without applying them.

.EXAMPLE
    .\cli.ps1 -Command capture -Profile my-machine
    Capture current machine state into manifests/my-machine.jsonc.

.EXAMPLE
    .\cli.ps1 -Command capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate
    Capture with restore and verify templates generated.

.EXAMPLE
    .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc
    Capture current machine state into a manifest (legacy mode).

.EXAMPLE
    .\cli.ps1 -Command plan -Manifest .\manifests\my-machine.jsonc
    Generate execution plan from manifest.

.EXAMPLE
    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc -DryRun
    Preview what would be applied.

.EXAMPLE
    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc
    Apply the manifest to the current machine.

.EXAMPLE
    .\cli.ps1 -Command verify -Manifest .\manifests\my-machine.jsonc
    Verify current state matches manifest.

.EXAMPLE
    .\cli.ps1 -Command doctor
    Diagnose environment issues.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet("capture", "plan", "apply", "verify", "doctor", "report", "diff", "restore")]
    [string]$Command,

    [Parameter(Mandatory = $false)]
    [string]$Manifest,

    [Parameter(Mandatory = $false)]
    [string]$OutManifest,

    [Parameter(Mandatory = $false)]
    [string]$Profile,

    [Parameter(Mandatory = $false)]
    [switch]$IncludeRuntimes,

    [Parameter(Mandatory = $false)]
    [switch]$IncludeStoreApps,

    [Parameter(Mandatory = $false)]
    [switch]$Minimize,

    [Parameter(Mandatory = $false)]
    [switch]$IncludeRestoreTemplate,

    [Parameter(Mandatory = $false)]
    [switch]$IncludeVerifyTemplate,

    [Parameter(Mandatory = $false)]
    [switch]$Discover,

    [Parameter(Mandatory = $false)]
    [Nullable[bool]]$DiscoverWriteManualInclude = $null,

    [Parameter(Mandatory = $false)]
    [switch]$DryRun,

    [Parameter(Mandatory = $false)]
    [switch]$EnableRestore,

    [Parameter(Mandatory = $false)]
    [string]$FileA,

    [Parameter(Mandatory = $false)]
    [string]$FileB,

    [Parameter(Mandatory = $false)]
    [switch]$Json
)

$ErrorActionPreference = "Stop"
$script:ProvisioningRoot = $PSScriptRoot

function Show-Help {
    Write-Host ""
    Write-Host "Provisioning CLI" -ForegroundColor Cyan
    Write-Host "================" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Machine provisioning and configuration management."
    Write-Host "Transforms a machine from unknown state to known, verified desired state."
    Write-Host ""
    Write-Host "USAGE:" -ForegroundColor Yellow
    Write-Host "    .\cli.ps1 -Command <command> [-Manifest <path>] [-OutManifest <path>] [-DryRun]"
    Write-Host ""
    Write-Host "COMMANDS:" -ForegroundColor Yellow
    Write-Host "    capture   Capture current machine state into a manifest"
    Write-Host "    plan      Generate execution plan from manifest"
    Write-Host "    apply     Execute the plan (use -DryRun to preview)"
    Write-Host "    restore   Restore configuration files from manifest (requires -EnableRestore)"
    Write-Host "    verify    Check current state against manifest"
    Write-Host "    doctor    Diagnose environment issues"
    Write-Host "    report    Show history of previous runs"
    Write-Host "    diff      Compare two plan/run artifacts"
    Write-Host ""
    Write-Host "OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>       Path to manifest file (JSONC/JSON/YAML)"
    Write-Host "    -Profile <name>        Profile name for capture (writes to manifests/<name>.jsonc)"
    Write-Host "    -OutManifest <path>    Output path for captured manifest (overrides -Profile)"
    Write-Host "    -DryRun                Preview changes without applying"
    Write-Host "    -EnableRestore         Enable restore operations (opt-in for safety)"
    Write-Host "    -FileA <path>          First artifact file for diff"
    Write-Host "    -FileB <path>          Second artifact file for diff"
    Write-Host "    -Json                  Output diff as JSON"
    Write-Host ""
    Write-Host "CAPTURE OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -IncludeRuntimes       Include runtime packages (vcredist, .NET, etc.)"
    Write-Host "    -IncludeStoreApps      Include Microsoft Store apps"
    Write-Host "    -Minimize              Drop entries without stable refs"
    Write-Host "    -IncludeRestoreTemplate  Generate restore template (requires -Profile)"
    Write-Host "    -IncludeVerifyTemplate   Generate verify template (requires -Profile)"
    Write-Host ""
    Write-Host "EXAMPLES:" -ForegroundColor Yellow
    Write-Host "    .\cli.ps1 -Command capture -Profile my-machine"
    Write-Host "    .\cli.ps1 -Command capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate"
    Write-Host "    .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command plan -Manifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc -DryRun"
    Write-Host "    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore"
    Write-Host "    .\cli.ps1 -Command verify -Manifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command doctor"
    Write-Host ""
    Write-Host "WORKFLOW:" -ForegroundColor Yellow
    Write-Host "    1. capture  -> Creates manifest from current machine"
    Write-Host "    2. plan     -> Shows what would change on target machine"
    Write-Host "    3. apply    -> Executes the plan (use -DryRun first!)"
    Write-Host "    4. verify   -> Confirms desired state is achieved"
    Write-Host ""
}

function Invoke-ProvisioningCapture {
    param(
        [string]$ProfileName,
        [string]$OutManifestPath,
        [bool]$IsIncludeRuntimes,
        [bool]$IsIncludeStoreApps,
        [bool]$IsMinimize,
        [bool]$IsIncludeRestoreTemplate,
        [bool]$IsIncludeVerifyTemplate,
        [bool]$IsDiscover,
        [Nullable[bool]]$DiscoverWriteManualIncludeValue
    )
    
    # Validate: need either -Profile or -OutManifest
    if (-not $ProfileName -and -not $OutManifestPath) {
        Write-Host "[ERROR] Either -Profile or -OutManifest is required for 'capture' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command capture -Profile <name>" -ForegroundColor Yellow
        Write-Host "   or: .\cli.ps1 -Command capture -OutManifest <path>" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "Examples:" -ForegroundColor DarkGray
        Write-Host "  .\cli.ps1 -Command capture -Profile my-machine" -ForegroundColor DarkGray
        Write-Host "  .\cli.ps1 -Command capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate" -ForegroundColor DarkGray
        Write-Host "  .\cli.ps1 -Command capture -Profile my-machine -Discover" -ForegroundColor DarkGray
        Write-Host "  .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc" -ForegroundColor DarkGray
        exit 1
    }
    
    . "$script:ProvisioningRoot\engine\capture.ps1"
    
    $captureParams = @{}
    if ($ProfileName) { $captureParams.Profile = $ProfileName }
    if ($OutManifestPath) { $captureParams.OutManifest = $OutManifestPath }
    if ($IsIncludeRuntimes) { $captureParams.IncludeRuntimes = $true }
    if ($IsIncludeStoreApps) { $captureParams.IncludeStoreApps = $true }
    if ($IsMinimize) { $captureParams.Minimize = $true }
    if ($IsIncludeRestoreTemplate) { $captureParams.IncludeRestoreTemplate = $true }
    if ($IsIncludeVerifyTemplate) { $captureParams.IncludeVerifyTemplate = $true }
    if ($IsDiscover) { $captureParams.Discover = $true }
    if ($null -ne $DiscoverWriteManualIncludeValue) { $captureParams.DiscoverWriteManualInclude = $DiscoverWriteManualIncludeValue }
    
    $result = Invoke-Capture @captureParams
    return $result
}

function Invoke-ProvisioningPlan {
    param([string]$ManifestPath)
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'plan' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command plan -Manifest <path>" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\plan.ps1"
    
    $result = Invoke-Plan -ManifestPath $ManifestPath
    return $result
}

function Invoke-ProvisioningApply {
    param(
        [string]$ManifestPath,
        [bool]$IsDryRun,
        [bool]$IsEnableRestore = $false
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'apply' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command apply -Manifest <path> [-DryRun] [-EnableRestore]" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\apply.ps1"
    
    $result = Invoke-Apply -ManifestPath $ManifestPath -DryRun:$IsDryRun -EnableRestore:$IsEnableRestore
    return $result
}

function Invoke-ProvisioningVerify {
    param([string]$ManifestPath)
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'verify' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command verify -Manifest <path>" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\verify.ps1"
    
    $result = Invoke-Verify -ManifestPath $ManifestPath
    return $result
}

function Invoke-ProvisioningDoctor {
    Write-Host ""
    Write-Host "Provisioning Doctor" -ForegroundColor Cyan
    Write-Host "==================" -ForegroundColor Cyan
    Write-Host ""
    
    # Check winget
    Write-Host "Checking winget... " -NoNewline
    try {
        $null = Get-Command winget -ErrorAction Stop
        $wingetVersion = (winget --version 2>&1) | Select-Object -First 1
        Write-Host "OK ($wingetVersion)" -ForegroundColor Green
    } catch {
        Write-Host "NOT FOUND" -ForegroundColor Red
        Write-Host "  winget is required for app installation" -ForegroundColor Yellow
    }
    
    # Check directories
    Write-Host ""
    Write-Host "Checking directories..." -ForegroundColor Cyan
    
    $dirs = @(
        @{ Name = "engine"; Path = "$script:ProvisioningRoot\engine" }
        @{ Name = "drivers"; Path = "$script:ProvisioningRoot\drivers" }
        @{ Name = "restorers"; Path = "$script:ProvisioningRoot\restorers" }
        @{ Name = "verifiers"; Path = "$script:ProvisioningRoot\verifiers" }
        @{ Name = "plans"; Path = "$script:ProvisioningRoot\plans" }
        @{ Name = "state"; Path = "$script:ProvisioningRoot\state" }
        @{ Name = "logs"; Path = "$script:ProvisioningRoot\logs" }
        @{ Name = "manifests"; Path = "$script:ProvisioningRoot\manifests" }
    )
    
    foreach ($dir in $dirs) {
        Write-Host "  $($dir.Name): " -NoNewline
        if (Test-Path $dir.Path) {
            Write-Host "OK" -ForegroundColor Green
        } else {
            Write-Host "MISSING" -ForegroundColor Yellow
        }
    }
    
    # Check for existing manifests
    Write-Host ""
    Write-Host "Checking manifests..." -ForegroundColor Cyan
    $manifestsDir = "$script:ProvisioningRoot\manifests"
    if (Test-Path $manifestsDir) {
        $manifests = Get-ChildItem -Path $manifestsDir -Filter "*.yaml" -ErrorAction SilentlyContinue
        if ($manifests.Count -gt 0) {
            Write-Host "  Found $($manifests.Count) manifest(s):" -ForegroundColor Green
            foreach ($m in $manifests) {
                Write-Host "    - $($m.Name)" -ForegroundColor DarkGray
            }
        } else {
            Write-Host "  No manifests found" -ForegroundColor Yellow
            Write-Host "  Run: .\cli.ps1 -Command capture" -ForegroundColor DarkGray
        }
    } else {
        Write-Host "  Manifests directory not found" -ForegroundColor Yellow
    }
    
    Write-Host ""
    Write-Host "Doctor check complete." -ForegroundColor Cyan
    Write-Host ""
}

function Invoke-ProvisioningReport {
    Write-Host ""
    Write-Host "Provisioning Report" -ForegroundColor Cyan
    Write-Host "==================" -ForegroundColor Cyan
    Write-Host ""
    
    $stateDir = "$script:ProvisioningRoot\state"
    
    if (-not (Test-Path $stateDir)) {
        Write-Host "No run history found." -ForegroundColor Yellow
        Write-Host "Run a command first to generate history." -ForegroundColor DarkGray
        return
    }
    
    $stateFiles = Get-ChildItem -Path $stateDir -Filter "*.json" -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 10
    
    if ($stateFiles.Count -eq 0) {
        Write-Host "No run history found." -ForegroundColor Yellow
        return
    }
    
    Write-Host "Recent runs (last 10):" -ForegroundColor Cyan
    Write-Host ""
    
    foreach ($file in $stateFiles) {
        try {
            $state = Get-Content -Path $file.FullName -Raw | ConvertFrom-Json
            
            $statusColor = if ($state.summary.failed -gt 0) { "Yellow" } else { "Green" }
            $dryRunTag = if ($state.dryRun) { " [DRY-RUN]" } else { "" }
            
            Write-Host "  $($state.timestamp) - $($state.command)$dryRunTag" -ForegroundColor $statusColor
            Write-Host "    Success: $($state.summary.success), Skipped: $($state.summary.skipped), Failed: $($state.summary.failed)" -ForegroundColor DarkGray
        } catch {
            Write-Host "  $($file.Name) - (corrupted)" -ForegroundColor Red
        }
    }
    
    Write-Host ""
}

function Invoke-ProvisioningDiff {
    param(
        [string]$FileAPath,
        [string]$FileBPath,
        [bool]$OutputJson
    )
    
    if (-not $FileAPath -or -not $FileBPath) {
        Write-Host "[ERROR] -FileA and -FileB are required for 'diff' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command diff -FileA <path> -FileB <path> [-Json]" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\diff.ps1"
    
    $artifactA = Read-ArtifactFile -Path $FileAPath
    if (-not $artifactA) {
        Write-Host "[ERROR] Could not read artifact A: $FileAPath" -ForegroundColor Red
        return $null
    }
    
    $artifactB = Read-ArtifactFile -Path $FileBPath
    if (-not $artifactB) {
        Write-Host "[ERROR] Could not read artifact B: $FileBPath" -ForegroundColor Red
        return $null
    }
    
    $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
    
    if ($OutputJson) {
        $json = ConvertTo-DiffJson -Diff $diff
        Write-Host $json
    } else {
        $labelA = Split-Path -Leaf $FileAPath
        $labelB = Split-Path -Leaf $FileBPath
        $output = Format-DiffOutput -Diff $diff -LabelA $labelA -LabelB $labelB
        Write-Host $output
    }
    
    return $diff
}

function Invoke-ProvisioningRestore {
    param(
        [string]$ManifestPath,
        [bool]$IsEnableRestore,
        [bool]$IsDryRun
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'restore' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command restore -Manifest <path> -EnableRestore [-DryRun]" -ForegroundColor Yellow
        return $null
    }
    
    if (-not (Test-Path $ManifestPath)) {
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\restore.ps1"
    
    $result = Invoke-Restore -ManifestPath $ManifestPath -EnableRestore:$IsEnableRestore -DryRun:$IsDryRun
    return $result
}

# Main execution
if (-not $Command) {
    Show-Help
    exit 0
}

switch ($Command) {
    "capture" { 
        Invoke-ProvisioningCapture `
            -ProfileName $Profile `
            -OutManifestPath $OutManifest `
            -IsIncludeRuntimes $IncludeRuntimes.IsPresent `
            -IsIncludeStoreApps $IncludeStoreApps.IsPresent `
            -IsMinimize $Minimize.IsPresent `
            -IsIncludeRestoreTemplate $IncludeRestoreTemplate.IsPresent `
            -IsIncludeVerifyTemplate $IncludeVerifyTemplate.IsPresent `
            -IsDiscover $Discover.IsPresent `
            -DiscoverWriteManualIncludeValue $DiscoverWriteManualInclude
    }
    "plan"    { Invoke-ProvisioningPlan -ManifestPath $Manifest }
    "apply"   { Invoke-ProvisioningApply -ManifestPath $Manifest -IsDryRun $DryRun.IsPresent -IsEnableRestore $EnableRestore.IsPresent }
    "restore" { Invoke-ProvisioningRestore -ManifestPath $Manifest -IsEnableRestore $EnableRestore.IsPresent -IsDryRun $DryRun.IsPresent }
    "verify"  { Invoke-ProvisioningVerify -ManifestPath $Manifest }
    "doctor"  { Invoke-ProvisioningDoctor }
    "report"  { Invoke-ProvisioningReport }
    "diff"    { Invoke-ProvisioningDiff -FileAPath $FileA -FileBPath $FileB -OutputJson $Json.IsPresent }
    default   { Show-Help }
}
