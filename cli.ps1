# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

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

.PARAMETER Update
    Merge new capture into existing manifest instead of overwriting.
    Preserves includes, restore, verify blocks. Updates captured timestamp.

.PARAMETER PruneMissingApps
    When used with -Update, remove apps from root manifest that are no longer
    present in the new capture. Never prunes apps from included manifests.

.PARAMETER Json
    Output diff as JSON or machine-readable report output.

.PARAMETER RunId
    Specific run ID to retrieve for report command.

.PARAMETER Latest
    Select the most recent run for report command.

.PARAMETER Last
    Select the N most recent runs for report command.

.PARAMETER Plan
    Path to a previously generated plan file. When provided, apply executes
    that exact plan without recomputing actions. Mutually exclusive with -Manifest.

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
    [ValidateSet("capture", "plan", "apply", "verify", "doctor", "report", "diff", "restore", "capabilities", "export-config", "validate-export", "revert")]
    [string]$Command,

    [Parameter(Mandatory = $false)]
    [switch]$Version,

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
    [switch]$Update,

    [Parameter(Mandatory = $false)]
    [switch]$PruneMissingApps,

    [Parameter(Mandatory = $false)]
    [switch]$WithConfig,

    [Parameter(Mandatory = $false)]
    [string[]]$ConfigModules,

    [Parameter(Mandatory = $false)]
    [string]$PayloadOut,

    [Parameter(Mandatory = $false)]
    [string]$Plan,

    [Parameter(Mandatory = $false)]
    [switch]$DryRun,

    [Parameter(Mandatory = $false)]
    [switch]$EnableRestore,

    [Parameter(Mandatory = $false)]
    [string]$Export,

    [Parameter(Mandatory = $false)]
    [string]$FileA,

    [Parameter(Mandatory = $false)]
    [string]$FileB,

    [Parameter(Mandatory = $false)]
    [switch]$Json,

    [Parameter(Mandatory = $false)]
    [string]$RunId,

    [Parameter(Mandatory = $false)]
    [switch]$Latest,

    [Parameter(Mandatory = $false)]
    [int]$Last = 0,

    # Streaming events output format (jsonl for NDJSON to stderr)
    [Parameter(Mandatory = $false)]
    [ValidateSet("jsonl", "")]
    [string]$Events
)

$ErrorActionPreference = "Stop"
$script:ProvisioningRoot = $PSScriptRoot

function Get-ProvisioningVersion {
    <#
    .SYNOPSIS
        Returns the current version of the provisioning CLI.
    .DESCRIPTION
        If VERSION.txt exists (release build), returns its content.
        Otherwise returns dev version: 0.0.0-dev+<short git sha>
    #>
    $versionFile = Join-Path $script:ProvisioningRoot "VERSION.txt"
    
    if (Test-Path $versionFile) {
        $version = (Get-Content -Path $versionFile -Raw).Trim()
        return $version
    }
    
    # Dev version: try to get git sha
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return "0.0.0-dev+$gitSha"
        }
    } catch {
        # Git not available
    }
    
    return "0.0.0-dev"
}

function Get-GitSha {
    <#
    .SYNOPSIS
        Returns the current git commit SHA (short form), or $null if unavailable.
    #>
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return $gitSha.Trim()
        }
    } catch {
        # Git not available
    }
    return $null
}

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
    Write-Host "    capture          Capture current machine state into a manifest"
    Write-Host "    plan             Generate execution plan from manifest"
    Write-Host "    apply            Execute the plan (use -DryRun to preview)"
    Write-Host "    restore          Restore configuration files from manifest (requires -EnableRestore, use -Export for Model B)"
    Write-Host "    export-config    Export config files from system to export folder (inverse of restore, use -DryRun to preview)"
    Write-Host "    validate-export  Validate export integrity before restore (use -Export for Model B)"
    Write-Host "    revert           Revert last restore operation by restoring backups"
    Write-Host "    verify           Check current state against manifest"
    Write-Host "    doctor           Diagnose environment issues"
    Write-Host "    report           Show history of previous runs (use -Latest, -Last <n>, or -RunId <id>)"
    Write-Host "    diff             Compare two plan/run artifacts"
    Write-Host "    capabilities     Report CLI capabilities for GUI integration"
    Write-Host ""
    Write-Host "OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Manifest <path>       Path to manifest file (JSONC/JSON/YAML)"
    Write-Host "    -Profile <name>        Profile name for capture (writes to manifests/<name>.jsonc)"
    Write-Host "    -OutManifest <path>    Output path for captured manifest (overrides -Profile)"
    Write-Host "    -Export <path>         Export directory path (default: <manifestDir>/export/)"
    Write-Host "                           For restore/validate-export: resolve sources from export snapshot"
    Write-Host "    -DryRun                Preview changes without applying"
    Write-Host "    -EnableRestore         Enable restore operations (opt-in for safety)"
    Write-Host "    -FileA <path>          First artifact file for diff"
    Write-Host "    -FileB <path>          Second artifact file for diff"
    Write-Host "    -Json                  Output as JSON (capabilities, report, diff, apply, verify)"
    Write-Host ""
    Write-Host "REPORT OPTIONS:" -ForegroundColor Yellow
    Write-Host "    -Latest                Show most recent run (default)"
    Write-Host "    -Last <n>              Show N most recent runs (compact list)"
    Write-Host "    -RunId <id>            Show specific run by ID"
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
    Write-Host "    .\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore
    .\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -Export .\snapshot -EnableRestore"
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
        [Nullable[bool]]$DiscoverWriteManualIncludeValue,
        [bool]$IsUpdate,
        [bool]$IsPruneMissingApps,
        [bool]$IsWithConfig,
        [string[]]$ConfigModulesList,
        [string]$PayloadOutPath,
        [string]$EventsFormat = ""
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
    if ($IsUpdate) { $captureParams.Update = $true }
    if ($IsPruneMissingApps) { $captureParams.PruneMissingApps = $true }
    if ($IsWithConfig) { $captureParams.WithConfig = $true }
    if ($ConfigModulesList -and $ConfigModulesList.Count -gt 0) { $captureParams.ConfigModules = $ConfigModulesList }
    if ($PayloadOutPath) { $captureParams.PayloadOut = $PayloadOutPath }
    if ($EventsFormat) { $captureParams.EventsFormat = $EventsFormat }
    
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
        [string]$PlanPath,
        [bool]$IsDryRun,
        [bool]$IsEnableRestore = $false,
        [bool]$OutputJson = $false,
        [string]$EventsFormat = ""
    )
    
    # Validate: -Manifest and -Plan are mutually exclusive
    if ($ManifestPath -and $PlanPath) {
        Write-Host "[ERROR] -Manifest and -Plan are mutually exclusive. Use one or the other." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command apply -Manifest <path> [-DryRun] [-EnableRestore]" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command apply -Plan <path> [-DryRun] [-EnableRestore]" -ForegroundColor Yellow
        return $null
    }
    
    # Validate: need either -Manifest or -Plan
    if (-not $ManifestPath -and -not $PlanPath) {
        Write-Host "[ERROR] Either -Manifest or -Plan is required for 'apply' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command apply -Manifest <path> [-DryRun] [-EnableRestore]" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command apply -Plan <path> [-DryRun] [-EnableRestore]" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\apply.ps1"
    
    if ($PlanPath) {
        # Apply from pre-generated plan
        $result = Invoke-ApplyFromPlan -PlanPath $PlanPath -DryRun:$IsDryRun -EnableRestore:$IsEnableRestore -OutputJson:$OutputJson -EventsFormat $EventsFormat
    } else {
        # Normal apply: generate plan then execute
        $result = Invoke-Apply -ManifestPath $ManifestPath -DryRun:$IsDryRun -EnableRestore:$IsEnableRestore -OutputJson:$OutputJson -EventsFormat $EventsFormat
    }
    
    return $result
}

function Invoke-ProvisioningVerify {
    param(
        [string]$ManifestPath,
        [bool]$OutputJson = $false,
        [string]$EventsFormat = ""
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'verify' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command verify -Manifest <path>" -ForegroundColor Yellow
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\verify.ps1"
    
    $result = Invoke-Verify -ManifestPath $ManifestPath -OutputJson:$OutputJson -EventsFormat $EventsFormat
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
    param(
        [string]$ReportRunId,
        [bool]$IsLatest,
        [int]$LastN,
        [bool]$OutputJson
    )
    
    # Validate mutual exclusion: -RunId vs (-Latest or -Last)
    if ($ReportRunId -and ($IsLatest -or $LastN -gt 0)) {
        Write-Host "[ERROR] -RunId is mutually exclusive with -Latest and -Last." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command report                    # Show latest run (default)"
        Write-Host "  .\cli.ps1 -Command report -Latest            # Show latest run"
        Write-Host "  .\cli.ps1 -Command report -RunId <id>        # Show specific run"
        Write-Host "  .\cli.ps1 -Command report -Last <n>          # Show N most recent runs"
        Write-Host "  .\cli.ps1 -Command report -Json              # Output as JSON"
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\report.ps1"
    
    $stateDir = "$script:ProvisioningRoot\state"
    
    # Get reports based on selection
    $states = @()
    if ($ReportRunId) {
        $states = Get-ProvisioningReport -StateDir $stateDir -RunId $ReportRunId
    } elseif ($LastN -gt 0) {
        $states = Get-ProvisioningReport -StateDir $stateDir -Last $LastN
    } else {
        # Default: Latest
        $states = Get-ProvisioningReport -StateDir $stateDir -Latest
    }
    
    if ($states.Count -eq 0) {
        if ($ReportRunId) {
            Write-Host "[ERROR] Run not found: $ReportRunId" -ForegroundColor Red
        } else {
            Write-Host "No run history found." -ForegroundColor Yellow
            Write-Host "Run a command first to generate history." -ForegroundColor DarkGray
        }
        return $null
    }
    
    # Output
    if ($OutputJson) {
        $json = Format-ReportJson -States $states
        Write-Host $json
    } else {
        $useCompact = ($LastN -gt 0)
        Write-ReportHuman -States $states -Compact:$useCompact
    }
    
    return $states
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
        [bool]$IsDryRun,
        [string]$ExportPath
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'restore' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command restore -Manifest <path> -EnableRestore [-Export <path>] [-DryRun]" -ForegroundColor Yellow
        return $null
    }
    
    if (-not (Test-Path $ManifestPath)) {
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\restore.ps1"
    
    $result = Invoke-Restore -ManifestPath $ManifestPath -EnableRestore:$IsEnableRestore -DryRun:$IsDryRun -ExportPath $ExportPath
    return $result
}

function Invoke-ProvisioningExportConfig {
    <#
    .SYNOPSIS
        Export configuration files from system to export folder.
    #>
    param(
        [string]$ManifestPath,
        [string]$ExportPath,
        [bool]$IsDryRun,
        [string]$EventsFormat = ""
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'export-config' command." -ForegroundColor Red
        Write-Host "" 
        Write-Host "Usage: .\cli.ps1 -Command export-config -Manifest <path> [-Export <path>] [-DryRun]" -ForegroundColor Yellow
        return $null
    }
    
    if (-not (Test-Path $ManifestPath)) {
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\export-capture.ps1"
    
    $result = Invoke-ExportCapture -ManifestPath $ManifestPath -ExportPath $ExportPath -DryRun:$IsDryRun -EventsFormat $EventsFormat
    return $result
}

function Invoke-ProvisioningValidateExport {
    <#
    .SYNOPSIS
        Validate export integrity before restore.
    #>
    param(
        [string]$ManifestPath,
        [string]$ExportPath,
        [string]$EventsFormat = ""
    )
    
    if (-not $ManifestPath) {
        Write-Host "[ERROR] -Manifest is required for 'validate-export' command." -ForegroundColor Red
        Write-Host ""
        Write-Host "Usage: .\cli.ps1 -Command validate-export -Manifest <path> [-Export <path>]" -ForegroundColor Yellow
        return $null
    }
    
    if (-not (Test-Path $ManifestPath)) {
        Write-Host "[ERROR] Manifest not found: $ManifestPath" -ForegroundColor Red
        return $null
    }
    
    . "$script:ProvisioningRoot\engine\export-validate.ps1"
    
    $result = Invoke-ExportValidate -ManifestPath $ManifestPath -ExportPath $ExportPath -EventsFormat $EventsFormat
    return $result
}

function Invoke-ProvisioningRevert {
    <#
    .SYNOPSIS
        Revert the last restore operation by restoring backups.
    #>
    param(
        [bool]$IsDryRun,
        [string]$EventsFormat = ""
    )
    
    . "$script:ProvisioningRoot\engine\export-revert.ps1"
    
    $result = Invoke-ExportRevert -DryRun:$IsDryRun -EventsFormat $EventsFormat
    return $result
}

function Invoke-ProvisioningCapabilities {
    <#
    .SYNOPSIS
        Returns CLI capabilities for GUI handshake/compatibility checking.
    #>
    param(
        [bool]$OutputJson = $true
    )
    
    . "$script:ProvisioningRoot\engine\json-output.ps1"
    
    $capabilitiesData = Get-CapabilitiesData
    
    if ($OutputJson) {
        $envelope = New-JsonEnvelope -Command "capabilities" -Success $true -Data $capabilitiesData
        Write-JsonOutput -Envelope $envelope
    } else {
        # Human-readable output
        Write-Host ""
        Write-Host "Endstate CLI Capabilities" -ForegroundColor Cyan
        Write-Host "==========================" -ForegroundColor Cyan
        Write-Host ""
        Write-Host "CLI Version: $(Get-EndstateVersion)" -ForegroundColor White
        Write-Host "Schema Version: $(Get-SchemaVersion)" -ForegroundColor White
        Write-Host ""
        Write-Host "Supported Commands:" -ForegroundColor Yellow
        foreach ($cmd in $capabilitiesData.commands.Keys) {
            $cmdInfo = $capabilitiesData.commands[$cmd]
            if ($cmdInfo.supported) {
                Write-Host "  - $cmd" -ForegroundColor Green
            }
        }
        Write-Host ""
        Write-Host "Platform: $($capabilitiesData.platform.os)" -ForegroundColor White
        Write-Host "Drivers: $($capabilitiesData.platform.drivers -join ', ')" -ForegroundColor White
        Write-Host ""
    }
    
    return $capabilitiesData
}

# Main execution
if ($Version.IsPresent) {
    $ver = Get-ProvisioningVersion
    Write-Host $ver
    exit 0
}

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
            -DiscoverWriteManualIncludeValue $DiscoverWriteManualInclude `
            -IsUpdate $Update.IsPresent `
            -IsPruneMissingApps $PruneMissingApps.IsPresent `
            -IsWithConfig $WithConfig.IsPresent `
            -ConfigModulesList $ConfigModules `
            -PayloadOutPath $PayloadOut `
            -EventsFormat $Events
    }
    "plan"    { Invoke-ProvisioningPlan -ManifestPath $Manifest }
    "apply"   { Invoke-ProvisioningApply -ManifestPath $Manifest -PlanPath $Plan -IsDryRun $DryRun.IsPresent -IsEnableRestore $EnableRestore.IsPresent -OutputJson $Json.IsPresent -EventsFormat $Events }
    "restore" { Invoke-ProvisioningRestore -ManifestPath $Manifest -IsEnableRestore $EnableRestore.IsPresent -IsDryRun $DryRun.IsPresent -ExportPath $Export }
    "export-config" { Invoke-ProvisioningExportConfig -ManifestPath $Manifest -ExportPath $Export -IsDryRun $DryRun.IsPresent -EventsFormat $Events }
    "validate-export" { Invoke-ProvisioningValidateExport -ManifestPath $Manifest -ExportPath $Export -EventsFormat $Events }
    "revert"  { Invoke-ProvisioningRevert -IsDryRun $DryRun.IsPresent -EventsFormat $Events }
    "verify"  { Invoke-ProvisioningVerify -ManifestPath $Manifest -OutputJson $Json.IsPresent -EventsFormat $Events }
    "doctor"  { Invoke-ProvisioningDoctor }
    "report"  { Invoke-ProvisioningReport -ReportRunId $RunId -IsLatest $Latest.IsPresent -LastN $Last -OutputJson $Json.IsPresent }
    "diff"    { Invoke-ProvisioningDiff -FileAPath $FileA -FileBPath $FileB -OutputJson $Json.IsPresent }
    "capabilities" { Invoke-ProvisioningCapabilities -OutputJson $Json.IsPresent }
    default   { Show-Help }
}
