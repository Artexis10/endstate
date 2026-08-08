# Cloud Recovery Drill

**Status:** Release gate
**Applies to:** Endstate Hosted Backup, contract schema 2.1
**Owner:** whoever is cutting the release
**Runtime:** ~20 minutes wall clock, mostly unattended

This is the release gate for Hosted Backup. It is the only procedure that proves the whole promise end to end — that a machine which has never seen your data can, given nothing but an email address, a passphrase, and a 24-word recovery phrase, reconstruct a profile byte for byte.

It is a documented, deterministic procedure with explicit pass/fail criteria, not a manual-test bullet someone ticks from memory. Every step below produces an artifact. The drill passes only when the final byte comparison passes; every other step is a precondition.

## Why this cannot be a CI job

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
- a `.wsb` file's `<LogonCommand>` starts the in-sandbox script;
- the in-sandbox script writes sentinel files into a mapped artifact directory — `STARTED.txt`, `STEP.txt`, then exactly one of `DONE.txt` or `ERROR.txt`, plus `result.json`;
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
    <Command>powershell.exe -ExecutionPolicy Bypass -NoExit -File "C:\Endstate\docs\testing\cloud-recovery-drill.ps1" -OutputDir "C:\Endstate\sandbox-tests\validation\cloud-recovery\TIMESTAMP"</Command>
  </LogonCommand>
</Configuration>
```

A companion in-sandbox script is not yet committed; until it is, run the steps below manually inside the sandbox and write the sentinels by hand. The step list is the specification for that script — implement it verbatim.

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

**Assert:** `success` is `true`. Note the captured profile path from the envelope; call it `$Profile`.

Snapshot the original for the final comparison — this copy lives on the host-mapped folder and therefore survives the sandbox:

```powershell
Copy-Item -Recurse -Force $Profile "$Artifacts\original"
```

### Step 3 — Push

```powershell
& $Engine backup push --profile $Profile --name "recovery-drill" --json | Tee-Object "$Artifacts\03-push.json"
```

**Assert:** `success` is `true`. Record `data.backupId` as `$BackupID` and `data.versionId` as `$VersionID`.

A push that reports success has, by construction, committed the version — the engine commits only after every chunk and the manifest are stored, and a failed commit fails the push (contract §7, §8). Step 4 verifies that from the server's side rather than trusting the client.

### Step 4 — Verify the version is committed

```powershell
& $Engine backup versions --backup-id $BackupID --json | Tee-Object "$Artifacts\04-versions.json"
```

**Assert, all four:**

1. `$VersionID` appears in `data.versions[]`. An uncommitted version is not listed at all under schema 2.1 — its presence here is the commit's server-side proof.
2. Its `manifestSha256` is a non-empty 64-character hex string. The pull in step 7 verifies the manifest blob against this value; an empty value silently disables that gate.
3. Its `size` is greater than zero.
4. `data.versions[]` has exactly one entry. A fresh account with one push must have exactly one generation; more means a partial upload from a previous attempt leaked into the listing, which is the exact defect this contract version closes.

### Step 5 — Wipe

Close the Windows Sandbox window.

That is the wipe. It destroys the OS image, the credential store holding the DEK and refresh token, and every local copy of the profile. Nothing is left but what is on the server and the `$Artifacts` directory on the host.

Do not shortcut this with `endstate backup logout` or by deleting the keychain entry. Those clear what we know to clear; the point of the drill is to prove nothing else is load-bearing.

### Step 6 — Recover on a clean machine

Launch a **new** sandbox from the same `.wsb`. Re-export the two staging environment variables (step: [Pointing at staging](#pointing-at-staging)) — a fresh sandbox has none of them.

```powershell
& $Engine backup recover --email $Email --json | Tee-Object "$Artifacts\06-recover.json"
```

The command reads the 24-word phrase from stdin, then a new passphrase. Use the phrase from `$Artifacts\recovery.txt` and any new passphrase.

**Assert:** `success` is `true`; `data.userId` matches the value from step 1. The DEK has now been unwrapped from `recoveryKeyWrappedDEK` on a machine that never held the original passphrase — that is the trust-model claim in contract §1 and §6, demonstrated rather than asserted.

### Step 7 — Pull

```powershell
& $Engine backup list --json | Tee-Object "$Artifacts\07-list.json"
```

**Assert:** the backup named `recovery-drill` is present with `versionCount` of 1. Record its `id` as `$BackupID` (it will match step 3; confirm it rather than assuming).

```powershell
& $Engine backup pull --backup-id $BackupID --to "$Artifacts\restored" --overwrite --json | Tee-Object "$Artifacts\08-pull.json"
```

**Assert:** `success` is `true`; `data.versionId` equals the `$VersionID` from step 3.

### Step 8 — Byte comparison

This is the gate. Everything above is setup.

```powershell
$a = Get-ChildItem -Recurse -File "$Artifacts\original" | Sort-Object FullName | ForEach-Object { "{0}`t{1}" -f $_.FullName.Substring("$Artifacts\original".Length), (Get-FileHash $_.FullName -Algorithm SHA256).Hash }
```

```powershell
$b = Get-ChildItem -Recurse -File "$Artifacts\restored" | Sort-Object FullName | ForEach-Object { "{0}`t{1}" -f $_.FullName.Substring("$Artifacts\restored".Length), (Get-FileHash $_.FullName -Algorithm SHA256).Hash }
```

```powershell
$diff = Compare-Object $a $b; $diff | Out-File "$Artifacts\09-diff.txt"; if ($diff) { "FAIL: $($diff.Count) differences" } else { "PASS: byte-identical" }
```

**Assert:** `Compare-Object` returns nothing. Both the relative paths and the SHA-256 of every file must match. A difference in the path set means the tar round-trip dropped or renamed an entry; a difference in a hash means the content changed.

### Step 9 — Record the result

Write the sentinel and the machine-readable result:

```powershell
@{ status = if ($diff) { "FAIL" } else { "PASS" }; email = $Email; backupId = $BackupID; versionId = $VersionID; commit = (git -C C:\Endstate rev-parse HEAD); timestamp = (Get-Date -Format o) } | ConvertTo-Json | Out-File "$Artifacts\result.json"
```

```powershell
if ($diff) { "drill failed" | Out-File "$Artifacts\ERROR.txt" } else { "drill passed" | Out-File "$Artifacts\DONE.txt" }
```

## Pass / fail criteria

The drill **passes** only when all of the following hold. There is no partial credit.

| # | Criterion | Step |
|---|---|---|
| 1 | Signup succeeds and writes a 24-word BIP39 phrase | 1 |
| 2 | Capture succeeds and produces a profile | 2 |
| 3 | Push succeeds | 3 |
| 4 | The pushed version is listed by the server, with a non-empty `manifestSha256` and non-zero size | 4 |
| 5 | Exactly one version is listed for a single push | 4 |
| 6 | Recovery on a clean machine succeeds using only email + recovery phrase | 6 |
| 7 | The backup and its version are visible after recovery | 7 |
| 8 | Pull succeeds and returns the same `versionId` that was pushed | 7 |
| 9 | Restored tree is byte-identical to the original: same relative paths, same SHA-256 per file | 8 |

The drill **fails** if any command returns a non-zero exit code, any envelope reports `success: false`, or criterion 9 does not hold. A failure is a release blocker, not a flake — re-run it once to rule out a transient network fault, and if it reproduces, stop the release.

`result.json` with `status: "PASS"` and a `commit` matching the release candidate is the evidence to attach to the release.

## Negative drills (recommended, not gating)

Two variants are worth running when the upload or commit path has changed. Neither is required to cut a release.

**Interrupted push.** Run step 3 and kill the engine process partway through the upload. Then run step 4. The killed version MUST NOT appear in the listing, and `versionCount` MUST NOT have incremented. This is the defect the commit endpoint exists to close; the unit suite covers it against a mock, and this covers it against a real server.

**Retention is not evicted by a failure.** Push five good generations, then interrupt a sixth. All five good generations MUST remain listed. Under the pre-2.1 behaviour the failed sixth would have pruned the oldest good one at create time.

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
