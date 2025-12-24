# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Live progress UI for endstate apply (PS 5.1 compatible).

.DESCRIPTION
    Provides app-based progress tracking for both sequential and parallel apply modes.
    - Thread-safe event queue for runspace communication
    - Progress state machine with running/completed tracking
    - Single-line progress bar renderer with throttling
    - No reliance on winget percentages or PS7-only APIs
#>

#region Event Queue (Thread-Safe)

function New-ProgressEventQueue {
    <#
    .SYNOPSIS
        Create a thread-safe event queue for progress tracking.
    .DESCRIPTION
        Uses ConcurrentQueue for PS 5.1 compatibility.
        Events: AppStarted, AppCompleted, AppOutput
    .OUTPUTS
        Hashtable with Queue property (ConcurrentQueue).
    #>
    
    $queue = [System.Collections.Concurrent.ConcurrentQueue[object]]::new()
    
    return @{
        Queue = $queue
    }
}

function Add-ProgressEvent {
    <#
    .SYNOPSIS
        Add an event to the progress queue.
    .PARAMETER EventQueue
        The event queue object.
    .PARAMETER EventType
        Type: AppStarted, AppCompleted, AppOutput
    .PARAMETER Data
        Event data hashtable.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$EventQueue,
        
        [Parameter(Mandatory = $true)]
        [ValidateSet('AppStarted', 'AppCompleted', 'AppOutput')]
        [string]$EventType,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$Data
    )
    
    $progressEvent = [PSCustomObject]@{
        Type = $EventType
        Timestamp = Get-Date
        Data = $Data
    }
    
    $EventQueue.Queue.Enqueue($progressEvent)
}

function Get-ProgressEvents {
    <#
    .SYNOPSIS
        Dequeue all available events from the queue.
    .PARAMETER EventQueue
        The event queue object.
    .OUTPUTS
        Array of events (may be empty).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$EventQueue
    )
    
    $events = @()
    $progressEvent = $null
    
    while ($EventQueue.Queue.TryDequeue([ref]$progressEvent)) {
        $events += $progressEvent
    }
    
    return $events
}

#endregion Event Queue

#region Progress State

function New-ProgressState {
    <#
    .SYNOPSIS
        Create a new progress state tracker.
    .PARAMETER TotalApps
        Total number of apps to process.
    .PARAMETER ParallelThrottle
        Max concurrent parallel jobs (0 for sequential mode).
    .OUTPUTS
        Hashtable with state properties.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [int]$TotalApps,
        
        [Parameter(Mandatory = $false)]
        [int]$ParallelThrottle = 0
    )
    
    return @{
        TotalApps = $TotalApps
        CompletedCount = 0
        FailedCount = 0
        ParallelThrottle = $ParallelThrottle
        RunningApps = @()
        QueuedCount = 0
        LastRenderTime = [DateTime]::MinValue
        RenderThrottleMs = 100
    }
}

function Update-ProgressState {
    <#
    .SYNOPSIS
        Update progress state based on an event.
    .PARAMETER State
        The progress state hashtable.
    .PARAMETER Event
        The event to process.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$Event
    )
    
    switch ($Event.Type) {
        'AppStarted' {
            $appId = $Event.Data.AppId
            if ($appId -and $appId -notin $State.RunningApps) {
                $State.RunningApps += $appId
            }
        }
        
        'AppCompleted' {
            $appId = $Event.Data.AppId
            $success = $Event.Data.Success
            
            if ($appId -and $appId -in $State.RunningApps) {
                $State.RunningApps = @($State.RunningApps | Where-Object { $_ -ne $appId })
            }
            
            $State.CompletedCount++
            
            if (-not $success) {
                $State.FailedCount++
            }
        }
    }
}

#endregion Progress State

#region Progress Renderer

function Get-ProgressBar {
    <#
    .SYNOPSIS
        Generate a progress bar string.
    .PARAMETER Completed
        Number of completed items.
    .PARAMETER Total
        Total number of items.
    .PARAMETER Width
        Width of the progress bar in characters (default: 30).
    .OUTPUTS
        String representation of progress bar.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [int]$Completed,
        
        [Parameter(Mandatory = $true)]
        [int]$Total,
        
        [Parameter(Mandatory = $false)]
        [int]$Width = 30
    )
    
    if ($Total -le 0) {
        return "[" + ("░" * $Width) + "]"
    }
    
    $percentage = [Math]::Min(1.0, $Completed / $Total)
    $filledWidth = [Math]::Floor($percentage * $Width)
    $emptyWidth = $Width - $filledWidth
    
    $filled = "█" * $filledWidth
    $empty = "░" * $emptyWidth
    
    return "[$filled$empty]"
}

function Format-ProgressLine {
    <#
    .SYNOPSIS
        Format a complete progress line.
    .PARAMETER State
        The progress state hashtable.
    .OUTPUTS
        Formatted progress string.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State
    )
    
    $bar = Get-ProgressBar -Completed $State.CompletedCount -Total $State.TotalApps -Width 30
    
    $statusText = "$($State.CompletedCount) / $($State.TotalApps) apps"
    
    if ($State.ParallelThrottle -gt 0) {
        $runningCount = $State.RunningApps.Count
        $statusText += " (parallel: $runningCount, queued: $($State.QueuedCount))"
    }
    
    return "$bar $statusText"
}

function Format-RunningApps {
    <#
    .SYNOPSIS
        Format the "Running (N):" section.
    .PARAMETER State
        The progress state hashtable.
    .OUTPUTS
        Formatted running apps string (may be empty).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State
    )
    
    $runningCount = $State.RunningApps.Count
    
    if ($runningCount -eq 0) {
        return ""
    }
    
    $appsList = $State.RunningApps -join ", "
    
    if ($appsList.Length -gt 80) {
        $appsList = $appsList.Substring(0, 77) + "..."
    }
    
    return "Running ($runningCount): $appsList"
}

function Show-Progress {
    <#
    .SYNOPSIS
        Render progress to console with throttling.
    .PARAMETER State
        The progress state hashtable.
    .PARAMETER Force
        Force render even if throttle period hasn't elapsed.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State,
        
        [Parameter(Mandatory = $false)]
        [switch]$Force
    )
    
    $now = Get-Date
    $elapsed = ($now - $State.LastRenderTime).TotalMilliseconds
    
    if (-not $Force -and $elapsed -lt $State.RenderThrottleMs) {
        return
    }
    
    $State.LastRenderTime = $now
    
    $progressLine = Format-ProgressLine -State $State
    $runningLine = Format-RunningApps -State $State
    
    # Clear current line and write progress
    Write-Host "`r$(' ' * 120)`r" -NoNewline
    Write-Host $progressLine -NoNewline -ForegroundColor Cyan
    
    if ($runningLine) {
        Write-Host ""
        Write-Host "  $runningLine" -ForegroundColor DarkGray
        Write-Host "`r" -NoNewline
    }
}

function Clear-Progress {
    <#
    .SYNOPSIS
        Clear the progress display.
    #>
    
    Write-Host "`r$(' ' * 120)`r" -NoNewline
}

#endregion Progress Renderer

#region Progress Orchestration

function Start-ProgressTracking {
    <#
    .SYNOPSIS
        Begin progress tracking (shows initial state).
    .PARAMETER State
        The progress state hashtable.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State
    )
    
    Show-Progress -State $State -Force
}

function Update-ProgressTracking {
    <#
    .SYNOPSIS
        Process events and update progress display.
    .PARAMETER State
        The progress state hashtable.
    .PARAMETER EventQueue
        The event queue object.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$EventQueue
    )
    
    $events = Get-ProgressEvents -EventQueue $EventQueue
    
    foreach ($progressEvent in $events) {
        Update-ProgressState -State $State -Event $progressEvent
    }
    
    if ($events.Count -gt 0) {
        Show-Progress -State $State
    }
}

function Complete-ProgressTracking {
    <#
    .SYNOPSIS
        Finalize progress tracking and clear display.
    .PARAMETER State
        The progress state hashtable.
    .PARAMETER EventQueue
        The event queue object (process remaining events).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$State,
        
        [Parameter(Mandatory = $false)]
        [hashtable]$EventQueue
    )
    
    if ($EventQueue) {
        $events = Get-ProgressEvents -EventQueue $EventQueue
        foreach ($progressEvent in $events) {
            Update-ProgressState -State $State -Event $progressEvent
        }
    }
    
    Show-Progress -State $State -Force
    Write-Host ""
    Clear-Progress
}

#endregion Progress Orchestration

# Functions exported:
# - New-ProgressEventQueue
# - Add-ProgressEvent
# - Get-ProgressEvents
# - New-ProgressState
# - Update-ProgressState
# - Get-ProgressBar
# - Format-ProgressLine
# - Format-RunningApps
# - Show-Progress
# - Clear-Progress
# - Invoke-WithProgress
