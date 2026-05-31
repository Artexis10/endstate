## Context

Phase 3 (`nix-native-rollback`) added `rollback` for **native-rollback** backends (Nix). The
command acquires a realizer via `newRealizerFn()`, gates on
`Rollbacker` + `CapabilityReporter.NativeRollback`, maps an engine generation number → the
backend-native anchor, and on success appends a rollback-marked Provisioning Generation. Hosts
with no realizer (Windows) refuse with `ROLLBACK_UNSUPPORTED`.

The winget driver (`driver.Driver`) exposes only `Detect` + `Install` — **no uninstall** — and
reports `Capabilities{}` all-false. Winget cannot atomically switch generations, so it has no
native rollback. But the engine already records, per `apply`, a Provisioning Generation with
`AddedRefs` = the refs installed that run. That recorded history is enough to reconstruct a
best-effort reversal.

## Goals / Non-Goals

**Goals:**
- `rollback` on winget = uninstall the union of `addedRefs` of all generations after the target.
- First uninstall path in the engine (`driver.Uninstaller`), discovered by type-assertion.
- Per-package, non-atomic, failure-tolerant; `--confirm`-gated; `--dry-run` preview.
- Append a rollback-marked generation recording `removedRefs`.
- Zero Windows regression (provable by hermetic tests + `GOOS=windows` build/vet).

**Non-Goals:**
- **No dependency-graph tracking / orphan prevention.** Best-effort with a warning (maintainer
  decision: architecture-plan open decision #3). winget-pulled transitive deps/co-installs are
  not recorded and may remain.
- **No convergence-to-exact-set** (uninstall packages never recorded as added) — that is Phase 5.
- **No config involvement** — package-stage only (already asserted by the
  `separation-of-concerns` "Rollback operates on packages only" requirement from Phase 3).

## Decisions

### Target = engine generation number; removeRefs = union of addedRefs after target

`rollback --to <N>` resolves `removeRefs` = ∪ `addedRefs` of every generation with `Number > N`.
Bare `rollback` targets the most recent generation (`N` = highest number − 1). This reverses the
recorded install transaction. The "union of addedRefs after N" math is robust to interspersed
rollback generations (they carry empty `addedRefs`, contributing nothing). If `removeRefs` is
empty, the rollback is a successful no-op ("nothing to roll back").

### `driver.Uninstaller` optional interface

```go
type UninstallResult struct { Status, Message string } // Status: uninstalled | absent | failed
type Uninstaller interface { Uninstall(ref string) (*UninstallResult, error) }
```
Discovered by type-assertion (like `driver.BatchDetector`). winget implements it via
`winget uninstall --id <ref> -e --silent --accept-source-agreements`:
- exit 0 → `uninstalled`
- "no installed package found" code/output → `absent` (successful no-op, for idempotency)
- other non-zero → `failed`
- binary missing → `(nil, ErrWingetNotAvailable)`

**Caveat:** the exact "no installed package found" winget exit code must be confirmed against a
real winget (this dev box is Linux/Nix, no winget) — mirrors how the install already-installed
code was locked. Hermetic tests use the existing `WingetDriver.ExecCommand` injection seam;
a best-effort output-substring fallback complements the exit-code check.

### Command flow (`runDriverRollback`, dispatched from `RunRollback`)

`RunRollback` dispatches: realizer present → Phase 3 native path; else driver present and is an
`Uninstaller` → this path; else `ROLLBACK_UNSUPPORTED`.

1. Resolve target N (`--to`, validated against `provision.List()`; missing → `GENERATION_NOT_FOUND`).
   Bare = most recent generation.
2. Compute `removeRefs`. Empty → success no-op.
3. `--dry-run` → report `removeRefs` as the preview, no uninstall. No `--confirm` (and not
   dry-run) → refuse (message names `--confirm`), no mutation.
4. For each ref: `Uninstall(ref)`; collect `{removed, absent, failed}`. Never abort early.
5. Emit the untracked-dependency warning.
6. If ≥1 ref removed: append a rollback-marked generation (`Backend="winget"`, `Rollback=true`,
   `AddedRefs=[]`, `RemovedRefs=<removed>`, `Partial`=any failure, `RunID="rollback-<ts>"`).
7. Result envelope: `success=true` with summary {removed, absent, failed, partial}; raw winget
   messages in per-ref entries. **Top-level error only** for: no uninstall-capable backend
   (`ROLLBACK_UNSUPPORTED`), bad target (`GENERATION_NOT_FOUND`), winget binary missing
   (`WINGET_NOT_AVAILABLE`), or **every** targeted uninstall failed (`ROLLBACK_FAILED`).
   (Mirrors `apply`, which returns a success envelope with a non-zero `failed` count.)

### Error codes — reuse, no new codes

`ROLLBACK_UNSUPPORTED` / `GENERATION_NOT_FOUND` / `ROLLBACK_FAILED` (from Phase 3) +
`WINGET_NOT_AVAILABLE` (existing). No `UNINSTALL_FAILED`: per-ref failures live in the result
data, not as an envelope code.

### Generation record change

Add optional `RemovedRefs []string \`json:"removedRefs,omitempty"\`` to `provision.Generation`
(additive; `SchemaVersion` stays `"1.0"`). A rollback generation has empty `AddedRefs` and
populated `RemovedRefs`.

## Separation of concerns

Already covered: the `separation-of-concerns` "Rollback operates on packages only" requirement
(added in `nix-native-rollback`) applies to the whole `rollback` command, including this path.
`runDriverRollback` touches only the package driver + `internal/provision`; it never imports
`internal/restore` or touches `state/backups/` (extend the existing rollback import-guard test).

## Risks / limitations (documented, not blockers)

- **Orphans:** winget-pulled transitive deps/co-installs are untracked → may remain after
  rollback. Surfaced as a warning; not prevented (accepted best-effort).
- **Dependency-protected uninstall:** winget may refuse to remove a package another package
  depends on → reported as a per-ref `failed`, run marked partial.
- **No real-winget CI here:** uninstall exit-code anchors verified hermetically + on the
  maintainer's Windows; `GOOS=windows` build/vet is the cross-compile gate.

## Migration

Purely additive. No manifest/envelope schema bump. No `cmd/endstate/main.go` change (the
`rollback` dispatch is already generic). Existing state, generations, and non-Windows behavior
unchanged.
