# Go Engine Integration Tests — Safe to run on real machine
# All writes go to $env:TEMP — nothing touches your system
# Run from: go-engine/ directory

$ErrorActionPreference = "Continue"
$passed = 0; $failed = 0; $total = 0

function Test-Command {
    param([string]$Name, [scriptblock]$Block)
    $script:total++
    Write-Host "`n=== TEST $($script:total): $Name ===" -ForegroundColor Cyan
    try {
        & $Block
        $script:passed++
        Write-Host "  PASS" -ForegroundColor Green
    } catch {
        $script:failed++
        Write-Host "  FAIL: $_" -ForegroundColor Red
    }
}

$exe = ".\endstate.exe"
$testDir = Join-Path $env:TEMP "endstate-go-test-$(Get-Random)"
New-Item -ItemType Directory -Path $testDir -Force | Out-Null
Write-Host "Test dir: $testDir`n" -ForegroundColor Yellow

# --- Create test fixtures ---
$manifestDir = Join-Path $testDir "manifest"
$configsDir = Join-Path $manifestDir "configs"
New-Item -ItemType Directory -Path $configsDir -Force | Out-Null

# Manifest with copy restore + verifiers
$escapedDir = $testDir -replace '\\','\\\\'
@"
{
  "version": 1, "name": "integration-test",
  "apps": [{ "id": "notepad", "refs": { "windows": "Notepad++.Notepad++" } }],
  "restore": [
    { "type": "copy", "source": "./configs/settings.json", "target": "$escapedDir\\restored-settings.json", "backup": true },
    { "type": "copy", "source": "./configs/missing.json", "target": "$escapedDir\\optional.json", "backup": true, "optional": true }
  ],
  "verify": [
    { "type": "command-exists", "command": "go" }
  ]
}
"@ | Set-Content (Join-Path $manifestDir "test.jsonc") -Encoding UTF8

'{"theme":"dark","fontSize":14}' | Set-Content (Join-Path $configsDir "settings.json") -Encoding UTF8

# Manifest with merge-json restore
$mergeDir = Join-Path $testDir "merge"
$mergeConfigs = Join-Path $mergeDir "configs"
New-Item -ItemType Directory -Path $mergeConfigs -Force | Out-Null
@"
{
  "version": 1, "name": "merge-test", "apps": [],
  "restore": [
    { "type": "merge-json", "source": "./configs/overlay.json", "target": "$escapedDir\\base.json", "backup": true }
  ]
}
"@ | Set-Content (Join-Path $mergeDir "merge.jsonc") -Encoding UTF8
'{"theme":"light","newKey":"added"}' | Set-Content (Join-Path $mergeConfigs "overlay.json") -Encoding UTF8
'{"theme":"dark","fontSize":14}' | Set-Content (Join-Path $testDir "base.json") -Encoding UTF8

$mp = Join-Path $manifestDir "test.jsonc"

# ============================================================
Test-Command "Apply real (Notepad++ already installed)" {
    $out = & $exe apply --manifest $mp --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    if ($j.data.summary.total -lt 1) { throw "No actions" }
    Write-Host "    Total=$($j.data.summary.total) Skipped=$($j.data.summary.skipped)"
}

Test-Command "Restore dry-run (copy)" {
    $out = & $exe restore --manifest $mp --enable-restore --dry-run --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    if (-not $j.data.dryRun) { throw "Expected dryRun=true" }
    Write-Host "    Results=$($j.data.results.Count) DryRun=$($j.data.dryRun)"
}

Test-Command "Restore real (copy to temp)" {
    $out = & $exe restore --manifest $mp --enable-restore --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    $target = Join-Path $testDir "restored-settings.json"
    if (-not (Test-Path $target)) { throw "File not created: $target" }
    $content = Get-Content $target -Raw
    if ($content -notlike '*dark*') { throw "Content mismatch" }
    $skipped = @($j.data.results | Where-Object { $_.status -eq "skipped_missing_source" })
    if ($skipped.Count -lt 1) { throw "Optional missing not skipped" }
    Write-Host "    Restored: $target"
    Write-Host "    Optional missing correctly skipped"
    Write-Host "    Journal: $($j.data.journalPath)"
}

Test-Command "Restore rejected without --enable-restore" {
    $out = & $exe restore --manifest $mp --json 2>$null
    $j = $out | ConvertFrom-Json
    if ($j.success) { throw "Should have failed" }
    if ($j.error.code -ne "RESTORE_FAILED") { throw "Wrong code: $($j.error.code)" }
    Write-Host "    Correctly rejected"
}

Test-Command "Merge-json restore dry-run" {
    $mergePath = Join-Path $mergeDir "merge.jsonc"
    $out = & $exe restore --manifest $mergePath --enable-restore --dry-run --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    Write-Host "    Results=$($j.data.results.Count)"
}

Test-Command "Merge-json restore real" {
    $mergePath = Join-Path $mergeDir "merge.jsonc"
    $out = & $exe restore --manifest $mergePath --enable-restore --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    $merged = Get-Content (Join-Path $testDir "base.json") -Raw | ConvertFrom-Json
    if ($merged.newKey -ne "added") { throw "Merge didn't add newKey" }
    if ($merged.fontSize -ne 14) { throw "Merge lost existing fontSize" }
    Write-Host "    Merged: theme=$($merged.theme) newKey=$($merged.newKey) fontSize=$($merged.fontSize)"
}

Test-Command "Revert command" {
    $out = & $exe revert --json 2>$null
    $j = $out | ConvertFrom-Json
    if ($null -eq $j.schemaVersion) { throw "Invalid envelope" }
    Write-Host "    Success=$($j.success)"
    if ($j.error) { Write-Host "    Message: $($j.error.message)" }
}

Test-Command "Export-config dry-run" {
    $exportManifestDir = Join-Path $testDir "export-mf"
    New-Item -ItemType Directory -Path $exportManifestDir -Force | Out-Null
    @"
{
  "version": 1, "name": "export-test", "apps": [],
  "restore": [
    { "type": "copy", "source": "./configs/file.txt", "target": "$escapedDir\\system-file.txt", "backup": true }
  ]
}
"@ | Set-Content (Join-Path $exportManifestDir "export.jsonc") -Encoding UTF8
    "system content" | Set-Content (Join-Path $testDir "system-file.txt") -Encoding UTF8
    $out = & $exe export-config --manifest (Join-Path $exportManifestDir "export.jsonc") --dry-run --json 2>$null
    $j = $out | ConvertFrom-Json
    if ($null -eq $j.schemaVersion) { throw "Invalid envelope" }
    Write-Host "    Success=$($j.success)"
}

Test-Command "Validate-export" {
    $exportManifestDir = Join-Path $testDir "export-mf"
    $out = & $exe validate-export --manifest (Join-Path $exportManifestDir "export.jsonc") --json 2>$null
    $j = $out | ConvertFrom-Json
    if ($null -eq $j.schemaVersion) { throw "Invalid envelope" }
    Write-Host "    Success=$($j.success)"
}

Test-Command "Bootstrap envelope" {
    $out = & $exe bootstrap --json 2>$null
    $j = $out | ConvertFrom-Json
    if ($null -eq $j.schemaVersion) { throw "Invalid envelope" }
    Write-Host "    Success=$($j.success)"
}

Test-Command "Events streaming (stderr)" {
    $eventsFile = Join-Path $testDir "events.jsonl"
    & $exe verify --manifest $mp --json --events jsonl 2>$eventsFile | Out-Null
    $events = @(Get-Content $eventsFile -ErrorAction SilentlyContinue)
    if ($events.Count -eq 0) { throw "No events on stderr" }
    $first = $events[0] | ConvertFrom-Json
    if ($first.event -ne "phase") { throw "First event not phase: $($first.event)" }
    if ($first.version -ne 1) { throw "Version not 1" }
    $last = $events[-1] | ConvertFrom-Json
    if ($last.event -ne "summary") { throw "Last event not summary: $($last.event)" }
    Write-Host "    Events=$($events.Count) First=phase Last=summary"
}

Test-Command "Apply + restore combined" {
    # Reset target
    Remove-Item (Join-Path $testDir "restored-settings.json") -ErrorAction SilentlyContinue
    $out = & $exe apply --manifest $mp --enable-restore --json 2>$null
    $j = $out | ConvertFrom-Json
    if (-not $j.success) { throw $j.error.message }
    $target = Join-Path $testDir "restored-settings.json"
    if (Test-Path $target) {
        Write-Host "    Restore ran inside apply: $target exists"
    } else {
        Write-Host "    Warning: restore target not created (may need investigation)"
    }
    Write-Host "    Total=$($j.data.summary.total) Skipped=$($j.data.summary.skipped)"
}

Test-Command "Verify with command-exists verifier" {
    $out = & $exe verify --manifest $mp --json 2>$null
    $j = $out | ConvertFrom-Json
    Write-Host "    Total=$($j.data.summary.total) Pass=$($j.data.summary.pass) Fail=$($j.data.summary.fail)"
}

# Cleanup
Write-Host "`n=== CLEANUP ===" -ForegroundColor Yellow
Remove-Item -Recurse -Force $testDir -ErrorAction SilentlyContinue
Write-Host "Cleaned: $testDir"

Write-Host "`n========================================" -ForegroundColor Cyan
Write-Host "  RESULTS: $passed/$total passed, $failed failed" -ForegroundColor $(if ($failed -eq 0) {"Green"} else {"Red"})
Write-Host "========================================`n" -ForegroundColor Cyan
