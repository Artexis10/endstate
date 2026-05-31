## Why

Phases 1–4 give the engine a unified package model: Nix/winget install (P1/P2), a numbered
Provisioning Generation (P2), native Unix rollback (P3), and best-effort winget rollback (P4).
But `apply` only ever **adds** — it installs declared packages and leaves undeclared ones
untouched (`non-destructive-defaults`: "Apply does not remove unlisted packages"). So the
declarative ideal is half-complete: **drift** accumulates — packages the engine installed for a
past manifest but that are no longer declared linger forever.

This change is **Phase 5 — convergence-to-exact-set**: an **opt-in**, `--confirm`-gated
`apply --prune` that converges the package set to *exactly* the declared manifest by
uninstalling drift. **Unix-first** (the Nix realizer, via `nix profile remove`): because Nix
installs into an **Endstate-managed, isolated profile**, "drift" is unambiguous and safe to
remove — every element in that profile was put there by Endstate, so pruning only removes what
the engine itself installed and the manifest no longer declares. Non-realizer backends (winget
on Windows) **hard-refuse** with `CONVERGENCE_UNSUPPORTED` — winget operates on the shared
system with untracked dependencies, so convergence there is riskier future work.

Default `apply` behavior is **unchanged**: without `--prune` nothing is ever removed. This keeps
`non-destructive-defaults` intact and makes removal a deliberate, confirmed opt-in.

## What Changes

- Add a **`--prune`** flag to `apply`. On the realizer (Nix) path, after the install phase it
  computes **drift** = installed profile elements that match no declared host installable, and
  removes them via a new realizer removal path. Requires `--confirm`; `--dry-run` previews the
  prune set without removing anything.
- Add an optional **`realizer.Pruner`** interface (`Remove(names []string) (Result, error)`),
  discovered by type-assertion like the other optional capabilities. The Nix backend implements
  it via `nix profile remove <name>` (atomic — advances a profile generation). A realizer that
  does not implement it, or the **winget driver path**, refuses `--prune` with
  `CONVERGENCE_UNSUPPORTED`.
- The converged `apply` writes one Provisioning Generation reflecting the final set: `addedRefs`
  (installed this run) **and** `removedRefs` (pruned this run) — **reusing the
  `Generation.RemovedRefs` field added in Phase 4**. A generation is written when anything was
  added **or** removed.
- Add the additive error code **`CONVERGENCE_UNSUPPORTED`** (removal-step failures reuse the
  realizer classification: systemic `REALIZER_UNAVAILABLE`/`PERMISSION_DENIED`, else
  `INSTALL_FAILED`).
- Safety: convergence touches **only the Endstate-managed Nix profile** — never system-wide
  packages, never config (`state/backups/`, the revert journal, restore are untouched).

## Capabilities

### New Capabilities

- `converge-to-exact-set`: `apply --prune --confirm` converges the package set to exactly the
  declared manifest by uninstalling drift (installed-but-undeclared) from the engine-managed
  package set. Realizer-only (Nix today); non-realizer backends refuse. `--dry-run` previews;
  the converged apply records both added and removed refs in one Provisioning Generation.

### Modified Capabilities

- `non-destructive-defaults`: **ADDS** a requirement ("Convergence is opt-in and confirmed")
  affirming that default `apply` removes nothing and that pruning requires both `--prune` and
  `--confirm`. This is an **ADDED requirement** (a new, distinct requirement name), NOT a
  modification of the existing *No Silent Deletions* / *Destructive Operations Require Explicit
  Flags* requirements — so it composes cleanly with any concurrent change and does not weaken
  the default-safe guarantee.

## Impact

- `internal/realizer/realizer.go` — add the `Pruner` optional interface.
- `internal/realizer/nix/prune.go` (new) — `Remove(names)` via `nix profile remove`; classified
  via the existing anchor path. Real-nix removal smoke verifiable on this box.
- `internal/commands/apply_realizer.go` — a prune phase after install (gated on `--prune` +
  `--confirm`; `--dry-run` previews); drift = `Current` ∖ desired; record `removedRefs`.
- `internal/commands/apply.go` (driver path) — `--prune` → `CONVERGENCE_UNSUPPORTED`.
- `internal/commands/apply_generation.go` — write a generation when added **or** removed; carry
  `RemovedRefs`.
- `internal/envelope/errors.go` — `ErrConvergenceUnsupported = "CONVERGENCE_UNSUPPORTED"`.
- `cmd/endstate/main.go` — **PROTECTED (maintainer-approved, additive)**: parse `--prune`, pass
  it through `ApplyFlags`; usage line.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**: document
  `apply --prune` (prune actions in the result + `CONVERGENCE_UNSUPPORTED`).
- **Zero Windows / default regression**: default `apply` is byte-identical; `apply --prune` on
  Windows refuses cleanly. Proven by host-aware tests + `GOOS=windows` build/vet; real-nix prune
  smoke on this box.
