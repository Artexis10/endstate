# Cloud Recovery Drill

**Status:** Release gate
**Applies to:** Endstate Cloud, contract schema 2.1
**Owner:** whoever is cutting the release
**Runtime:** ~20 minutes wall clock, mostly unattended

This is the release gate for Endstate Cloud. It is the only procedure that proves the whole promise end to end — that a machine which has never seen your data can, given nothing but an email address, a passphrase, and a 24-word recovery phrase, reconstruct a profile byte for byte.

It is a documented, deterministic procedure with explicit pass/fail criteria, not a manual-test bullet someone ticks from memory. Every step below produces an artifact. The drill passes only when the final byte comparison passes; every other step is a precondition.

## Why this cannot be a CI job

The release receipt also records a selected older-generation restore. The
staging negative drill must corrupt or remove the newest manifest and a newest
chunk in turn, prove the pull fails safely without replacing the local target,
and prove an older valid generation remains selectable.

Ordinary CI cannot run it, and pretending otherwise is how this class of bug ships:

- **A clean machine is the point.** The failure modes this catches — a DEK that only unwraps because it is already cached in the keychain, a restore that only works because the profile is still on disk — are invisible on any machine that has run `backup push`. The drill needs a genuinely fresh Windows with an empty credential store.
- **It needs a real backend.** Presigned URLs, quota accounting, retention pruning, and the commit endpoint are server behaviours. An `httptest` mock proves the engine's half of the contract (and the unit suite does exactly that); it cannot prove the two halves agree.
- **It creates a real account.** Signup writes a row, consumes a subscription, and stores blobs. That is not something to do on every push.

The unit suite and this drill are complements, not alternatives. The unit suite in `go-engine/internal/commands/backup_orchestration_test.go` proves the client-side invariants on every commit — a failed upload never commits, a commit is sent exactly once, a hash mismatch refuses to decrypt. This drill proves the composition against a live substrate.

## Preconditions

| Requirement | Notes |
|---|---|
| Windows 10/11 with Windows Sandbox enabled | `Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM` (run once, on the host, as admin; reboot required) |
| A staging substrate | See [Pointing at staging](#pointing-at-staging). Never run the drill against production. |
| A test account on that substrate | The email MUST match the staging `HOSTED_BACKUP_TEST_EMAIL_PATTERN` — see [Test accounts](#test-accounts) |
| The engine built from the commit under test | `cd go-engine && go build -o ../bin/endstate.exe ./cmd/endstate` |
| Networking enabled in the sandbox | The `.wsb` below sets `<Networking>Default</Networking>`. The existing module-validation harness does the same. |

## Harness

The drill reuses the Windows Sandbox harness under `sandbox-tests/`, in the shape `scripts/sandbox-validate.ps1` established:

- the repo is mapped read-write into the sandbox at `C:\Endstate`;
- a `.wsb` file's `<LogonCommand>` opens the guarded sandbox session;
- the operator writes sentinel files into a mapped artifact directory — `STARTED.txt`, `STEP.txt`, then exactly one of `DONE.txt` or `ERROR.txt`, plus `result.json` conforming to the receipt schema;
- the host polls for a sentinel with a timeout and reports PASS/FAIL.

Because the sandbox is discarded on close, "wipe the machine" is free and total: no keychain entry, no profile, no engine state survives.

Artifacts go to `sandbox-tests/validation/cloud-recovery/<timestamp>/`.

### Sandbox configuration

Save as `sandbox-tests/validation/cloud-recovery/<timestamp>/drill.wsb`, substituting the repo path and artifact path:

```xml
<Configuration>
  <Networking>Default</Networking>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>C:\path\to\endstate</HostFolder>
      <SandboxFolder>C:\Endstate</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>powershell.exe -NoExit</Command>
  </LogonCommand>
</Configuration>
```

This document is an execution procedure, not execution evidence. A release
candidate has passed only when a guarded Windows Sandbox run produces the
machine-readable receipt described below. Do not mark a documentation review,
unit test, or an unexecuted command transcript as a completed drill.

## Pointing at staging

Two environment variables, per contract §9. Set them in the sandbox before any `endstate backup` call:

```powershell
$env:ENDSTATE_OIDC_ISSUER_URL = "https://staging.substratesystems.io"
```

```powershell
$env:ENDSTATE_OIDC_AUDIENCE = "endstate-backup"
```

Confirm the engine resolved the backend before going further:

```powershell
C:\Endstate\bin\endstate.exe backup status --json
```

The envelope's `data.issuerUrl` MUST equal the staging URL. If it shows `https://substratesystems.io`, the environment variable did not take — stop, do not proceed against production.

## Test accounts

Staging substrate honours `HOSTED_BACKUP_TEST_EMAIL_PATTERN`, a server-side regular expression that marks an address as a drill account. Accounts matching it are provisioned with a subscription without going through Paddle, and are safe to purge in bulk. Use an address that matches the pattern configured on the staging deployment; ask the substrate owner for the current value rather than guessing.

The drill creates a **fresh** account every run. Do not reuse one: reusing an account means the retention and quota assertions run against unknown prior state, and a stale keychain entry on the host can mask a broken login path.

## The drill

Every command is a single line. Run them in order, inside the sandbox, from `C:\Endstate`. Record each envelope; `result.json` is the concatenation.

Set the run variables first:

```powershell
$Email = "endstate-drill-$(Get-Date -Format yyyyMMddHHmmss)@example.test"
```

```powershell
$Artifacts = "C:\Endstate\sandbox-tests\validation\cloud-recovery\TIMESTAMP"
```

```powershell
$Engine = "C:\Endstate\bin\endstate.exe"
```

### Step 1 — Signup

```powershell
& $Engine backup signup --email $Email --save-recovery-to "$Artifacts\recovery.txt" --json | Tee-Object "$Artifacts\01-signup.json"
```

The passphrase is read from stdin. Use a fixed drill passphrase; it is not a secret, and determinism matters more than entropy here.

**Assert:** envelope `success` is `true`; `$Artifacts\recovery.txt` exists and contains exactly 24 whitespace-separated BIP39 words. Count them — a 12-word phrase means the client generated 128 bits instead of 256 and the drill fails here (contract §6).

### Step 2 — Capture a profile

```powershell
& $Engine capture --json | Tee-Object "$Artifacts\02-capture.json"
```

**Assert:** `success` is `true`, then set the captured profile path directly
from the envelope:

```powershell
$Profile = ((Get-Content "$Artifacts\02-capture.json" -Raw | ConvertFrom-Json).data.outputPath)
```

Snapshot the original profile as a file inside a directory for the final
comparison — `backup pull --to` publishes a directory, so comparing a bare
source file to that directory would be structurally invalid. This copy lives
on the host-mapped folder and therefore survives the sandbox:

```powershell
$OlderOriginalDir = "$Artifacts\original-older"
New-Item -ItemType Directory -Force $OlderOriginalDir | Out-Null
Copy-Item -Force $Profile (Join-Path $OlderOriginalDir (Split-Path -Leaf $Profile))
```

### Step 3 — Push an older generation

```powershell
& $Engine backup push --profile $Profile --name "recovery-drill" --json | Tee-Object "$Artifacts\03-push.json"
```

**Assert:** `success` is `true`. Record `data.backupId` as `$BackupID` and `data.versionId` as `$OlderVersionID`.

A push that reports success has crossed its negotiated durability boundary. For
`requiresCommit: true`, the engine commits only after every chunk and the
manifest are stored, and a failed commit (including 404) fails the push. An
absent or `false` field is the legacy create-is-durable bridge. Step 4 verifies
server visibility rather than inferring durability from the response schema
header.

### Step 4 — Push the newest generation and verify both are committed

Capture again, then push the second capture into the same backup. This creates
the explicit older-generation case rather than merely assuming a previous
version remains usable:

```powershell
& $Engine capture --json | Tee-Object "$Artifacts\03b-capture-newest.json"
```

```powershell
$Profile = ((Get-Content "$Artifacts\03b-capture-newest.json" -Raw | ConvertFrom-Json).data.outputPath)
```

```powershell
$NewestOriginalDir = "$Artifacts\original-newest"; New-Item -ItemType Directory -Force $NewestOriginalDir | Out-Null; Copy-Item -Force $Profile (Join-Path $NewestOriginalDir (Split-Path -Leaf $Profile))
```

```powershell
& $Engine backup push --backup-id $BackupID --profile $Profile --json | Tee-Object "$Artifacts\03c-push-newest.json"
```

Record the second push's `data.versionId` as `$NewestVersionID`. It MUST differ
from `$OlderVersionID`.

```powershell
& $Engine backup versions --backup-id $BackupID --json | Tee-Object "$Artifacts\04-versions.json"
```

**Assert, all four:**

1. `$OlderVersionID` and `$NewestVersionID` each appear in `data.versions[]`. An uncommitted version is not listed at all under schema 2.1 — presence is the server-side proof.
2. Each version has a non-empty 64-character `manifestSha256` and a `size` greater than zero.
3. `data.versions[]` has exactly two entries. A fresh account with two completed pushes must have exactly two generations; anything else indicates a leaked partial upload or an unexpected prune.

Persist every value needed by the new disposable sandbox before closing it;
PowerShell variables do not survive that boundary:

```powershell
@{ email=$Email; backupId=$BackupID; olderVersionId=$OlderVersionID; newestVersionId=$NewestVersionID; engine=$Engine; artifacts=$Artifacts } | ConvertTo-Json | Set-Content "$Artifacts\recovery-context.json"
```

### Step 5 — Wipe

Close the Windows Sandbox window.

That is the wipe. It destroys the OS image, the credential store holding the DEK and refresh token, and every local copy of the profile. Nothing is left but what is on the server and the `$Artifacts` directory on the host.

Do not shortcut this with `endstate backup logout` or by deleting the keychain entry. Those clear what we know to clear; the point of the drill is to prove nothing else is load-bearing.

### Step 6 — Recover on a clean machine

Launch a **new** sandbox from the same `.wsb`. Re-export the two staging environment variables (step: [Pointing at staging](#pointing-at-staging)) — a fresh sandbox has none of them.

Re-initialise the fixed host-mapped artifact path before reading any persisted
context; `$Artifacts` was only a PowerShell variable in the destroyed sandbox:

```powershell
$Artifacts = "C:\Endstate\sandbox-tests\validation\cloud-recovery\TIMESTAMP"
if (-not (Test-Path "$Artifacts\recovery-context.json")) { throw "Missing persisted recovery context: $Artifacts\recovery-context.json" }
```

```powershell
$Context = Get-Content "$Artifacts\recovery-context.json" -Raw | ConvertFrom-Json; $Email=$Context.email; $BackupID=$Context.backupId; $OlderVersionID=$Context.olderVersionId; $NewestVersionID=$Context.newestVersionId; $Engine=$Context.engine
```

```powershell
& $Engine backup recover --email $Email --json | Tee-Object "$Artifacts\06-recover.json"
```

The command reads the 24-word phrase from stdin, then a new passphrase. Use the phrase from `$Artifacts\recovery.txt` and any new passphrase.

**Assert:** `success` is `true`; `data.userId` matches the value from step 1. The DEK has now been unwrapped from `recoveryKeyWrappedDEK` on a machine that never held the original passphrase — that is the trust-model claim in contract §1 and §6, demonstrated rather than asserted.

### Step 7 — Pull

```powershell
& $Engine backup list --json | Tee-Object "$Artifacts\07-list.json"
```

**Assert:** the backup named `recovery-drill` is present with `versionCount` of 2. Record its `id` as `$BackupID` (it will match step 3; confirm it rather than assuming).

```powershell
& $Engine backup pull --backup-id $BackupID --to "$Artifacts\restored-newest" --overwrite --json | Tee-Object "$Artifacts\08-pull-newest.json"
```

**Assert:** `success` is `true`; `data.versionId` equals `$NewestVersionID`.

Restore the older generation explicitly; default selection is not evidence that
an older generation remains selectable:

```powershell
& $Engine backup pull --backup-id $BackupID --version-id $OlderVersionID --to "$Artifacts\restored-older" --overwrite --json | Tee-Object "$Artifacts\08b-pull-older.json"
```

**Assert:** `success` is `true`; `data.versionId` equals `$OlderVersionID`.

### Step 8 — Byte comparison

This is the gate. Everything above is setup.

```powershell
$a = Get-ChildItem -Recurse -File "$Artifacts\original-newest" | Sort-Object FullName | ForEach-Object { "{0}`t{1}" -f $_.FullName.Substring("$Artifacts\original-newest".Length), (Get-FileHash $_.FullName -Algorithm SHA256).Hash }
```

```powershell
$b = Get-ChildItem -Recurse -File "$Artifacts\restored-newest" | Sort-Object FullName | ForEach-Object { "{0}`t{1}" -f $_.FullName.Substring("$Artifacts\restored-newest".Length), (Get-FileHash $_.FullName -Algorithm SHA256).Hash }
```

```powershell
$diff = Compare-Object $a $b; $diff | Out-File "$Artifacts\09-diff.txt"; if ($diff) { "FAIL: $($diff.Count) differences" } else { "PASS: byte-identical" }
```

Repeat the same comparison with `$Artifacts\original-older` and
`$Artifacts\restored-older`; save it as `$OlderDiff`.

**Assert:** `Compare-Object` returns nothing. Both the relative paths and the SHA-256 of every file must match. A difference in the path set means the tar round-trip dropped or renamed an entry; a difference in a hash means the content changed.

### Step 9 — Destructive staging scenarios

These are guarded, destructive checks against the **fresh staging test account
only**. They are not production checks and they are not satisfied by unit
tests. The staging operator records the object keys and actions in
`$Artifacts\09-destructive-actions.txt`; never put a recovery phrase, access
token, or presigned URL in that file.

Before each corrupt or missing-object pull, make a local preservation target:

```powershell
$Preserved = "$Artifacts\preserved-local"; New-Item -ItemType Directory -Force $Preserved | Out-Null; "must-survive" | Out-File "$Preserved\sentinel.txt"
```

For the **corrupt newest manifest** scenario, use the staging object-store
console or the approved staging-only operator tool to replace the encrypted
manifest object belonging to `$NewestVersionID` with different bytes. Run:

```powershell
& $Engine backup pull --backup-id $BackupID --to $Preserved --overwrite --json | Tee-Object "$Artifacts\09a-corrupt-manifest.json"
```

**Assert:** the pull fails, and `sentinel.txt` still contains `must-survive`.
Restore the original manifest before proceeding, then confirm the older
generation still restores with the explicit command from step 7.

For the **missing or corrupt newest chunk** scenario, delete one encrypted
chunk from `$NewestVersionID`, or replace it with different bytes, using that
same staging-only operator path. Repeat the pull into a newly seeded
`$Preserved` directory.

**Assert:** the pull fails safely, the sentinel remains, and the explicit older
generation remains restorable. Restore the original chunk before the next
scenario.

For the **partial upload and retry** scenario, arrange a staging-only upload
fault after the manifest PUT and before one chunk PUT completes (the approved
staging fault injector or object-store network rule is required; do not kill a
production process). Start a third `backup push`, stop it at that fault, and
list versions:

```powershell
& $Engine backup versions --backup-id $BackupID --json | Tee-Object "$Artifacts\09c-after-partial.json"
```

**Assert:** no partial version is listed or selected. Remove the fault, rerun
the identical `backup push`, and list versions again. **Assert:** the retry
returns a new version ID and exactly one additional complete version is listed.

If the staging environment does not have the required object-store access or
fault injector, record each affected scenario as `NOT_EXECUTED` with the exact
missing capability. A receipt containing `NOT_EXECUTED` is an explicit release
gate block, not a passing drill.

### Step 10 — Record the result

Write the sentinel and the machine-readable result. The receipt must conform
to [`cloud-recovery-receipt.schema.json`](cloud-recovery-receipt.schema.json);
the release candidate and sandbox identity make it auditable rather than a
claim copied from a different run:

Each assertion writes `scenario-<name>.json` with `{scenario, result,
asserted:true}`; never seed a scenario as PASS. The helper records those parsed
assertion records, but deliberately cannot issue `PASS`: the destructive
object-store work and clean-Sandbox recovery remain manually audited. Every
`NOT_EXECUTED` record must name its own exact missing capability in `reason`.
Inside the disposable Sandbox only, set the guard before running the gate:

```powershell
$env:ENDSTATE_DISPOSABLE_RECOVERY_VM = 'ENDSTATE_DISPOSABLE_RECOVERY_VM'
& C:\Endstate\scripts\cloud-recovery-gate.ps1 -ContextPath "$Artifacts\recovery-context.json" -ReceiptPath "$Artifacts\result.json" -Commit (git -C C:\Endstate rev-parse HEAD) -Sandbox $env:COMPUTERNAME -DisposableVMId $env:COMPUTERNAME -DisposableVMSentinel $env:ENDSTATE_DISPOSABLE_RECOVERY_VM
```

```powershell
$Receipt = Get-Content "$Artifacts\result.json" -Raw | ConvertFrom-Json; "drill $($Receipt.status) — attach raw artifacts for manual release review" | Out-File "$Artifacts\ERROR.txt"
```

## Pass / fail criteria

The drill **passes** only when all of the following hold. There is no partial credit.

| # | Criterion | Step |
|---|---|---|
| 1 | Signup succeeds and writes a 24-word BIP39 phrase | 1 |
| 2 | Capture succeeds and produces a profile | 2 |
| 3 | Push succeeds | 3 |
| 4 | The pushed version is listed by the server, with a non-empty `manifestSha256` and non-zero size | 4 |
| 5 | Exactly two complete versions are listed for two pushes | 4 |
| 6 | Recovery on a clean machine succeeds using only email + recovery phrase | 6 |
| 7 | The backup and its version are visible after recovery | 7 |
| 8 | Default pull restores the newest version, and explicit pull restores the selected older version | 7 |
| 9 | Newest restored tree is byte-identical to the original: same relative paths, same SHA-256 per file | 8 |
| 10 | Corrupt newest manifest and missing/corrupt newest chunk fail without replacing the local target; older version remains restorable | 9 |
| 11 | Partial upload is not listed, and retry creates one complete version | 9 |

The drill **fails** if any command returns a non-zero exit code, any envelope reports `success: false` except an expected negative pull, any scenario is `FAIL` or `NOT_EXECUTED`, or criterion 9 does not hold. A failure is a release blocker, not a flake — re-run it once to rule out a transient network fault, and if it reproduces, stop the release.

`result.json` is a manual-review record, not a release pass. Attach it with the
raw command envelopes, version listings, hashes, and destructive-action log;
the release reviewer records the final decision separately.

## Additional retention drill

When retention or quota code changes, push five good generations, then
interrupt a sixth through the staging fault injector. All five good generations
MUST remain listed. This is additional to the mandatory partial-upload scenario
in step 9; it proves a failed sixth generation cannot evict the oldest valid
one.

## Cleanup

Delete the drill account on staging when finished:

```powershell
& $Engine account delete --confirm --json
```

Test-pattern accounts are also purged in bulk on the staging substrate, so a missed cleanup is untidy rather than harmful. Keep the `$Artifacts` directory — it is the release evidence.

## References

- `docs/contracts/hosted-backup-contract.md` §6 (recovery), §7 (commit endpoint), §8 (durability and retention), §10 (grace and purge windows)
- `sandbox-tests/powertoys-afterburner/` — the original sandbox contract-test harness this drill's shape follows
- `scripts/sandbox-validate.ps1` — the sentinel/polling conventions (`STARTED.txt`, `STEP.txt`, `DONE.txt`, `ERROR.txt`, `result.json`)
- `docs/VALIDATION.md` — how sandbox validation relates to CI evidence, and what CI evidence explicitly does not prove
- `go-engine/internal/commands/backup_orchestration_test.go` — the hermetic counterpart to this drill
