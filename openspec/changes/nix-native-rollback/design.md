## Context

After `provisioning-generation` (Phase 2), the engine writes a numbered Provisioning
Generation per successful `apply` (both backends) under `state/generations/<n>.json`, lists
them via the read-only `generations` command, and the Nix realizer advertises
`Capabilities{NativeRollback: true}`. `provision.Rollbacker` (`Rollback(to int) error`) is
**declared but unimplemented**. Each Nix-backed generation records `Native` = the Nix profile
version active when it was committed.

This change implements rollback for native-rollback backends (Nix) and adds the `rollback`
command. The dual `Driver`+`Realizer` split is unchanged; winget rollback is Phase 4.

## Goals / Non-Goals

**Goals:**
- A top-level `rollback` command that reverts the package set to a prior Provisioning
  Generation, identified by **engine generation number** (the moat: never a raw Nix version).
- `provision.Rollbacker` implemented on the Nix realizer via `nix profile rollback`.
- `--confirm`-gated with a `--dry-run` preview (non-destructive default).
- Append a new Provisioning Generation on success (append-only, honest history).
- **Zero Windows regression** — provable by host-aware tests + `GOOS=windows` build.

**Non-Goals:**
- **No winget rollback** (Phase 4) — non-native-rollback backends refuse cleanly.
- **No convergence / uninstall-drift** (Phase 5).
- **No config/restore involvement** — rollback is package-stage only.
- **No generation pruning / retention policy change.**

## Decisions

### (a) Target identity = engine generation number

`rollback --to <N>` resolves engine Provisioning Generation N from `provision.List()`, reads
its `Native` field, and rolls the backend to that native anchor. Bare `rollback` (no `--to`)
reverts to the immediately previous version. Rationale: `generations` already presents
engine-owned numbers; mapping `N → Native` keeps the UX in engine terms and preserves the
"user never learns Nix" moat. `Native` was shipped in Phase 2 for exactly this.

### (b) Append a new Provisioning Generation on success

The engine's generation store is an append-only list with **no HEAD pointer** — `generations`
simply lists files newest-first. If rollback only repointed the Nix profile and recorded
nothing, the newest engine generation would still describe the pre-rollback set, so the list
would no longer reflect what is active. Therefore a successful rollback appends a new
generation snapshotting the now-active set: `Items` = the current profile set (status
`present`), `AddedRefs = []`, `Native` = the active Nix version after rollback,
`RunID = "rollback-<ts>"`, `Rollback = true`.

*Empirical note (Determinate Nix 3.21.0):* `nix profile rollback` switches the profile to an
**existing** older version (`"switching profile from version 519 to 518"`); it does **not**
mint a new Nix version. So the appended record is an *engine* generation whose `Native` points
back at the older Nix version — not a new Nix generation. This corrects the handoff's §4(b)
parenthetical.

### (c) Require `--confirm`

Rollback changes the installed set, so it is `--confirm`-gated (default refuses; `--dry-run`
previews without confirmation or mutation). Consistent with `non-destructive-defaults` and
symmetric with the Phase 4 `--confirm`-gated winget rollback. Nix rollback is atomic and
reversible (roll forward again with `--to`), but the gate is kept for spec consistency.

### Backend method

`func (b *Backend) Rollback(to int) error` (satisfies `provision.Rollbacker`):
- `to <= 0` → `nix profile rollback --profile <profile>` (previous).
- `to > 0`  → `nix profile rollback --profile <profile> --to <to>` (`to` is the **Nix** version).
- Injected `Runner` seam (hermetic tests). Failures classified via the existing `classify(...)`
  → `*realizer.Error` (REALIZER_UNAVAILABLE on spawn/daemon, PERMISSION_DENIED, else
  ROLLBACK_FAILED). Raw text only in `Error.Raw` → `error.detail`.

### Command flow (`RunRollback`)

1. `r, err := newRealizerFn()`; `err != nil` (e.g. Windows) → `ROLLBACK_UNSUPPORTED`.
2. `rb, ok := r.(provision.Rollbacker)`; require `CapabilityReporter.Capabilities().NativeRollback`.
   Else → `ROLLBACK_UNSUPPORTED`.
3. Resolve native target: `--to N` → load gen N (missing/no-`Native` → `GENERATION_NOT_FOUND`),
   `native = atoi(Native)`; no `--to` → `native = -1` (previous).
4. `--dry-run` → return resolved-target preview, no mutation. No `--confirm` (and not dry-run)
   → refuse with remediation, no mutation.
5. `rb.Rollback(native)`; on `*realizer.Error`: systemic (`isSystemic`) → top-level envelope
   error; else `ROLLBACK_FAILED`. Raw text confined to detail.
6. On success: read `r.Current()`, append a rollback-marked Provisioning Generation; return a
   `RollbackResult` (from/to native versions, new engine generation number).

### Error codes (additive)

| Code | When |
|------|------|
| `ROLLBACK_UNSUPPORTED` | Host backend does not advertise native rollback (winget / no realizer) |
| `GENERATION_NOT_FOUND` | `--to N` references a missing generation, or one with no native anchor |
| `ROLLBACK_FAILED` | The backend rollback failed (non-systemic); raw text in `error.detail` |

Systemic infrastructure failures during rollback reuse `REALIZER_UNAVAILABLE` /
`PERMISSION_DENIED` (same classification as `apply`).

### Generation record change

Add optional `Rollback bool \`json:"rollback,omitempty"\`` to `provision.Generation`. Additive;
older readers ignore it; `SchemaVersion` stays `"1.0"`.

## Separation of concerns (inviolable)

`rollback` operates on the package generation only. It never imports `internal/restore`, never
reads/writes `state/backups/`, and never touches the revert journal. Asserted as an **ADDED**
requirement in the `separation-of-concerns` capability ("Rollback operates on packages only") —
an ADDED (not MODIFIED) requirement so it composes with `provisioning-generation`'s concurrent
MODIFY of *Distinct Pipeline Stages* without an archive-order conflict — and enforced by a code
guard test (`TestRollbackStaysPackageOnly`) that scans `rollback.go`'s imports for
`internal/restore` (mirroring the Phase-2 `internal/provision` guard).

## Risks / limitations (documented, not blockers)

- A target generation written before this change has `Native` set (Nix path) — older `winget`
  generations have `Native=""` and are correctly rejected with `GENERATION_NOT_FOUND` on Windows
  (which also has no realizer, so it never reaches that branch in this phase).
- `nix profile wipe-history` (or GC) can remove the Nix version a generation's `Native` points
  at; rollback to such a target fails at the backend and surfaces as `ROLLBACK_FAILED` with raw
  text in detail. Not introduced here; documented.

## Migration

Purely additive. No manifest/envelope schema bump. Existing state, generations, and Windows
behavior unchanged.
