param(
  [Parameter(Mandatory)] [string] $ContextPath,
  [Parameter(Mandatory)] [string] $ReceiptPath,
  [Parameter(Mandatory)] [string] $Commit,
  [Parameter(Mandatory)] [string] $Sandbox,
  [Parameter(Mandatory)] [string] $DisposableVMId,
  [Parameter(Mandatory)] [string] $DisposableVMSentinel
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$scenarioNames = @('newestCleanRestore','olderGenerationRestore','corruptNewestManifest','missingOrCorruptNewestChunk','partialUpload','retryAfterPartialUpload','localProfilePreserved')

if ($DisposableVMSentinel -ne 'ENDSTATE_DISPOSABLE_RECOVERY_VM' -or $env:ENDSTATE_DISPOSABLE_RECOVERY_VM -ne $DisposableVMSentinel) {
  throw 'Refusing recovery drill outside an explicitly marked disposable VM.'
}
if ([string]::IsNullOrWhiteSpace($DisposableVMId)) { throw 'Disposable VM identity is required.' }

function Assert-Envelope([string] $Path, [bool] $ExpectedSuccess) {
  $envelope = Get-Content -Raw $Path | ConvertFrom-Json
  if ([bool]$envelope.success -ne $ExpectedSuccess) { throw "Unexpected envelope success in $Path" }
  return $envelope
}

function Write-RecoveryContext([hashtable] $Context) {
  $Context | ConvertTo-Json -Depth 5 | Set-Content -NoNewline $ContextPath
}

if (-not (Test-Path $ContextPath)) { throw "Missing persisted recovery context: $ContextPath" }
$context = Get-Content -Raw $ContextPath | ConvertFrom-Json
foreach ($field in 'email','backupId','olderVersionId','newestVersionId') { if ([string]::IsNullOrWhiteSpace([string]$context.$field)) { throw "Recovery context missing $field" } }

# Scenario evidence is written by the guarded drill commands as individual JSON
# records. Nothing defaults to PASS: each record must state PASS, FAIL, or
# NOT_EXECUTED and include its parsed envelope/assertion evidence. A skipped
# destructive scenario is a release block only when its own record names why.
$scenarios = @{}
foreach ($name in $scenarioNames) {
  $evidencePath = Join-Path (Split-Path $ContextPath) ("scenario-$name.json")
  if (-not (Test-Path $evidencePath)) { $scenarios[$name] = @{ result='FAIL'; reason='missing parsed assertion record' }; continue }
  $evidence = Get-Content -Raw $evidencePath | ConvertFrom-Json
  if ($evidence.scenario -ne $name -or $evidence.result -notin @('PASS','FAIL','NOT_EXECUTED') -or -not $evidence.asserted) { $scenarios[$name] = @{ result='FAIL'; reason='invalid parsed assertion record' }; continue }
  $reason = if ($null -ne $evidence.PSObject.Properties['reason']) { [string]$evidence.reason } else { '' }
  if ($evidence.result -eq 'NOT_EXECUTED' -and [string]::IsNullOrWhiteSpace($reason)) { throw "NOT_EXECUTED scenario '$name' requires an exact reason" }
  $scenarios[$name] = @{ result=$evidence.result; reason=$reason }
}
$scenarioResults = @($scenarios.Values | ForEach-Object { $_.result })
# This helper is deliberately an evidence assembler, not an automated release
# gate. The destructive object-store actions and clean-sandbox recovery remain
# manual. Do not let operator-authored files mint a PASS receipt: an auditor
# must validate the raw artifacts before signing a release record.
$status = if ($scenarioResults -contains 'FAIL') { 'FAIL' } else { 'BLOCKED' }
@{ schemaVersion='1.2'; status=$status; email=$context.email; backupId=$context.backupId; newestVersionId=$context.newestVersionId; olderVersionId=$context.olderVersionId; commit=$Commit; sandbox=$Sandbox; disposableVmId=$DisposableVMId; timestamp=(Get-Date -Format o); scenarios=$scenarios; manualReviewRequired=$true } | ConvertTo-Json -Depth 5 | Set-Content -NoNewline $ReceiptPath
