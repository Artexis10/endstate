<#
.SYNOPSIS
    Provisioning diff engine - compares two plan/run artifacts.

.DESCRIPTION
    Compares two JSON plan or run artifacts and outputs meaningful differences.
    Supports comparison by file path or run ID.
#>

function Compare-ProvisioningArtifacts {
    <#
    .SYNOPSIS
        Compare two provisioning plan/run artifacts.
    .DESCRIPTION
        Pure function that compares two parsed artifacts and returns structured diff.
        Does not perform I/O - suitable for unit testing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$ArtifactA,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$ArtifactB
    )
    
    # Use ArrayList for mutable collections
    $actionsAdded = [System.Collections.ArrayList]::new()
    $actionsRemoved = [System.Collections.ArrayList]::new()
    $actionsChanged = [System.Collections.ArrayList]::new()
    
    $diff = @{
        summaryA = @{
            install = 0
            skip = 0
            restore = 0
            verify = 0
        }
        summaryB = @{
            install = 0
            skip = 0
            restore = 0
            verify = 0
        }
        actionsAdded = $actionsAdded
        actionsRemoved = $actionsRemoved
        actionsChanged = $actionsChanged
        identical = $true
    }
    
    # Extract summaries
    if ($ArtifactA.summary) {
        $diff.summaryA.install = [int]($ArtifactA.summary.install)
        $diff.summaryA.skip = [int]($ArtifactA.summary.skip)
        $diff.summaryA.restore = [int]($ArtifactA.summary.restore)
        $diff.summaryA.verify = [int]($ArtifactA.summary.verify)
    }
    
    if ($ArtifactB.summary) {
        $diff.summaryB.install = [int]($ArtifactB.summary.install)
        $diff.summaryB.skip = [int]($ArtifactB.summary.skip)
        $diff.summaryB.restore = [int]($ArtifactB.summary.restore)
        $diff.summaryB.verify = [int]($ArtifactB.summary.verify)
    }
    
    # Build action lookup by unique key (type + id/ref/path)
    $actionsA = @{}
    $actionsB = @{}
    
    foreach ($action in $ArtifactA.actions) {
        $key = Get-ActionKey -Action $action
        $actionsA[$key] = $action
    }
    
    foreach ($action in $ArtifactB.actions) {
        $key = Get-ActionKey -Action $action
        $actionsB[$key] = $action
    }
    
    # Find removed actions (in A but not in B)
    foreach ($key in $actionsA.Keys) {
        if (-not $actionsB.ContainsKey($key)) {
            [void]$diff.actionsRemoved.Add(@{
                key = $key
                action = $actionsA[$key]
            })
            $diff.identical = $false
        }
    }
    
    # Find added actions (in B but not in A)
    foreach ($key in $actionsB.Keys) {
        if (-not $actionsA.ContainsKey($key)) {
            [void]$diff.actionsAdded.Add(@{
                key = $key
                action = $actionsB[$key]
            })
            $diff.identical = $false
        }
    }
    
    # Find changed actions (in both but different status/reason)
    foreach ($key in $actionsA.Keys) {
        if ($actionsB.ContainsKey($key)) {
            $actionA = $actionsA[$key]
            $actionB = $actionsB[$key]
            
            $statusA = $actionA.status
            $statusB = $actionB.status
            $reasonA = $actionA.reason
            $reasonB = $actionB.reason
            
            if ($statusA -ne $statusB -or $reasonA -ne $reasonB) {
                [void]$diff.actionsChanged.Add(@{
                    key = $key
                    statusA = $statusA
                    statusB = $statusB
                    reasonA = $reasonA
                    reasonB = $reasonB
                })
                $diff.identical = $false
            }
        }
    }
    
    # Sort for deterministic output - wrap in @() to ensure array even for single items
    $diff.actionsAdded = @($diff.actionsAdded | Sort-Object { $_.key })
    $diff.actionsRemoved = @($diff.actionsRemoved | Sort-Object { $_.key })
    $diff.actionsChanged = @($diff.actionsChanged | Sort-Object { $_.key })
    
    return $diff
}

function Get-ActionKey {
    <#
    .SYNOPSIS
        Generate a unique key for an action for comparison purposes.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $Action
    )
    
    $type = $Action.type
    
    switch ($type) {
        "app" {
            return "app:$($Action.ref)"
        }
        "restore" {
            return "restore:$($Action.source)->$($Action.target)"
        }
        "verify" {
            $verifyType = $Action.verifyType
            $path = $Action.path
            $command = $Action.command
            if ($path) {
                return "verify:$verifyType`:$path"
            } elseif ($command) {
                return "verify:$verifyType`:$command"
            } else {
                return "verify:$verifyType"
            }
        }
        default {
            return "$type`:$($Action.id)"
        }
    }
}

function Read-ArtifactFile {
    <#
    .SYNOPSIS
        Read and parse a JSON artifact file.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    try {
        # Load artifact file (supports JSONC format)
        . "$PSScriptRoot\manifest.ps1"
        $artifact = Read-JsoncFile -Path $Path -Depth 100
        return $artifact
    } catch {
        return $null
    }
}

function Resolve-RunIdToPath {
    <#
    .SYNOPSIS
        Resolve a run ID to its artifact file path.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$RunId,
        
        [Parameter(Mandatory = $false)]
        [string]$BaseDir
    )
    
    if (-not $BaseDir) {
        $BaseDir = Join-Path $PSScriptRoot ".."
    }
    
    # Check plans directory first
    $plansDir = Join-Path $BaseDir "plans"
    $planFile = Join-Path $plansDir "$RunId.json"
    if (Test-Path $planFile) {
        return $planFile
    }
    
    # Check state directory
    $stateDir = Join-Path $BaseDir "state"
    $stateFile = Join-Path $stateDir "$RunId.json"
    if (Test-Path $stateFile) {
        return $stateFile
    }
    
    return $null
}

function Format-DiffOutput {
    <#
    .SYNOPSIS
        Format diff result for human-readable console output.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Diff,
        
        [Parameter(Mandatory = $false)]
        [string]$LabelA = "A",
        
        [Parameter(Mandatory = $false)]
        [string]$LabelB = "B"
    )
    
    $output = @()
    
    $output += ""
    $output += "Provisioning Diff"
    $output += "================="
    $output += ""
    
    # Summary comparison
    $output += "Summary Comparison:"
    $output += "  Install: $($Diff.summaryA.install) -> $($Diff.summaryB.install)"
    $output += "  Skip:    $($Diff.summaryA.skip) -> $($Diff.summaryB.skip)"
    $output += "  Restore: $($Diff.summaryA.restore) -> $($Diff.summaryB.restore)"
    $output += "  Verify:  $($Diff.summaryA.verify) -> $($Diff.summaryB.verify)"
    $output += ""
    
    if ($Diff.identical) {
        $output += "No differences found - artifacts are identical."
    } else {
        # Actions removed
        if ($Diff.actionsRemoved.Count -gt 0) {
            $output += "Actions Removed (in $LabelA but not $LabelB):"
            foreach ($item in $Diff.actionsRemoved) {
                $output += "  - $($item.key) [$($item.action.status)]"
            }
            $output += ""
        }
        
        # Actions added
        if ($Diff.actionsAdded.Count -gt 0) {
            $output += "Actions Added (in $LabelB but not $LabelA):"
            foreach ($item in $Diff.actionsAdded) {
                $output += "  + $($item.key) [$($item.action.status)]"
            }
            $output += ""
        }
        
        # Actions changed
        if ($Diff.actionsChanged.Count -gt 0) {
            $output += "Actions Changed:"
            foreach ($item in $Diff.actionsChanged) {
                $statusChange = "$($item.statusA) -> $($item.statusB)"
                $output += "  ~ $($item.key): $statusChange"
                if ($item.reasonA -ne $item.reasonB) {
                    $output += "    reason: '$($item.reasonA)' -> '$($item.reasonB)'"
                }
            }
            $output += ""
        }
    }
    
    return $output -join "`n"
}

function ConvertTo-DiffJson {
    <#
    .SYNOPSIS
        Convert diff result to stable JSON format.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Diff
    )
    
    $ordered = [ordered]@{
        identical = $Diff.identical
        summaryA = [ordered]@{
            install = $Diff.summaryA.install
            skip = $Diff.summaryA.skip
            restore = $Diff.summaryA.restore
            verify = $Diff.summaryA.verify
        }
        summaryB = [ordered]@{
            install = $Diff.summaryB.install
            skip = $Diff.summaryB.skip
            restore = $Diff.summaryB.restore
            verify = $Diff.summaryB.verify
        }
        actionsRemoved = @($Diff.actionsRemoved | ForEach-Object { $_.key })
        actionsAdded = @($Diff.actionsAdded | ForEach-Object { $_.key })
        actionsChanged = @($Diff.actionsChanged | ForEach-Object {
            [ordered]@{
                key = $_.key
                statusA = $_.statusA
                statusB = $_.statusB
            }
        })
    }
    
    return $ordered | ConvertTo-Json -Depth 10
}

# Functions exported: Compare-ProvisioningArtifacts, Get-ActionKey, Read-ArtifactFile, Resolve-RunIdToPath, Format-DiffOutput, ConvertTo-DiffJson
