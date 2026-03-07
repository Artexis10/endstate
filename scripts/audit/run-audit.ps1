<#
.SYNOPSIS
    Comprehensive module audit script for Endstate.
    Checks all module paths against actual filesystem, discovers uncaptured config,
    and cross-references against winget inventory.
#>

param(
    [string]$RepoRoot = (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
)

$ErrorActionPreference = 'Continue'

# --- Helpers ---
function Strip-JsoncComments {
    param([string]$Text)
    # Remove single-line // comments (but not inside strings)
    $lines = $Text -split "`n"
    $result = @()
    foreach ($line in $lines) {
        # Simple approach: remove // comments not inside quotes
        # This handles the common case of line-level comments
        $inString = $false
        $escaped = $false
        $commentStart = -1
        for ($i = 0; $i -lt $line.Length; $i++) {
            $c = $line[$i]
            if ($escaped) { $escaped = $false; continue }
            if ($c -eq '\') { $escaped = $true; continue }
            if ($c -eq '"') { $inString = !$inString; continue }
            if (!$inString -and $c -eq '/' -and ($i + 1) -lt $line.Length -and $line[$i + 1] -eq '/') {
                $commentStart = $i
                break
            }
        }
        if ($commentStart -ge 0) {
            $result += $line.Substring(0, $commentStart)
        } else {
            $result += $line
        }
    }
    # Also handle trailing commas before } or ]
    $json = $result -join "`n"
    $json = $json -replace ',(\s*[}\]])', '$1'
    return $json
}

function Expand-EnvPath {
    param([string]$Path)
    $expanded = $Path
    $expanded = $expanded -replace '%APPDATA%', $env:APPDATA
    $expanded = $expanded -replace '%LOCALAPPDATA%', $env:LOCALAPPDATA
    $expanded = $expanded -replace '%USERPROFILE%', $env:USERPROFILE
    $expanded = $expanded -replace '%PROGRAMDATA%', $env:ProgramData
    $expanded = $expanded -replace '%PROGRAMFILES%', $env:ProgramFiles
    $expanded = $expanded -replace '^~/', "$env:USERPROFILE/"
    $expanded = $expanded -replace '^~\\', "$env:USERPROFILE\"
    # Normalize path separators
    $expanded = $expanded -replace '/', '\'
    return $expanded
}

function Get-PathInfo {
    param([string]$RawPath)
    $expanded = Expand-EnvPath $RawPath
    $info = @{
        raw = $RawPath
        expanded = $expanded
        exists = $false
        type = $null
        sizeBytes = 0
    }
    if (Test-Path $expanded) {
        $info.exists = $true
        $item = Get-Item $expanded -Force
        if ($item.PSIsContainer) {
            $info.type = "directory"
            try {
                $info.sizeBytes = (Get-ChildItem $expanded -Recurse -Force -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum -ErrorAction SilentlyContinue).Sum
                if ($null -eq $info.sizeBytes) { $info.sizeBytes = 0 }
            } catch {
                $info.sizeBytes = 0
            }
        } else {
            $info.type = "file"
            $info.sizeBytes = $item.Length
        }
    }
    return $info
}

# --- Load winget inventory ---
$wingetFile = Join-Path $PSScriptRoot "winget-inventory.txt"
$wingetLines = @()
$wingetIds = @()
if (Test-Path $wingetFile) {
    $wingetLines = Get-Content $wingetFile
    foreach ($line in $wingetLines) {
        # Try to extract winget ID from the line (second column)
        if ($line -match '^\S.*?\s{2,}(\S+\.\S+)\s') {
            $wingetIds += $Matches[1]
        }
    }
}

# --- Enumerate modules ---
$modulesDir = Join-Path $RepoRoot "modules\apps"
$moduleIds = Get-ChildItem $modulesDir -Directory | Select-Object -ExpandProperty Name
$report = @{}

foreach ($moduleId in $moduleIds) {
    $moduleFile = Join-Path $modulesDir "$moduleId\module.jsonc"
    if (-not (Test-Path $moduleFile)) { continue }

    $raw = Get-Content $moduleFile -Raw
    $json = Strip-JsoncComments $raw
    try {
        $module = $json | ConvertFrom-Json
    } catch {
        Write-Warning "Failed to parse $moduleFile : $_"
        continue
    }

    $entry = @{
        installed = $false
        wingetMatch = $null
        verifyPaths = @{}
        capturePaths = @{}
        restorePaths = @{}
        missingPaths = @()
        suggestions = @()
    }

    # Check winget match
    if ($module.matches -and $module.matches.winget) {
        foreach ($wid in $module.matches.winget) {
            if ($wingetIds -contains $wid) {
                $entry.installed = $true
                $entry.wingetMatch = $wid
                break
            }
        }
    }

    # Check exe match if not found via winget
    if (-not $entry.installed -and $module.matches -and $module.matches.exe) {
        foreach ($exe in $module.matches.exe) {
            $found = Get-Command $exe -ErrorAction SilentlyContinue
            if ($found) {
                $entry.installed = $true
                break
            }
        }
    }

    # Check verify paths
    if ($module.verify) {
        foreach ($v in $module.verify) {
            if ($v.type -eq 'file-exists' -and $v.path) {
                $pathInfo = Get-PathInfo $v.path
                $entry.verifyPaths[$v.path] = $pathInfo
                if (-not $pathInfo.exists) {
                    $entry.missingPaths += $v.path
                }
            }
            if ($v.type -eq 'command-exists' -and $v.command) {
                $found = Get-Command $v.command -ErrorAction SilentlyContinue
                $entry.verifyPaths["cmd:$($v.command)"] = @{ exists = ($null -ne $found); type = "command" }
            }
        }
    }

    # Check capture paths
    if ($module.capture -and $module.capture.files) {
        foreach ($f in $module.capture.files) {
            if ($f.source) {
                $pathInfo = Get-PathInfo $f.source
                $entry.capturePaths[$f.source] = $pathInfo
                if (-not $pathInfo.exists) {
                    $entry.missingPaths += $f.source
                }
            }
        }
    }

    # Check restore targets
    if ($module.restore) {
        foreach ($r in $module.restore) {
            if ($r.target) {
                $pathInfo = Get-PathInfo $r.target
                $entry.restorePaths[$r.target] = $pathInfo
            }
        }
    }

    $report[$moduleId] = $entry
}

# --- Output JSON report ---
$output = @{
    generatedAt = (Get-Date -Format "o")
    modules = $report
}

$jsonOutput = $output | ConvertTo-Json -Depth 10
$jsonOutput | Set-Content (Join-Path $PSScriptRoot "module-audit-report.json") -Encoding UTF8

# --- Generate summary markdown ---
$sb = [System.Text.StringBuilder]::new()
[void]$sb.AppendLine("# Module Audit Summary")
[void]$sb.AppendLine("")
[void]$sb.AppendLine("Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
[void]$sb.AppendLine("")

$installedCount = ($report.Values | Where-Object { $_.installed }).Count
$totalCount = $report.Count
[void]$sb.AppendLine("## Overview")
[void]$sb.AppendLine("- Total modules: $totalCount")
[void]$sb.AppendLine("- Installed on this machine: $installedCount")
[void]$sb.AppendLine("- Not installed: $($totalCount - $installedCount)")
[void]$sb.AppendLine("")

[void]$sb.AppendLine("## Installed Modules")
[void]$sb.AppendLine("")
[void]$sb.AppendLine("| Module | Winget ID | Verify OK | Missing Paths |")
[void]$sb.AppendLine("|--------|-----------|-----------|---------------|")
foreach ($key in ($report.Keys | Sort-Object)) {
    $m = $report[$key]
    if ($m.installed) {
        $verifyOk = if ($m.verifyPaths.Count -eq 0) { "N/A" } else {
            $allOk = ($m.verifyPaths.Values | Where-Object { -not $_.exists }).Count -eq 0
            if ($allOk) { "Yes" } else { "PARTIAL" }
        }
        $missing = ($m.missingPaths | Select-Object -Unique).Count
        [void]$sb.AppendLine("| $key | $($m.wingetMatch) | $verifyOk | $missing |")
    }
}

[void]$sb.AppendLine("")
[void]$sb.AppendLine("## Not Installed Modules")
[void]$sb.AppendLine("")
foreach ($key in ($report.Keys | Sort-Object)) {
    $m = $report[$key]
    if (-not $m.installed) {
        [void]$sb.AppendLine("- $key")
    }
}

[void]$sb.AppendLine("")
[void]$sb.AppendLine("## Modules with Missing Paths")
[void]$sb.AppendLine("")
foreach ($key in ($report.Keys | Sort-Object)) {
    $m = $report[$key]
    $uniqueMissing = $m.missingPaths | Select-Object -Unique
    if ($uniqueMissing.Count -gt 0 -and $m.installed) {
        [void]$sb.AppendLine("### $key")
        foreach ($p in $uniqueMissing) {
            $expanded = Expand-EnvPath $p
            [void]$sb.AppendLine("- ``$p`` → ``$expanded``")
        }
        [void]$sb.AppendLine("")
    }
}

$sb.ToString() | Set-Content (Join-Path $PSScriptRoot "module-audit-summary.md") -Encoding UTF8

Write-Host "Audit complete. Reports saved to scripts/audit/"
Write-Host "  - module-audit-report.json"
Write-Host "  - module-audit-summary.md"
