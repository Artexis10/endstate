$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'cloud-recovery-gate.ps1'
$root = Join-Path ([System.IO.Path]::GetTempPath()) ('endstate-cloud-recovery-gate-' + [guid]::NewGuid())
New-Item -ItemType Directory -Force $root | Out-Null
try {
  $contextPath = Join-Path $root 'recovery-context.json'
  @{ email='drill@example.test'; backupId='backup'; olderVersionId='older'; newestVersionId='newest' } | ConvertTo-Json | Set-Content -NoNewline $contextPath
  $names = @('newestCleanRestore','olderGenerationRestore','corruptNewestManifest','missingOrCorruptNewestChunk','partialUpload','retryAfterPartialUpload','localProfilePreserved')
  foreach ($name in $names) { @{ scenario=$name; result='PASS'; asserted=$true } | ConvertTo-Json | Set-Content -NoNewline (Join-Path $root "scenario-$name.json") }

  $receiptPath = Join-Path $root 'receipt.json'
  Remove-Item Env:ENDSTATE_DISPOSABLE_RECOVERY_VM -ErrorAction SilentlyContinue
  $guarded = $false
  try {
    & $scriptPath -ContextPath $contextPath -ReceiptPath $receiptPath -Commit '1234567' -Sandbox 'sandbox' -DisposableVMId 'sandbox-id' -DisposableVMSentinel 'ENDSTATE_DISPOSABLE_RECOVERY_VM'
  } catch { $guarded = $true }
  if (-not $guarded) { throw 'gate ran without the disposable VM guard' }

  $env:ENDSTATE_DISPOSABLE_RECOVERY_VM = 'ENDSTATE_DISPOSABLE_RECOVERY_VM'
  & $scriptPath -ContextPath $contextPath -ReceiptPath $receiptPath -Commit '1234567' -Sandbox 'sandbox' -DisposableVMId 'sandbox-id' -DisposableVMSentinel $env:ENDSTATE_DISPOSABLE_RECOVERY_VM
  $receipt = Get-Content -Raw $receiptPath | ConvertFrom-Json
  if ($receipt.status -ne 'BLOCKED' -or $receipt.scenarios.newestCleanRestore.result -ne 'PASS') { throw 'gate did not preserve parsed evidence without minting PASS' }

  @{ scenario='partialUpload'; result='NOT_EXECUTED'; asserted=$true } | ConvertTo-Json | Set-Content -NoNewline (Join-Path $root 'scenario-partialUpload.json')
  $rejected = $false
  try {
    & $scriptPath -ContextPath $contextPath -ReceiptPath $receiptPath -Commit '1234567' -Sandbox 'sandbox' -DisposableVMId 'sandbox-id' -DisposableVMSentinel $env:ENDSTATE_DISPOSABLE_RECOVERY_VM
  } catch { $rejected = $true }
  if (-not $rejected) { throw 'gate accepted NOT_EXECUTED without a scenario reason' }
} finally {
  Remove-Item -Recurse -Force $root
}
