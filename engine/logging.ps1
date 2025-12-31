# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning logging utilities.

.DESCRIPTION
    Human-readable logging with file output support.
#>

$script:LogFile = $null
$script:LogLevel = "INFO"

function Initialize-ProvisioningLog {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RunId
    )
    
    $logsDir = Join-Path $PSScriptRoot "..\logs"
    if (-not (Test-Path $logsDir)) {
        New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
    }
    
    $script:LogFile = Join-Path $logsDir "$RunId.log"
    
    $header = @"
================================================================================
Provisioning Run: $RunId
Started: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")
Machine: $env:COMPUTERNAME
User: $env:USERNAME
================================================================================

"@
    $header | Out-File -FilePath $script:LogFile -Encoding UTF8
    
    return $script:LogFile
}

function Write-ProvisioningLog {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Message,
        
        [Parameter(Mandatory = $false)]
        [ValidateSet("INFO", "WARN", "ERROR", "SUCCESS", "SKIP", "ACTION", "DEBUG")]
        [string]$Level = "INFO",
        
        [Parameter(Mandatory = $false)]
        [switch]$NoConsole
    )
    
    $timestamp = Get-Date -Format "HH:mm:ss"
    $logLine = "[$timestamp] [$Level] $Message"
    
    # Console output with colors
    if (-not $NoConsole) {
        $color = switch ($Level) {
            "INFO"    { "White" }
            "WARN"    { "Yellow" }
            "ERROR"   { "Red" }
            "SUCCESS" { "Green" }
            "SKIP"    { "DarkGray" }
            "ACTION"  { "Cyan" }
            "DEBUG"   { "DarkGray" }
            default   { "White" }
        }
        
        $prefix = switch ($Level) {
            "INFO"    { "[INFO]   " }
            "WARN"    { "[WARN]   " }
            "ERROR"   { "[ERROR]  " }
            "SUCCESS" { "[OK]     " }
            "SKIP"    { "[SKIP]   " }
            "ACTION"  { "[ACTION] " }
            "DEBUG"   { "[DEBUG]  " }
            default   { "         " }
        }
        
        Write-Host "$prefix$Message" -ForegroundColor $color
    }
    
    # File output
    if ($script:LogFile) {
        $logLine | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
    }
}

function Write-ProvisioningSection {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Title
    )
    
    $separator = "=" * 60
    Write-Host ""
    Write-Host $separator -ForegroundColor DarkCyan
    Write-Host " $Title" -ForegroundColor Cyan
    Write-Host $separator -ForegroundColor DarkCyan
    
    if ($script:LogFile) {
        "" | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
        $separator | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
        " $Title" | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
        $separator | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
    }
}

function Close-ProvisioningLog {
    param(
        [Parameter(Mandatory = $false)]
        [int]$SuccessCount = 0,
        
        [Parameter(Mandatory = $false)]
        [int]$SkipCount = 0,
        
        [Parameter(Mandatory = $false)]
        [int]$FailCount = 0
    )
    
    if ($script:LogFile) {
        $footer = @"

================================================================================
Completed: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")
Summary: $SuccessCount succeeded, $SkipCount skipped, $FailCount failed
================================================================================
"@
        $footer | Out-File -FilePath $script:LogFile -Append -Encoding UTF8
    }
    
    Write-Host ""
    Write-Host "Summary: " -NoNewline
    Write-Host "$SuccessCount succeeded" -ForegroundColor Green -NoNewline
    Write-Host ", " -NoNewline
    Write-Host "$SkipCount skipped" -ForegroundColor DarkGray -NoNewline
    Write-Host ", " -NoNewline
    if ($FailCount -gt 0) {
        Write-Host "$FailCount failed" -ForegroundColor Red
    } else {
        Write-Host "$FailCount failed" -ForegroundColor Green
    }
}

function Get-RunId {
    $timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $machine = $env:COMPUTERNAME
    if ($machine) {
        $machine = $machine.ToUpper() -replace '[^A-Z0-9_-]', '-' -replace ' ', '-'
    } else {
        $machine = "UNKNOWN"
    }
    return "$timestamp-$machine"
}

# Functions exported: Initialize-ProvisioningLog, Write-ProvisioningLog, Write-ProvisioningSection, Close-ProvisioningLog, Get-RunId
