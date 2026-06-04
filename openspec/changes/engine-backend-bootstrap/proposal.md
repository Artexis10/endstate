## Why

Endstate's promise to a non-technical user is "rebuild my machine without learning anything." On
Windows that already holds: `winget` ships with the OS, so the package backend is just *there*. On
**macOS and Linux it does not hold**, because Endstate's backends — the **Nix realizer** and (per the
`macos-brew-driver` design) the **Homebrew driver** — are not preinstalled. Today a missing backend is
a hard stop: the realizer returns `REALIZER_UNAVAILABLE` when Nix is absent, and a brew driver would
fail the same way when `brew` is not on `PATH`. For the audience Endstate is *for*, "go install Nix /
Homebrew first" is exactly the wall this product exists to remove.

The governing principle is **invisible but inspectable**: the user should never have to learn the
words "Nix" or "Homebrew," yet every privileged, system-modifying step must stay explicit, consented,
and auditable. "Invisible" means the *concepts* are hidden — **not** that consent is hidden. On macOS
in particular the backend installers force prompts that cannot (and must not) be suppressed: the
Homebrew installer triggers an Xcode Command Line Tools install and a sudo password prompt; the
Determinate Nix installer is a privileged, multi-user, daemon-and-volume system change. So the honest
shape is **one plain-language consent**, then the **official** upstream installer, then a **verify**,
then proceed — or **decline gracefully** and continue the rest of the run without that backend.

The `macos-brew-driver` design already sketched the **Homebrew half** of this (its §8 / Requirement
"Homebrew is bootstrapped when absent, with consent"). But that is only half the story: the engine has
**two** backends to bootstrap on Unix, and the **Nix half is heavier and broader** (root/daemon,
multi-user, a dedicated macOS APFS volume, and the thorny "do we ever uninstall a backend we
installed?" question). This change scopes — **design only, no implementation** — the **unified
"engine installs its own backends" capability**: the backend-agnostic consent/verify/decline contract
that both bootstraps share, plus the Nix-specific concerns the brew §8 sketch does not cover.

## What Changes

This is a **design-only** OpenSpec change. It produces the comparison, the recommendation, and a
delta-spec **sketch**; it changes **no Go code, no engine behavior, and no installer is run**.

The direction it proposes (for the human to ratify):

- **ADOPT a single, backend-agnostic bootstrap contract** — *detect → consent → official install →
  verify → proceed-or-decline-gracefully* — that the Nix realizer and the Homebrew driver both satisfy.
  A backend that is present and working is a silent no-op (no prompt). A backend that is absent is
  installed **only after one explicit, plain-language consent**, **never silently**. A declined consent
  **skips that backend's lane** with a clear message and **continues the rest of the run**. A bootstrap
  that fails surfaces a clear error and is treated as **unavailable**, never as silently half-done.
- **Orchestrate the OFFICIAL upstream installer, never a vendored fork.** Nix → the **Determinate Nix
  installer** (the same one `.github/workflows/nix-integration.yml` already uses in CI). Homebrew → the
  upstream `install.sh`. The engine *drives* the installer; it does not embed, fork, or re-implement it.
  The step is inspectable (the user can see exactly what will run), consistent with Endstate's
  non-destructive / no-silent-mutation posture.
- **Scope the Nix-specific footprint** the brew §8 sketch does not: Nix's bootstrap is a privileged
  multi-user install (root, a launchd/systemd daemon, and on macOS a dedicated APFS **Nix Store
  volume**). The engine treats **uninstalling a backend it installed as a separate, explicit,
  user-owned action** — it does **not** silently remove a backend, consistent with non-destructive
  defaults.
- **Record the platform asymmetry.** Windows needs **no** backend bootstrap (winget ships with the OS).
  This capability is a **macOS + Linux** concern.

No backend, installer flag, or final consent-UX is *selected* by this change. The recommendation (see
`design.md`) is a starting position; the open decisions are left explicit for review. The
**Homebrew-specific** bootstrap requirement remains owned by `macos-brew-driver` §8 and graduates from
there; this capability does **not** duplicate it — it provides the shared contract the brew bootstrap
is one instance of, plus the Nix instance.

## Capabilities

### New Capabilities

- `engine-backend-bootstrap`: A capability, specified here at requirement level only (no
  implementation), under which Endstate installs **its own** package backend when it is absent —
  consented, via the official installer, verified before use, and gracefully skippable. The
  load-bearing requirements (sketched in `specs/engine-backend-bootstrap/spec.md`):
  - **A missing backend is bootstrapped only with explicit consent** — present → no-op; absent → one
    plain-language consent before any install; declined → that backend's lane is skipped and the run
    continues; never silent.
  - **A bootstrapped backend is verified working before use** — post-install the engine confirms the
    backend works (e.g. `brew --version`, a Nix eval) before provisioning through it; a backend that
    installs but fails verification is treated as unavailable, not half-used.
  - **The engine orchestrates the official installer, never a vendored fork** — Determinate (Nix) /
    upstream `install.sh` (Homebrew); the privileged step is explicit and inspectable.
  - **Nix backend bootstrap accounts for its heavier footprint** — multi-user daemon + macOS APFS
    volume + root; the engine does **not** silently uninstall a backend it installed; Windows is
    exempt (winget ships with the OS).

### Modified Capabilities

- None. This change is additive and design-only. It **composes with** `macos-brew-driver` (whose §8
  sketches the Homebrew-specific bootstrap requirement) by providing the backend-agnostic contract that
  requirement is one instance of; it does not modify or duplicate it.

## Impact

**This change is design-only. It modifies no Go code and ships no behavior.** The list below is the
surface a *future* implementation would touch, recorded here so the eventual proposal is pre-scoped:

- `go-engine/internal/commands/select.go` / the backend factories — a bootstrap pre-step would run
  *before* `newRealizerFn()` / a brew-driver factory hard-fails on an absent backend, turning today's
  `REALIZER_UNAVAILABLE` hard stop into detect → consent → install → verify (or graceful decline).
- `go-engine/internal/commands/apply.go` — the bootstrap gate runs ahead of the realizer/brew lanes;
  a declined or failed bootstrap removes that lane and continues, rather than aborting `apply`.
- A new `go-engine/internal/bootstrap/` (or equivalent) package — the detect/consent/install/verify
  orchestration, one strategy per backend (Determinate Nix, Homebrew), each shelling the **official**
  installer behind the `ExecCommand` seam so it is hermetically testable.
- The consent surface — a plain-language prompt the **GUI** renders (CLI source of truth), since the
  primary audience runs the GUI; the engine emits the consent request as a streamed event.
- `.github/workflows/nix-integration.yml` — note the **CI-test wrinkle** (design.md): the GH
  `macos-latest` runner has **Homebrew preinstalled**, so the *install* path cannot be exercised there
  (detect → present → no-op only). The bootstrap's install path is validated by manual/real-machine
  runs, not the existing macOS CI smoke.
- `openspec/specs/engine-backend-bootstrap/` — the spec this sketch graduates into on adoption.
