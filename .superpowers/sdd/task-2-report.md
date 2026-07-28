# Task 2 — Windows observer, uninstall, and declared-boundary cleanup primitives

Base: `995dfb7`
Branch: `feat/hosted-live-notepad`
Worktree: `C:\tmp\endstate-hosted-live`

## Scope delivered

- The production live definition now declares all six deterministic Notepad++
  targets (five comparator files and the `userDefineLangs` directory), binds
  `TargetsSHA256` to that full declared set, and rejects duplicate, foreign,
  or overlapping declarations.
- The Windows observer has a production PE-version source and trusted exact
  AppX-resolved Winget install/uninstall wrappers. Process permits bind the
  executable image, exact arguments, environment, and receipt identity.
- Windows cleanup resolves only validated `APPDATA` descendants from the
  compiled definition, rejects links/reparse points and foreign/overlapping
  paths, removes exact regular-file or directory leaves without following
  links, and cleans only owned attempt roots.
- Closed boundary snapshots cover package observation, declared configuration
  targets, services, drivers, scheduled tasks, and pending-reboot indicators.
  Equality and absence checks are explicitly limited to that declared boundary.
- The two non-process mutations use a private session-bound host permit. The
  permit binds campaign, operation, sequence, nonce, issuer/admission token,
  expiry, definition/declared-targets/observer/workflow digests, and exact
  validated `APPDATA` or attempt-root digests. The issuer lifecycle is
  reserved → launch-committed → sealed; no raw sequence advance is exposed.

## Inherited state

The prior executor left the Task 2 production and test files uncommitted, but
left no Task 2 RED/GREEN report. The inherited focused suite was run before
continuation and passed:

```text
go test ./internal/validationharness -run "Windows|Observer|Process|Cleanup|Boundary|Version|Winget" -count=1 -timeout 5m
ok github.com/Artexis10/endstate/go-engine/internal/validationharness 11.905s
```

That result was treated as a starting point, not proof of the untested host
permit boundary.

## Continuation RED/GREEN evidence

1. `TestLiveAuthorityMintsOnlyBoundDeclaredTargetWipePermit` was extended to
   substitute `attempt-root-cleanup` into a wipe permit. It was RED:

   ```text
   host mutation permit accepted a substituted operation
   ```

   `liveHostMutationCapability.matches` now requires the exact admission
   operation and the matching zero/nonzero attempt-root binding shape. The
   focused test then passed.

2. `TestLiveHostMutationReceiptSealerBindsFinalizedTarget` was added before
   changing the issuer. It was RED:

   ```text
   host mutation receipt sealer accepted a substituted target
   ```

   The receipt issuer now records the binding accepted by the host finalizer
   and requires that exact binding when sealing the typed host receipt. The
   focused test then passed.

3. `TestWindowsLiveDeclaredTargetWipeRejectsWrongRootAndReplayWithoutMutation`
   verifies a wrong-root permit and a replay leave their targets intact and do
   not advance the issuer. Its first run exposed an invalid test assumption:
   authority issuers accept only campaign-derived nonces. The assertion was
   corrected to test the next planned sequence with its authority nonce; the
   production behavior was already fail-closed and the corrected test passed.

## Verification

Every Go command used:

```powershell
$env:APPDATA='C:\tmp\endstate-hosted-live-appdata'
$env:GOCACHE='C:\tmp\endstate-hosted-live-go-cache'
$env:GOFLAGS='-buildvcs=false'
```

| Command | Result |
| --- | --- |
| `go test ./internal/validationharness -run 'Windows\|Observer\|Process\|Cleanup\|Boundary\|Version\|Winget\|HostMutation\|DeclaredTarget' -count=1 -timeout 5m` | PASS (`ok`, 6.253s) |
| `go test ./internal/validationharness -count=1 -timeout 5m` | PASS (`ok`, 91.127s) |
| `go vet ./internal/validationharness` | PASS (no output) |
| `go build ./internal/validationharness` | PASS (no output) |
| `GOOS=linux GOARCH=amd64 go build ./internal/validationharness` | PASS (no output) |
| `git diff --check 995dfb7` | PASS (no whitespace errors) |

## Deviations

None. Task 3 lifecycle orchestration, CLI, workflow YAML, result persistence,
and promotion remain outside this task.
