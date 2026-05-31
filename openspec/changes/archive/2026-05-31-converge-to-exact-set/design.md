## Context

Phases 1–4 shipped: Nix/winget install, Provisioning Generation + `generations`, native Unix
rollback (P3), best-effort winget rollback (P4). `apply` adds only; it never removes undeclared
packages. The realizer interface is `Name`/`Current`/`Plan(desired)→Diff{ToAdd,Present}`/
`Realize(toAdd)` — there is **no removal** path, and `Diff` has no `ToRemove`. The Nix backend
installs into an **isolated, Endstate-managed profile** (`$XDG_STATE_HOME/endstate/nix-profile`),
so the profile's contents are entirely engine-owned.

## Goals / Non-Goals

**Goals:**
- Opt-in `apply --prune` that converges the package set to exactly the manifest by removing drift.
- Unix-first via `nix profile remove` (atomic). `--confirm`-gated; `--dry-run` preview.
- Reuse `Generation.RemovedRefs` (Phase 4) to record what was pruned.
- Zero default/Windows regression.

**Non-Goals:**
- **No winget convergence** — refuse (`CONVERGENCE_UNSUPPORTED`). winget operates on the shared
  system with untracked dependencies; convergence there is riskier future work.
- **No config involvement** — package-stage only.
- **No removal of non-engine packages** — only the Endstate-managed profile is touched.

## Decisions (maintainer, this session)

- **(a) Surface = flag on `apply` (`--prune`).** Convergence is manifest-relative and `apply`
  already loads the manifest and computes the diff; `--prune` enables the removal half so `apply`
  becomes truly declarative ("make reality match exactly").
- **(b) Non-realizer backends hard-refuse** (`CONVERGENCE_UNSUPPORTED`) — explicit, mirrors P4's
  `ROLLBACK_UNSUPPORTED`.
- **(c) `--confirm` required** (default refuses); `--dry-run` previews. Honors
  `non-destructive-defaults`.

## Design

### Drift computation

`desired` = the manifest's host installables (as `apply_realizer` already builds). After install,
read `Current()`. **Drift** = every `Current().Elements` entry whose name/attrPath leaf matches no
`desired` ref leaf (reuse `leafAttr`/`presentInSet` matching, inverted). Those element names are
the prune set. Manual apps and non-host refs are not in the Nix profile, so they are unaffected.

### Removal — `realizer.Pruner` optional interface

```go
// realizer
type Pruner interface { Remove(names []string) (Result, error) }
```
Discovered by type-assertion (like `Rollbacker`/`Uninstaller`). Nix implements via
`nix profile remove <name>...` (atomic; advances a profile generation; classified through the
existing `classify` anchor path). If the realizer is not a `Pruner` → `CONVERGENCE_UNSUPPORTED`.
Winget path (driver) → `CONVERGENCE_UNSUPPORTED` before doing anything.

### Flow (apply_realizer, prune phase after install)

1. Install phase runs as today (ToAdd).
2. If `flags.Prune`:
   - If `--dry-run`: include the computed prune set in the result; remove nothing.
   - Else if `!--confirm`: refuse with a clear message (install results stand; nothing removed).
   - Else: compute drift from a fresh `Current()`, `Remove(drift)`. On failure: systemic
     (`isSystemic`) → top-level envelope error; else `INSTALL_FAILED` with raw text in detail.
3. Generation: write when `len(added) > 0 || len(removed) > 0`; `AddedRefs`=installed-this-run,
   `RemovedRefs`=pruned-this-run, `Native`=final nix generation. (Install + remove may advance
   the Nix profile twice; the engine still records one generation per apply.)

### Error codes

Add `CONVERGENCE_UNSUPPORTED` (non-realizer / non-Pruner backend asked to prune). Removal-step
failures reuse the realizer classification (`REALIZER_UNAVAILABLE`/`PERMISSION_DENIED` systemic;
else `INSTALL_FAILED`). No other new codes.

### Ordering note

Install-then-prune (not prune-then-install): never remove before the desired set is in place.
Both orders reach the same end state; install-first is safer on partial failure.

## Separation of concerns

Package-stage only. The prune phase touches the realizer + `internal/provision` only — never
restore, `state/backups/`, or the revert journal. Covered by the existing install-stage
separation invariants; no `separation-of-concerns` delta needed.

## Risks / limitations

- Two Nix generation advances per `apply --prune` (add, then remove) but one engine Provisioning
  Generation — `Native` records the final nix gen. Documented.
- A package shared between the old and new manifest is declared in both → not drift → kept. Good.
- winget convergence deferred (shared-system orphan/dependency risk) — refused, not silently
  skipped.

## Migration

Additive. No manifest/envelope schema bump (reuses `RemovedRefs`). Default `apply` unchanged.
