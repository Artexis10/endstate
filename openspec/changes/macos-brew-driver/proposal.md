## Why

On macOS, Endstate today provisions packages and home-manager config through the **Nix realizer**
(`selectRealizer("darwin")` → `nix.New()`). That is the right backend for cross-OS declarative CLI
packages and for the home-manager config story. It is the **wrong** backend for the single most
important "rebuild my Mac" use case: **native GUI apps**. Nix on macOS cannot idiomatically install
a `.app` bundle into `/Applications` (nixpkgs Darwin GUI coverage is thin and non-native), and it
forces a full, heavy Nix install on a user who may want none of it.

The native way Mac users install GUI apps is **Homebrew Casks** — `brew install --cask google-chrome`,
`slack`, `docker`, `rectangle`, `visual-studio-code`. Casks (and, for CLI tools, formulae) are the
backbone of a believable Mac rebuild. So Homebrew is **complementary to Nix, not redundant**:

- **Nix** = cross-OS declarative config + pinned CLI packages + the home-manager catalog.
- **Homebrew** = native macOS apps/casks + a **no-Nix adoption path** for users who just want their
  Mac apps back without installing Nix at all.

**Who this is for.** Endstate targets **non-technical users first** (technical users benefit too). For a
non-technical person, "rebuild my Mac" means *their actual apps* — Chrome, Zoom, Spotify, Office, Slack —
reinstalled without ever opening a terminal or learning what a package manager is. Those are GUI apps,
which Nix on macOS cannot install idiomatically and which the user will never install Nix to get.
**Homebrew Casks are the only realistic path to that promise**, which makes brew — for the primary
audience — the *core* macOS app-recovery mechanism, not a developer convenience. The backend stays
invisible (the user sees Endstate, never "brew"), and the engine even **installs Homebrew itself when it
is absent** (with consent) so "go install a package manager first" never blocks a non-technical user —
see §8 of `design.md`.

Architecturally, `brew` is the macOS analog of **winget**: a **per-package, non-atomic
`driver.Driver`**, NOT a whole-set `realizer.Realizer`. It mirrors winget's optional-interface shape
(`Uninstaller` for best-effort rollback, `VersionedInstaller` for pinning, `BatchDetector` for fast
detection) and reuses the already-specified winget best-effort-rollback pattern. This change scopes —
**design only, no implementation** — how that driver is shaped, how a manifest distinguishes a Cask
from a formula, and (the hard part) how the engine routes apps between `brew` and the Nix realizer on
the first OS to have **both a driver and a realizer live at once**.

## What Changes

This is a **design-only** OpenSpec change. It produces the comparison, the recommendation, and a
delta-spec **sketch**; it changes **no Go code, no engine behavior, and no real manifest**.

The direction it proposes (for the human to ratify) is to **ADD a `brew` Driver beside the existing
`winget` driver and `nix` realizer**, specifically:

- **Brew is a `driver.Driver`, winget's macOS sibling.** Per-package, non-atomic, best-effort. It
  implements the same optional interfaces winget does — `Uninstaller` (best-effort rollback),
  `VersionedInstaller` (pinning, with brew's weaker pin semantics noted), `BatchDetector` (fast
  presence via `brew list`) — and explicitly does **not** offer generation/native rollback (that
  stays Nix-only).
- **A manifest ref distinguishes Cask from formula.** Casks are the headline value, so the scheme
  must make them first-class. The design recommends a `cask:` prefix inside the `darwin` ref (e.g.
  `"refs": { "darwin": "cask:google-chrome" }`), with a bare `darwin` ref meaning a formula.
- **Per-app routing on darwin via the existing `driver` field.** macOS becomes the first host with
  both a realizer and a driver available. The Nix realizer stays the **default** (config + Nix
  packages); `brew` is **opt-in per app** via `"driver": "brew"`. The design specifies how
  `apply`/`capture`/`verify`/`plan` coexist when both backends are present, and how capture decides
  which backend a discovered package came from.
- **Capture from `brew leaves` / `brew list --cask`** into manifest formulae + casks (bare names,
  version via `brew list --versions`).
- **A real-macOS verification plan** that treats real `brew` output as ground truth (the winget
  lesson), wired into the existing `nix-integration.yml` macOS runner as the vehicle for a future
  real-brew smoke.

No final routing model or ref scheme is *selected* by this change. The recommendation (see
`design.md`) is a starting position; the open decisions are left explicit for review.

## Capabilities

### New Capabilities

- `macos-brew-driver`: A capability, specified here at requirement level only (no implementation),
  under which Endstate installs, captures, and verifies macOS packages through Homebrew as a
  per-package `driver.Driver` that coexists with the Nix realizer. The load-bearing requirements
  (sketched in `specs/macos-brew-driver/spec.md`):
  - **Brew installs both formulae and Casks** — a manifest `darwin` ref distinguishes the two, and
    Casks are first-class (the headline value).
  - **Capture records both formulae and Casks** as bare names with best-effort versions.
  - **Brew is best-effort, not atomic** — no whole-set generation and no native rollback; rollback is
    the winget-style best-effort uninstall pattern.
  - **Per-app driver selection routes brew vs nix on darwin** — the Nix realizer stays default;
    `"driver": "brew"` opts an app into the brew driver; the two backends coexist across
    apply/capture/verify/plan.

### Modified Capabilities

- None in this design-only change. It is additive: it introduces a new driver capability beside the
  existing `winget` driver and `nix` realizer capabilities, and ratifies how the existing
  per-app `driver` selection seam (`manifest.App.Driver` + GOOS-keyed `Refs`) extends to darwin.

## Impact

**This change is design-only. It modifies no Go code and ships no behavior.** The list below is the
surface a *future* implementation would touch, recorded here so the eventual proposal is pre-scoped:

- `go-engine/internal/driver/brew/` — a new driver package mirroring `internal/driver/winget/`:
  `Detect` / `Install` (core `Driver`), plus optional `Uninstall` (`Uninstaller`),
  `InstallVersion` / `ReinstallVersion` (`VersionedInstaller`), `DetectBatch` (`BatchDetector`), and
  `Capabilities`. Formula-vs-Cask is resolved from the ref here.
- `go-engine/internal/commands/select.go` — `selectBackend("darwin")` would return the brew driver so
  darwin has **both** a driver and a realizer; today darwin returns `ErrNoBackend` from
  `selectBackend` and `nix.New()` from `selectRealizer`. The routing rule (realizer default, brew
  per-app opt-in) is the design's hard part.
- `go-engine/internal/commands/apply.go` / `capture.go` / `plan.go` / `verify.go` — these currently
  **short-circuit to the realizer** when `newRealizerFn()` succeeds (it always does on darwin), making
  the driver loop unreachable. Co-existence requires splitting darwin apps into a realizer set and a
  brew-driver set and running both lanes.
- `go-engine/internal/commands/capture_realizer.go` / `capture.go` — capture would gain a brew lane
  (`brew leaves`, `brew list --cask`, `brew list --versions`) and a rule for which backend a
  discovered package is attributed to.
- `.github/workflows/nix-integration.yml` — the existing macOS runner is the vehicle for a future
  **real-brew smoke** (added by the implementation change, not here).
- `openspec/specs/macos-brew-driver/` — the spec this sketch graduates into on adoption.
