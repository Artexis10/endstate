## Why

The design-only change `macos-brew-driver` scoped a macOS Homebrew driver, and `macos-brew-apply-wiring`
graduated its **install / capture / verify / plan** lanes — a `driver: "brew"` app is now installed on
darwin alongside the Nix realizer and recorded in its own `backend: "brew"` provisioning generation. The
brew **driver package** has implemented `Uninstall` (non-`--zap`, already-absent tolerant) since it
shipped. What is still missing is the **command wiring**: `rollback` short-circuits to the native (Nix)
realizer on darwin (`RunRollback` calls `newRealizerFn()` first; `runRealizerRollback` owns the whole
operation), so a machine's brew-installed apps — recorded in `backend: "brew"` generations the native
rollback never touches — are **never uninstalled**. Rolling back a darwin machine silently leaves its
brew apps behind.

This change **wires best-effort brew rollback into the realizer rollback path** so that rolling back to
an explicit target generation also uninstalls the brew apps installed after it — the same two-lane
composition `apply` already uses, now for `rollback`. It graduates the **best-effort rollback** subset of
the `macos-brew-driver` design into implemented behavior, reusing the existing winget best-effort
pattern (uninstall the union of added refs of generations after the target, tolerate per-package failure,
require `--confirm`, append a rollback-marked generation).

Deferred (left in the design change, not graduated here): precise version pinning (brew's pin is weak and
already surfaced as advisory drift by verify), and the invisible Homebrew bootstrap.

## What Changes

- **Two-lane rollback at the realizer path.** When `rollback --to N` runs on a host whose realizer owns
  the native (Nix) rollback, the engine ALSO uninstalls, best-effort, the union of brew references added
  by every `backend: "brew"` provisioning generation numbered greater than `N`. The native package
  rollback runs first; the brew uninstall lane runs second and never unwinds or aborts it. A per-package
  brew failure is reported (partial) but does not fail the run while any brew uninstall succeeded.
- **Explicit target required for the brew lane.** The brew uninstall lane engages only with an explicit
  `--to N` (symmetric with `--enable-restore` config rollback). **Bare rollback** (no `--to`) stays
  native-package-only and byte-identical to today: the native "previous" anchor is generation-relative
  and cannot be reconciled with interleaved brew generations without an explicit boundary.
- **Brew-only target is valid.** A target generation that recorded no native package anchor (a
  `backend: "brew"` generation) is accepted when there are later brew refs to uninstall — the brew
  packages roll back with no native package change. This relaxes the prior "no native anchor → not found"
  rejection only for the brew-composed case.
- **Confirmation, dry-run, non-destructive.** The brew uninstalls require `--confirm` (the native
  rollback's existing gate) and are previewed under `--dry-run` without mutating. Cask uninstalls are
  non-destructive (never `--zap`). The untracked-transitive-dependency caveat is surfaced (brew's
  auto-installed dependencies are exactly such a caveat).
- **Separate brew rollback generation.** Brew uninstalls are recorded in their own `backend: "brew"`
  rollback-marked provisioning generation (RemovedRefs populated, AddedRefs empty), mirroring the apply
  path's separate brew generation and the winget best-effort rollback record.
- **Real-macOS smoke** (`scripts/smoke/brew-realbrew-smoke.sh`) is extended to apply a brew formula, then
  `rollback --to N --confirm`, and assert the formula is uninstalled — confirming brew's real uninstall
  anchors (the hermetic tests only lock the assumed shapes).

## Capabilities

- `macos-brew-best-effort-rollback` (new): two-lane rollback composing best-effort brew uninstall with
  the native realizer rollback, its confirmation/dry-run/non-destructive guarantees, and the brew-only
  target relaxation. Stated under requirement names distinct from the still-active `macos-brew-driver`
  design change and the archived `macos-brew-apply-wiring` so `openspec validate --all --strict` does not
  collide.

## Out of Scope (deferred to the design change)

- Precise brew version pinning (brew's pin model is weak; declared versions stay advisory, and verify
  already reports drift).
- The invisible Homebrew bootstrap (detect → consent → install → verify).
- Bare-rollback brew support (the brew lane requires an explicit `--to N` this change).
