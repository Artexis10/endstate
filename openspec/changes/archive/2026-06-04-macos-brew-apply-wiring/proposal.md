## Why

The design-only change `macos-brew-driver` scoped a macOS Homebrew driver and how the engine routes
apps between `brew` and the Nix realizer on the first OS to have **both a driver and a realizer live at
once**. The hermetic brew **driver package** then shipped (Detect/Install/Uninstall/version, all
behind an injectable `ExecCommand`). What was still missing: the brew driver did **nothing** — on
darwin `selectRealizer` wins at the apply gate, `runApplyRealizer` owns config + all apps + the
generation write + the event stream, and the winget-style driver loop below it is unreachable
(`selectBackend("darwin") → ErrNoBackend`).

This change **wires the brew driver into the pipeline** so a `driver: "brew"` app is actually
installed, captured, verified, and planned on darwin — alongside the Nix realizer, in one run. It
graduates a **subset** of the `macos-brew-driver` design into implemented behavior. Routing is explicit
`driver: "brew"` opt-in; the Nix realizer stays the darwin default, and a no-brew manifest is
byte-identical to today (a non-regression test asserts it).

Deferred (left in the design change, not graduated here): best-effort brew rollback, precise version
pinning, and the invisible Homebrew bootstrap.

## What Changes

- **Two-lane apply at the realizer gate.** Partition the synthesized app set into a brew lane
  (`driver: "brew"`) and the rest; hand the realizer a copy carrying only the rest (it never sees a
  brew/`cask:` ref). The realizer lane runs first and commits its atomic generation; the brew lane runs
  second, best-effort, interleaving its per-item events into the realizer's existing plan/apply/verify
  phases (exactly one summary per phase). A brew failure is a per-item failure that never aborts the
  Nix generation. Brew installs are recorded in a **separate** `backend: "brew"` provisioning
  generation written after the Nix one.
- **Brew install lane** installs formulae (`brew install <name>`) and Casks (`brew install --cask`),
  selected by the `cask:` ref prefix. A `driver: "brew"` app on a non-darwin host is a **visible skip**.
- **Capture lane** enumerates `brew leaves` + `brew list --cask` (+ best-effort versions) and emits
  `driver: "brew"` apps (Casks as `cask:` refs), deduped by id against the realizer-captured set. The
  captured-app shape gains a `driver` field preserved through capture and the `--update` merge so a
  brew app round-trips.
- **Verify / plan lanes** fold brew presence (and advisory version drift) into the single verify/plan
  summary.
- **`cask:` validation** at load: a `cask:` darwin ref without `driver: "brew"` is rejected
  (`CASK_REF_REQUIRES_BREW_DRIVER`); `driver: "brew"` without a darwin ref is rejected
  (`BREW_DRIVER_REQUIRES_DARWIN_REF`). Host-independent.
- **Real-macOS smoke** (`scripts/smoke/brew-realbrew-smoke.sh`) confirms brew's real-output anchors —
  the hermetic tests only lock the assumed shapes (the winget lesson) — wired into the existing
  `nix-integration.yml` macOS leg.

## Capabilities

- `macos-brew-apply-wiring` (new): two-lane apply, brew install/capture/verify/plan lanes, and the
  cask-ref validation. Stated under requirement names distinct from the still-active `macos-brew-driver`
  design change so `openspec validate --all --strict` does not collide.

## Out of Scope (deferred to the design change)

- Best-effort brew rollback (uninstall later additions).
- Precise brew version pinning (brew's pin model is weak; declared versions stay advisory).
- The invisible Homebrew bootstrap (detect → consent → install → verify).
