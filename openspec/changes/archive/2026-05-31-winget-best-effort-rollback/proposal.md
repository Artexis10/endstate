## Why

Phase 3 (`nix-native-rollback`, PR #60, merged) added the top-level `rollback` command for
backends that advertise **native** rollback — the Nix realizer, via `nix profile rollback`.
Hosts whose backend is the winget driver (Windows) currently refuse with
`ROLLBACK_UNSUPPORTED`, because winget has no native rollback and the engine had no uninstall
path at all.

This change is **Phase 4 — Windows migrates in**: a **best-effort** winget rollback. Since
winget cannot atomically switch generations, the engine reverts by **reconstructing the
package delta from the recorded Provisioning Generations** and uninstalling what was added
after the target — the union of the `addedRefs` of every generation numbered greater than the
target. This is the first uninstall path in the engine. It is best-effort by nature: winget
pulls transitive dependencies and co-installs that the engine never recorded, so a rollback
may leave orphans or fail on a package another package still depends on. Per the maintainer
(architecture plan open decision #3), this is **accepted as documented best-effort with a
warning**, not prevented.

It is `--confirm`-gated (default refuses — honors `non-destructive-defaults`, symmetric with
the Phase 3 gate), with a `--dry-run` preview. It operates on **packages only** (the
`separation-of-concerns` "Rollback operates on packages only" requirement added in Phase 3
already covers it).

## What Changes

- Add an optional **`driver.Uninstaller`** interface (`Uninstall(ref) (*UninstallResult, error)`)
  + `UninstallResult` to the `driver` package, discovered by type-assertion exactly like
  `driver.BatchDetector`. Implement it on the winget driver via `winget uninstall` (the first
  uninstall path in the engine), classifying exit codes to outcomes
  (`uninstalled` / `absent` / `failed`); an already-absent package is a successful no-op.
- Extend the **`rollback`** command (`internal/commands/rollback.go`): when no realizer is
  present but the driver implements `Uninstaller`, take a **best-effort transaction-rollback**
  path:
  - Resolve the target by **engine generation number** (`--to <N>`; bare = the most recent
    generation). Compute `removeRefs` = union of `addedRefs` of all generations with
    `number > N`.
  - Uninstall each ref independently (non-atomic): collect per-ref outcomes, never abort on the
    first failure; an already-absent ref counts as removed. Mark the run **partial** when any
    uninstall failed. Surface a **warning** that winget-pulled transitive deps/co-installs are
    untracked and may be orphaned.
  - Require `--confirm` (default refuses); `--dry-run` lists what would be removed without
    uninstalling.
  - On success (≥1 ref removed), **append** a new rollback-marked Provisioning Generation
    recording `removedRefs` (`addedRefs` empty; `partial` set from per-ref failures).
- Add an optional **`RemovedRefs []string`** field to `provision.Generation` (additive;
  `SchemaVersion` stays `"1.0"`).
- **No new error codes:** reuse `ROLLBACK_UNSUPPORTED` (backend can neither roll back natively
  nor uninstall), `GENERATION_NOT_FOUND` (bad `--to`), `WINGET_NOT_AVAILABLE` (winget binary
  missing), and `ROLLBACK_FAILED` (every targeted uninstall failed). Per-ref failures inside a
  partial run are reported in the result data, not as a top-level error code (mirrors `apply`).
- **No `cmd/endstate/main.go` change** — the `rollback` dispatch is already generic.

## Capabilities

### New Capabilities

- `winget-best-effort-rollback`: `rollback` works on non-native backends that can uninstall
  (winget), reverting by uninstalling the union of `addedRefs` recorded after the target
  generation. Per-package and non-atomic: it tolerates per-package failure (reporting partial),
  treats already-absent as a no-op, surfaces an orphan caveat, requires `--confirm` (with a
  `--dry-run` preview), and appends a rollback-marked generation recording what it removed.

### Modified Capabilities

- None. The package-stage-only guarantee is already asserted by the `separation-of-concerns`
  "Rollback operates on packages only" requirement (added in `nix-native-rollback`), which
  covers the whole `rollback` command. The `--confirm` gate realizes the existing
  `non-destructive-defaults` "destructive operations require explicit flags" invariant without
  modifying that spec.

## Impact

- `internal/driver/driver.go` — add `Uninstaller` optional interface + `UninstallResult` +
  status constants (`uninstalled` / `absent` / `failed`).
- `internal/driver/winget/uninstall.go` (new) — `Uninstall(ref)` via
  `winget uninstall --id <ref> -e --silent --accept-source-agreements`; exit-code/output
  classification. **Real-winget verification of the "no installed package found" exit code is
  on the maintainer's Windows side** (this WSL box has no winget); hermetic tests use the
  `ExecCommand` injection seam already on `WingetDriver`.
- `internal/commands/rollback.go` — refactor `RunRollback` to dispatch realizer (Phase 3) vs
  driver (Phase 4) paths; add `runDriverRollback` (compute `removeRefs`, per-ref uninstall,
  partial handling, warning, append generation).
- `internal/provision/provision.go` — additive optional `RemovedRefs []string` field.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**: extend
  `## Command: rollback` with the winget best-effort data shape (removed/absent/failed counts,
  `partial`, `removedRefs`, orphan-warning note).
- **Zero Windows behavior regression**: `rollback` previously returned `ROLLBACK_UNSUPPORTED`
  on Windows; it now performs a best-effort rollback. No other command changes. Proven by
  host-aware hermetic tests + `GOOS=windows` build/vet; real-winget smoke is the maintainer's.
