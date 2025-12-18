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
    Output path for the captured manifest (required for capture command).

.PARAMETER DryRun
    Preview changes without applying them.

.EXAMPLE
    .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc
    Capture current machine state into a manifest.

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
    [ValidateSet("capture", "plan", "apply", "verify", "doctor", "report")]
    [string]$Command,

    [Parameter(Mandatory = $false)]
    [string]$Manifest,

    [Parameter(Mandatory = $false)]
    [string]$OutManifest,

    [Parameter(Mandatory = $false)]
    [switch]$DryRun
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
    Write-Host "    verify    Check current state against manifest"
    Write-Host "    doctor    Diagnose environment issues"
    Write-Host "    report    Show history of previous runs"
    Write-Host ""
    Write-Host "OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>       Path to manifest file (JSONC/JSON/YAML)"
    Write-Host "    -OutManifest <path>    Output path for captured manifest (required for capture)"
    Write-Host "    -DryRun                Preview changes without applying"
    Write-Host ""
    Write-Host "EXAMPLES:" -ForegroundColor Yellow
    Write-Host "    .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command plan -Manifest .\manifests\my-machine.jsonc"
    Write-Host "    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc -DryRun"
    Write-Host "    .\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc"
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
    param([string]$OutManifestPath)
    
    if (-not $OutManifestPath) {
        Write-Host "[ERROR] -OutManifest is required for 'capture' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command capture -OutManifest <path>" -ForegroundColor Yellow
        Write-Host "Example: .\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc" -ForegroundColor DarkGray
        exit 1
    }
    
    . "$script:ProvisioningRoot\engine\capture.ps1"
    
    $result = Invoke-Capture -OutManifest $OutManifestPath
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
        [bool]$IsDryRun
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'apply' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command apply -Manifest <path> [-DryRun]" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\apply.ps1"
    
    if ($IsDryRun) {
        $result = Invoke-Apply -ManifestPath $ManifestPath -DryRun
    } else {
        $result = Invoke-Apply -ManifestPath $ManifestPath
    }
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

# Main execution
if (-not $Command) {
    Show-Help
    exit 0
}

switch ($Command) {
    "capture" { Invoke-ProvisioningCapture -OutManifestPath $OutManifest }
    "plan"    { Invoke-ProvisioningPlan -ManifestPath $Manifest }
    "apply"   { Invoke-ProvisioningApply -ManifestPath $Manifest -IsDryRun $DryRun.IsPresent }
    "verify"  { Invoke-ProvisioningVerify -ManifestPath $Manifest }
    "doctor"  { Invoke-ProvisioningDoctor }
    "report"  { Invoke-ProvisioningReport }
    default   { Show-Help }
}
