# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    JSON output module - provides standardized JSON envelope for CLI output.

.DESCRIPTION
    All --json outputs use this module to ensure consistent envelope format
    as defined in the CLI JSON Contract v1.0.
#>

$script:JsonOutputRoot = $PSScriptRoot | Split-Path -Parent
$script:SchemaVersion = "1.0"

function Get-EndstateVersion {
    <#
    .SYNOPSIS
        Returns the current Endstate CLI version.
    #>
    $versionFile = Join-Path $script:JsonOutputRoot "VERSION.txt"
    
    if (Test-Path $versionFile) {
        return (Get-Content -Path $versionFile -Raw).Trim()
    }
    
    try {
        $gitSha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $gitSha) {
            return "0.0.0-dev+$gitSha"
        }
    } catch { }
    
    return "0.0.0-dev"
}

function Get-SchemaVersion {
    <#
    .SYNOPSIS
        Returns the current JSON schema version.
    #>
    return $script:SchemaVersion
}

function Get-RunId {
    <#
    .SYNOPSIS
        Generates a unique run ID in format yyyyMMdd-HHmmss.
    #>
    return Get-Date -Format "yyyyMMdd-HHmmss"
}

function New-JsonEnvelope {
    <#
    .SYNOPSIS
        Creates a standardized JSON envelope for CLI output.
    .PARAMETER Command
        The command that produced this output.
    .PARAMETER RunId
        Unique run identifier. If not provided, generates one.
    .PARAMETER Success
        Whether the command succeeded.
    .PARAMETER Data
        Command-specific result data.
    .PARAMETER Error
        Error object if success is false.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Command,
        
        [Parameter(Mandatory = $false)]
        [string]$RunId,
        
        [Parameter(Mandatory = $true)]
        [bool]$Success,
        
        [Parameter(Mandatory = $false)]
        [object]$Data = @{},
        
        [Parameter(Mandatory = $false)]
        [object]$Error = $null
    )
    
    if (-not $RunId) {
        $RunId = Get-RunId
    }
    
    $envelope = [ordered]@{
        schemaVersion = Get-SchemaVersion
        cliVersion = Get-EndstateVersion
        command = $Command
        runId = $RunId
        timestampUtc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        success = $Success
        data = $Data
        error = $Error
    }
    
    return $envelope
}

function New-JsonError {
    <#
    .SYNOPSIS
        Creates a standardized error object.
    .PARAMETER Code
        Stable, machine-readable error code (SCREAMING_SNAKE_CASE).
    .PARAMETER Message
        Human-readable error description.
    .PARAMETER Detail
        Optional structured context.
    .PARAMETER Remediation
        Optional suggested action to resolve the error.
    .PARAMETER DocsKey
        Optional documentation reference key.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Code,
        
        [Parameter(Mandatory = $true)]
        [string]$Message,
        
        [Parameter(Mandatory = $false)]
        [object]$Detail = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$Remediation = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$DocsKey = $null
    )
    
    $error = [ordered]@{
        code = $Code
        message = $Message
    }
    
    if ($null -ne $Detail) {
        $error.detail = $Detail
    }
    
    if ($Remediation) {
        $error.remediation = $Remediation
    }
    
    if ($DocsKey) {
        $error.docsKey = $DocsKey
    }
    
    return $error
}

function ConvertTo-JsonOutput {
    <#
    .SYNOPSIS
        Converts an envelope to JSON string output.
    .PARAMETER Envelope
        The envelope object to convert.
    .PARAMETER Depth
        JSON serialization depth. Default: 20.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [object]$Envelope,
        
        [Parameter(Mandatory = $false)]
        [int]$Depth = 20
    )
    
    return $Envelope | ConvertTo-Json -Depth $Depth
}

function Write-JsonOutput {
    <#
    .SYNOPSIS
        Writes JSON envelope to stdout.
    .PARAMETER Envelope
        The envelope object to output.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [object]$Envelope
    )
    
    $json = ConvertTo-JsonOutput -Envelope $Envelope
    Write-Output $json
}

function Get-CapabilitiesData {
    <#
    .SYNOPSIS
        Returns the capabilities data object for the capabilities command.
    #>
    
    Write-Host "Endstate CLI Capabilities" -ForegroundColor Cyan
    Write-Host "==========================" -ForegroundColor Cyan
    
    $capabilities = [ordered]@{
        supportedSchemaVersions = [ordered]@{
            min = "1.0"
            max = "1.0"
        }
        commands = [ordered]@{
            capture = [ordered]@{
                supported = $true
                flags = @("--profile", "--out-manifest", "--include-runtimes", "--include-store-apps", "--minimize", "--discover", "--update", "--prune-missing-apps", "--with-config", "--config-modules", "--payload-out")
            }
            plan = [ordered]@{
                supported = $true
                flags = @("--manifest")
            }
            apply = [ordered]@{
                supported = $true
                flags = @("--manifest", "--plan", "--dry-run", "--enable-restore", "--json", "--events")
            }
            verify = [ordered]@{
                supported = $true
                flags = @("--manifest", "--json", "--events")
            }
            restore = [ordered]@{
                supported = $true
                flags = @("--manifest", "--enable-restore", "--dry-run")
            }
            report = [ordered]@{
                supported = $true
                flags = @("--run-id", "--latest", "--last", "--json")
            }
            doctor = [ordered]@{
                supported = $true
                flags = @("--json")
            }
            diff = [ordered]@{
                supported = $true
                flags = @("--file-a", "--file-b", "--json")
            }
            capabilities = [ordered]@{
                supported = $true
                flags = @("--json")
            }
        }
        features = [ordered]@{
            streaming = $true
            streamingFormat = "jsonl"
            parallelInstall = $true
            configModules = $true
            jsonOutput = $true
        }
        platform = [ordered]@{
            os = "windows"
            drivers = @("winget")
        }
    }
    
    return $capabilities
}

# Error code constants
$script:ErrorCodes = @{
    MANIFEST_NOT_FOUND = "MANIFEST_NOT_FOUND"
    MANIFEST_PARSE_ERROR = "MANIFEST_PARSE_ERROR"
    MANIFEST_VALIDATION_ERROR = "MANIFEST_VALIDATION_ERROR"
    PLAN_NOT_FOUND = "PLAN_NOT_FOUND"
    PLAN_PARSE_ERROR = "PLAN_PARSE_ERROR"
    WINGET_NOT_AVAILABLE = "WINGET_NOT_AVAILABLE"
    INSTALL_FAILED = "INSTALL_FAILED"
    RESTORE_FAILED = "RESTORE_FAILED"
    VERIFY_FAILED = "VERIFY_FAILED"
    PERMISSION_DENIED = "PERMISSION_DENIED"
    INTERNAL_ERROR = "INTERNAL_ERROR"
    SCHEMA_INCOMPATIBLE = "SCHEMA_INCOMPATIBLE"
    INVALID_ARGUMENT = "INVALID_ARGUMENT"
    RUN_NOT_FOUND = "RUN_NOT_FOUND"
}

function Get-ErrorCode {
    <#
    .SYNOPSIS
        Returns an error code constant.
    .PARAMETER Name
        The error code name.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )
    
    if ($script:ErrorCodes.ContainsKey($Name)) {
        return $script:ErrorCodes[$Name]
    }
    return "INTERNAL_ERROR"
}

# Functions exported: Get-EndstateVersion, Get-SchemaVersion, Get-RunId, New-JsonEnvelope, New-JsonError, ConvertTo-JsonOutput, Write-JsonOutput, Get-CapabilitiesData, Get-ErrorCode
