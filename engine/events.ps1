# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    NDJSON streaming events module for Endstate engine.

.DESCRIPTION
    Emits newline-delimited JSON events to stderr during execution.
    Events are UI-only and ephemeral - they do NOT replace the authoritative
    stdout JSON envelope.
    
    Event Schema v1:
    - version: 1 (always)
    - event: "phase" | "item" | "summary" | "artifact" | "error"
    - timestamp: RFC3339 UTC
    
    Phase Event:
    { "version": 1, "event": "phase", "phase": "plan" | "apply" | "verify" | "capture", "timestamp": "..." }
    
    Item Event:
    { "version": 1, "event": "item", "id": "App.Id", "driver": "winget", 
      "status": "to_install" | "installing" | "installed" | "present" | "skipped" | "failed",
      "reason": "already_installed" | "filtered" | "filtered_runtime" | "filtered_store" | "sensitive_excluded" | "detected" | "install_failed" | null,
      "message": "optional human message", "timestamp": "..." }
    
    Summary Event:
    { "version": 1, "event": "summary", "phase": "plan" | "apply" | "verify" | "capture",
      "total": N, "success": N, "skipped": N, "failed": N, "timestamp": "..." }
    
    Artifact Event:
    { "version": 1, "event": "artifact", "phase": "capture", "kind": "manifest",
      "path": "C:\\...jsonc", "timestamp": "..." }
    
    Error Event:
    { "version": 1, "event": "error", "scope": "item" | "engine", "message": "text", "timestamp": "..." }
#>

# Module state
$script:EventsEnabled = $false
$script:EventsVersion = 1

function Enable-StreamingEvents {
    <#
    .SYNOPSIS
        Enable NDJSON streaming events to stderr.
    #>
    $script:EventsEnabled = $true
}

function Disable-StreamingEvents {
    <#
    .SYNOPSIS
        Disable NDJSON streaming events.
    #>
    $script:EventsEnabled = $false
}

function Test-StreamingEventsEnabled {
    <#
    .SYNOPSIS
        Check if streaming events are enabled.
    #>
    return $script:EventsEnabled
}

function Get-Rfc3339Timestamp {
    <#
    .SYNOPSIS
        Returns current UTC timestamp in RFC3339 format.
    #>
    return (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ss.fffZ")
}

function Write-StreamingEvent {
    <#
    .SYNOPSIS
        Write a single NDJSON event to stderr.
    .PARAMETER Event
        Hashtable containing the event data.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Event
    )
    
    if (-not $script:EventsEnabled) {
        return
    }
    
    # Add version and timestamp if not present
    if (-not $Event.ContainsKey('version')) {
        $Event['version'] = $script:EventsVersion
    }
    if (-not $Event.ContainsKey('timestamp')) {
        $Event['timestamp'] = Get-Rfc3339Timestamp
    }
    
    # Convert to JSON (single line, no pretty print)
    $json = $Event | ConvertTo-Json -Compress -Depth 10
    
    # Write to stderr
    [Console]::Error.WriteLine($json)
}

function Write-PhaseEvent {
    <#
    .SYNOPSIS
        Emit a phase change event.
    .PARAMETER Phase
        The phase: "plan", "apply", "verify", or "capture"
    #>
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("plan", "apply", "verify", "capture")]
        [string]$Phase
    )
    
    Write-StreamingEvent @{
        event = "phase"
        phase = $Phase
    }
}

function Write-ItemEvent {
    <#
    .SYNOPSIS
        Emit an item progress event.
    .PARAMETER Id
        The item ID (e.g., "Notepad++.Notepad++")
    .PARAMETER Driver
        The driver (e.g., "winget")
    .PARAMETER Status
        The status: "to_install", "installing", "installed", "present", "skipped", "failed"
    .PARAMETER Reason
        Optional reason: "already_installed", "filtered", "install_failed", etc.
    .PARAMETER Message
        Optional human-readable message.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Id,
        
        [Parameter(Mandatory = $true)]
        [string]$Driver,
        
        [Parameter(Mandatory = $true)]
        [ValidateSet("to_install", "installing", "installed", "present", "skipped", "failed")]
        [string]$Status,
        
        [Parameter(Mandatory = $false)]
        [string]$Reason = $null,
        
        [Parameter(Mandatory = $false)]
        [string]$Message = $null
    )
    
    $event = @{
        event = "item"
        id = $Id
        driver = $Driver
        status = $Status
    }
    
    if ($Reason) {
        $event['reason'] = $Reason
    } else {
        $event['reason'] = $null
    }
    
    if ($Message) {
        $event['message'] = $Message
    }
    
    Write-StreamingEvent $event
}

function Write-SummaryEvent {
    <#
    .SYNOPSIS
        Emit a summary event at the end of a phase.
    .PARAMETER Phase
        The phase: "plan", "apply", "verify", or "capture"
    .PARAMETER Total
        Total items processed.
    .PARAMETER Success
        Number of successful items.
    .PARAMETER Skipped
        Number of skipped items.
    .PARAMETER Failed
        Number of failed items.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("plan", "apply", "verify", "capture")]
        [string]$Phase,
        
        [Parameter(Mandatory = $true)]
        [int]$Total,
        
        [Parameter(Mandatory = $true)]
        [int]$Success,
        
        [Parameter(Mandatory = $true)]
        [int]$Skipped,
        
        [Parameter(Mandatory = $true)]
        [int]$Failed
    )
    
    Write-StreamingEvent @{
        event = "summary"
        phase = $Phase
        total = $Total
        success = $Success
        skipped = $Skipped
        failed = $Failed
    }
}

function Write-ErrorEvent {
    <#
    .SYNOPSIS
        Emit an error event.
    .PARAMETER Scope
        The scope: "item" or "engine"
    .PARAMETER Message
        The error message.
    .PARAMETER ItemId
        Optional item ID if scope is "item".
    #>
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("item", "engine")]
        [string]$Scope,
        
        [Parameter(Mandatory = $true)]
        [string]$Message,
        
        [Parameter(Mandatory = $false)]
        [string]$ItemId = $null
    )
    
    $event = @{
        event = "error"
        scope = $Scope
        message = $Message
    }
    
    if ($ItemId) {
        $event['id'] = $ItemId
    }
    
    Write-StreamingEvent $event
}

function Write-ArtifactEvent {
    <#
    .SYNOPSIS
        Emit an artifact event (e.g., manifest saved).
    .PARAMETER Phase
        The phase: "capture"
    .PARAMETER Kind
        The artifact kind: "manifest"
    .PARAMETER Path
        The path to the artifact.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet("capture")]
        [string]$Phase,
        
        [Parameter(Mandatory = $true)]
        [ValidateSet("manifest")]
        [string]$Kind,
        
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    Write-StreamingEvent @{
        event = "artifact"
        phase = $Phase
        kind = $Kind
        path = $Path
    }
}

# Functions exported: Enable-StreamingEvents, Disable-StreamingEvents, Test-StreamingEventsEnabled,
#                     Write-PhaseEvent, Write-ItemEvent, Write-SummaryEvent, Write-ArtifactEvent, Write-ErrorEvent
