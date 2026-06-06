## Why

On Windows Endstate's package backend ships with the OS (`winget`), so "rebuild my machine" needs
no prerequisites. On **macOS and Linux it does not**: Endstate's backends — the **Nix realizer**
(the default lane: cross-OS CLI packages + home-manager config) and the **Homebrew driver** (the
`driver: "brew"` lane: GUI apps/casks) — are **not preinstalled**. Today a missing backend is a hard
stop: the realizer surfaces `REALIZER_UNAVAILABLE` when Nix is absent, and the brew lane degrades to
visible skips when `brew` is absent. For the non-technical audience Endstate targets first, "go
install Nix / Homebrew first" is exactly the wall the product exists to remove.

This change **implements** the `engine-backend-bootstrap` capability that was scoped design-only in
the `engine-backend-bootstrap` change: the engine **detects** a needed backend, and when it is absent
on macOS/Linux, **requests one plain-language consent**, runs the **official upstream installer**,
**verifies** the backend works, then **proceeds** — or, on decline, **skips that lane with a clear
message and continues the run**. The governing principle is **invisible but inspectable**: the user
never learns the words "Nix"/"Homebrew," but every privileged step stays explicit, consented, and
auditable.

This is delivered as **one arc across three stacked PRs** under a single capability:

1. the **shared backend-agnostic contract** (detect → consent → official install → verify →
   proceed-or-decline) **plus the Homebrew instance wired into the command lanes** — this change's
   first increment;
2. the **Nix instance** + the lane restructuring that lets a declined Nix still run a consented brew
   lane;
3. the **brew-default-for-apps routing flip** (which depends on brew being reliably bootstrappable).

## What Changes

- **ADD a backend-agnostic bootstrap contract** — a new `internal/bootstrap` package implementing
  *detect → consent → official install → verify → proceed-or-decline-gracefully* behind injectable
  seams (`Detect`/`Install`/`Verify`) so it is hermetically testable. **No real installer ever runs
  in `go test`.** A present+working backend is a silent no-op (no prompt). An absent backend is
  installed only after **one explicit, combined consent**, never silently. A declined consent skips
  that backend's lane with a clear message and continues the rest of the run. A backend that installs
  but fails its verify probe is treated as **unavailable**, not used half-configured.
- **Orchestrate the OFFICIAL upstream installer, never a vendored fork** — Nix → the Determinate Nix
  installer (matching `.github/workflows/nix-integration.yml`); Homebrew → the upstream `install.sh`.
  The engine drives the installer and never suppresses the OS credential / Xcode-CLT prompts it forces.
- **Wire the consent as a flag-driven gate + streamed event** — `--bootstrap-backends` opts in,
  `--no-bootstrap` forces skip; in a non-interactive context with no consent the engine **skips the
  lane with a clear message** and emits a single combined **consent-request event** the GUI renders
  (CLI is source of truth). The install path runs only on the mutating `apply` command; read-only
  commands (`plan`/`verify`/`capture`) detect → use-or-skip without offering to install.
- **Wire the Homebrew instance** into the apply brew lane (this increment): an absent+declined brew
  backend reuses the existing visible-skip path, so a present brew backend or a no-brew manifest is
  behavior-identical to today.

Increment 2 (the Nix instance + declined-lane restructuring) and increment 3 (the brew-default
routing flip) extend this same change's spec and tasks; the change is archived after increment 3.

## Capabilities

### New Capabilities

- `engine-backend-bootstrap-impl`: Endstate installs **its own** package backend when it is absent on
  macOS/Linux — consented, via the official installer, verified before use, and gracefully skippable.
  A distinct capability id from the design-only `engine-backend-bootstrap` (which this implements) so
  `openspec validate --all --strict` does not collide while both are active.

### Modified Capabilities

- None in this increment. The **Homebrew-specific** bootstrap requirement remains owned by
  `macos-brew-driver` §8 (the brew instance here is one instance of this shared contract); this change
  does not restate it. The brew-default routing flip (increment 3) coordinates with
  `macos-brew-driver`'s per-app driver-selection requirement.

## Impact

- New `go-engine/internal/bootstrap/` — the detect/consent/install/verify orchestration, one strategy
  per backend (Determinate Nix, Homebrew), each shelling the **official** installer behind an
  injectable seam so it is hermetically testable.
- New `go-engine/internal/commands/bootstrap.go` — the `bootstrapBackendsFn` pre-step seam,
  `neededBackends`, and `ensureBackendsForRun`, run before the backend factory gate
  (`newRealizerFn`/`newBrewDriverFn`) resolves.
- `go-engine/internal/commands/apply.go` — the brew lane resolves through the bootstrap pre-step;
  a declined/failed brew bootstrap leaves the lane in its existing visible-skip state.
- `go-engine/internal/events/` — a new streamed `consent` event carrying the combined backend set,
  the plain-language message, and an inspectable details field (the exact installer commands).
- `go-engine/cmd/endstate/main.go` — `--bootstrap-backends` / `--no-bootstrap` flags.
- **CI wrinkle (recorded, not solved):** the GH `macos-latest` runner has Homebrew and (via the
  Determinate action) Nix preinstalled, so the macOS smoke can only exercise *detect → present →
  no-op*. The *absent → consent → install → verify* path is validated on a clean real machine / VM,
  out of band. Hermetic unit tests cover the contract logic with fake installers.
