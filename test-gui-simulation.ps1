param()

Write-Host "`n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
Write-Host "OPTIONAL: GUI Simulation with PowerShell Parsing" -ForegroundColor Cyan
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "pwsh"
$psi.Arguments = "-NoProfile -File autosuite.ps1 report --json"
$psi.WorkingDirectory = $PSScriptRoot
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true

$process = New-Object System.Diagnostics.Process
$process.StartInfo = $psi

Write-Host "`nSimulating GUI process spawn..." -ForegroundColor Yellow

$process.Start() | Out-Null
$stdout = $process.StandardOutput.ReadToEnd()
$process.WaitForExit()

Write-Host "`n1. Captured STDOUT type check:" -ForegroundColor White
$stdoutType = $stdout.GetType().FullName
Write-Host "   Type: $stdoutType" -ForegroundColor Gray

if ($stdoutType -eq 'System.String') {
    Write-Host "   ✓ Type is System.String" -ForegroundColor Green
} else {
    Write-Host "   ✗ Type is NOT System.String" -ForegroundColor Red
    exit 1
}

Write-Host "`n2. ConvertFrom-Json test:" -ForegroundColor White
try {
    $parsed = $stdout | ConvertFrom-Json
    Write-Host "   ✓ ConvertFrom-Json succeeded" -ForegroundColor Green
} catch {
    Write-Host "   ✗ ConvertFrom-Json failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host "`n3. Parsed.success type check:" -ForegroundColor White
$successType = $parsed.success.GetType().FullName
Write-Host "   Type: $successType" -ForegroundColor Gray
Write-Host "   Value: $($parsed.success)" -ForegroundColor Gray

if ($parsed.success -is [bool]) {
    Write-Host "   ✓ `$parsed.success is boolean" -ForegroundColor Green
} else {
    Write-Host "   ✗ `$parsed.success is NOT boolean" -ForegroundColor Red
    exit 1
}

Write-Host "`n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
Write-Host "✓ GUI SIMULATION PASSED" -ForegroundColor Green
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Green
Write-Host ""
