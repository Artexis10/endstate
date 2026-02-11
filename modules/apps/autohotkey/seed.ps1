<#
.SYNOPSIS
    Seeds a representative AutoHotkey scripts directory for curation testing.

.DESCRIPTION
    Creates sample AutoHotkey scripts that represent real-world usage patterns
    WITHOUT any credentials or sensitive data. Used by the curation workflow
    to generate representative files for module validation.

    Note: AutoHotkey has no app-level config file. This module captures
    user scripts as a convenience backup.

    Configures:
    - Sample .ahk scripts in Documents\AutoHotkey\

    DOES NOT configure:
    - No app-level config exists for AutoHotkey

.EXAMPLE
    .\seed.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Write-Step {
    param([string]$Message)
    Write-Host "[SEED] $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " AutoHotkey Scripts Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$ahkDir = Join-Path $env:USERPROFILE "Documents\AutoHotkey"

# Ensure directory exists
if (-not (Test-Path $ahkDir)) {
    Write-Step "Creating AutoHotkey scripts directory..."
    New-Item -ItemType Directory -Path $ahkDir -Force | Out-Null
}

# ============================================================================
# SAMPLE STARTUP SCRIPT
# ============================================================================
Write-Step "Writing sample startup script..."

$startupPath = Join-Path $ahkDir "startup.ahk"

$startupScript = @"
; AutoHotkey v2 Startup Script - Endstate Seed
; This script demonstrates common hotkey patterns
#Requires AutoHotkey v2.0
#SingleInstance Force

; ============================================================================
; WINDOW MANAGEMENT
; ============================================================================

; Win+E: Open Explorer to user home
#e::Run "explorer.exe " A_MyDocuments

; Win+T: Open Windows Terminal
#t::Run "wt.exe"

; ============================================================================
; TEXT EXPANSION
; ============================================================================

; Quick email placeholder
::@@e::placeholder@example.com

; Quick date stamp
::@@d::
{
    SendInput FormatTime(, "yyyy-MM-dd")
}

; ============================================================================
; UTILITY HOTKEYS
; ============================================================================

; Ctrl+Shift+V: Paste as plain text
^+v::
{
    ClipSaved := A_Clipboard
    A_Clipboard := A_Clipboard  ; Strip formatting
    SendInput "^v"
    Sleep 50
    A_Clipboard := ClipSaved
}

; Win+Shift+S: Already mapped to Snipping Tool by Windows
; (included as documentation of expected system hotkey)
"@

$startupScript | Set-Content -Path $startupPath -Encoding UTF8

Write-Pass "Startup script written to: $startupPath"

# ============================================================================
# SAMPLE UTILITY LIBRARY
# ============================================================================
Write-Step "Writing sample utility library..."

$libPath = Join-Path $ahkDir "lib_utils.ahk"

$libScript = @"
; AutoHotkey v2 Utility Library - Endstate Seed
; Common utility functions
#Requires AutoHotkey v2.0

; Center a window on the current monitor
CenterWindow(WinTitle := "A") {
    WinGetPos(&X, &Y, &W, &H, WinTitle)
    MonitorGetWorkArea(, &MLeft, &MTop, &MRight, &MBottom)
    MW := MRight - MLeft
    MH := MBottom - MTop
    NewX := MLeft + (MW - W) // 2
    NewY := MTop + (MH - H) // 2
    WinMove(NewX, NewY, , , WinTitle)
}

; Toggle window always-on-top
ToggleAlwaysOnTop(WinTitle := "A") {
    ExStyle := WinGetExStyle(WinTitle)
    if (ExStyle & 0x8) {
        WinSetAlwaysOnTop(false, WinTitle)
    } else {
        WinSetAlwaysOnTop(true, WinTitle)
    }
}
"@

$libScript | Set-Content -Path $libPath -Encoding UTF8

Write-Pass "Utility library written to: $libPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($startupPath, $libPath)
foreach ($f in $seededFiles) {
    $exists = Test-Path $f
    $size = if ($exists) { (Get-Item $f).Length } else { 'N/A' }
    Write-Host "  $f exists=$exists size=$size" -ForegroundColor Gray
}

# ============================================================================
# SUMMARY
# ============================================================================
Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " AutoHotkey Scripts Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $startupPath" -ForegroundColor Gray
Write-Host "  - $libPath" -ForegroundColor Gray
Write-Host ""
Write-Step "Note: AutoHotkey has no app-level config. Scripts are user content."
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
