> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships, no dependency is added. "Done" items are the comparison/scoping work completed in
> `design.md`; "open" items are the decisions the human makes on review before any implementation
> proposal is opened.

> CLOSED 2026-06-11: every §2 decision was ratified by the shipped implementation (brew driver #113,
> two-lane wiring #117, best-effort rollback #121, bootstrap arc #125/#127); §3 graduated into the
> `macos-brew-apply-wiring`, `macos-brew-best-effort-rollback`, and `engine-backend-bootstrap` specs.
> At closure this change's remaining unique delta (verify no-downgrade nuance + the cask auto-route
> reconciliation foreshadowed by #127) is carried as a delta against `macos-brew-apply-wiring`.
> §4 non-tasks stay unchecked — they were declared out of scope, and the real-brew smoke (4.3) later
> shipped separately in `nix-integration.yml`.

## 1. Scoping & comparison (done — captured in design.md)

- [x] 1.1 Frame brew as **complementary to Nix, not redundant**: Nix = cross-OS config + CLI
      packages; brew = native macOS apps/casks + a no-Nix adoption path.
- [x] 1.2 Establish the driver shape: brew is a `driver.Driver` (winget's macOS sibling), with the
      same optional interfaces (`Uninstaller`, `VersionedInstaller`, `BatchDetector`); best-effort and
      non-atomic; **no** generation/native rollback (that stays Nix-only).
- [x] 1.3 Compare formula-vs-Cask ref schemes (`cask:` prefix vs typed field vs two ref keys) and
      recommend the `cask:` prefix in the darwin ref, keeping Casks first-class.
- [x] 1.4 Resolve the hard part — the darwin selection model with **both** a realizer and a driver
      live: realizer default, `driver: "brew"` opt-in, a two-lane pipeline, and the capture
      attribution rule (provisioning history → `brew leaves`/`brew list --cask`).
- [x] 1.5 Specify capture (`brew leaves` / `brew list --cask` / `brew list --versions`), verify
      (`brew list` presence + advisory version), and brew's weak pin semantics vs winget's `--version`.
- [x] 1.6 Record the real-macOS verification plan (the winget lesson) and name `nix-integration.yml`'s
      macOS runner as the vehicle for a future real-brew smoke.
- [x] 1.7 Define the phased path (P1 formulae + Casks together, P2 rollback + version, P3 no-Nix
      ergonomics) with a recommendation.
- [x] 1.8 Sketch the delta-spec requirements in `specs/macos-brew-driver/spec.md`.
- [x] 1.9 Scope the **invisible Homebrew bootstrap** (detect → consent → official install → verify →
      graceful decline) as the non-technical "zero prerequisites" path, and frame brew (for the primary,
      non-technical audience) as the core macOS app-recovery path rather than a developer convenience;
      note the parallel **Nix** bootstrap is a separate, broader capability (§8).

## 2. Decisions (open — the human ratifies)

- [x] 2.1 **Decide the ref scheme.** RATIFIED: the `cask:` prefix (Scheme A) — shipped in #113/#117.
- [x] 2.2 **Decide the routing default.** RATIFIED: realizer is the darwin default; `driver: "brew"`
      opts in per app — shipped in #117, later refined by Cask auto-routing (#125/#127).
- [x] 2.3 **Decide the no-Nix adoption bar.** RATIFIED: the needed-backend model — bootstrap consent
      covers only the backends a run actually needs (#125), so a brew-only manifest never requires Nix.
- [x] 2.4 **Decide capture attribution.** RATIFIED: `brew leaves`/`brew list --cask` with no
      duplication against realizer-captured apps; realizer apps carry no driver field — shipped in #117.
- [x] 2.5 **Decide version strictness.** RATIFIED: brew pins are advisory — verify reports drift,
      apply never downgrades/reinstalls to chase a pin (unlike winget's strict pin rule).
- [x] 2.6 **Confirm non-destructive cask uninstall** (non-`--zap`). RATIFIED — shipped in #121.
- [x] 2.7 **Decide the routing default through the non-technical lens.** RATIFIED: the middle path —
      realizer default + brew opt-in, with `cask:` references auto-routing to brew (#125/#127).
- [x] 2.8 **Decide the invisible Homebrew bootstrap.** RATIFIED: detect→consent→install→verify with
      one combined consent — shipped as a fast-follow, the engine-backend-bootstrap arc (#125).

## 3. Spec hardening (open — before implementation)

- [x] 3.1 **Spec the formula/Cask install** — graduated into `macos-brew-apply-wiring` ("Brew installs
      formulae and casks via the driver lane").
- [x] 3.2 **Spec capture of both kinds** — graduated into `macos-brew-apply-wiring` ("Capture
      enumerates brew formulae and casks into the manifest").
- [x] 3.3 **Spec brew as best-effort** — graduated into `macos-brew-best-effort-rollback`.
- [x] 3.4 **Spec the darwin per-app routing** — graduated into `macos-brew-apply-wiring` ("Two-lane
      apply..."); the "cask ref without brew driver fails loudly" half was REVERSED to auto-routing by
      the bootstrap arc (#125/#127), reconciled in this change's closing delta.
- [x] 3.5 Graduate into implementation proposals — shipped as the `macos-brew-apply-wiring`,
      `macos-brew-best-effort-rollback`, and `engine-backend-bootstrap-impl` changes.
- [x] 3.6 **Spec the bootstrap** — graduated into the `engine-backend-bootstrap` spec (#116/#125).

## 4. Non-tasks (explicitly out of scope here)

- [ ] 4.1 (NOT in this change) Any Go / engine implementation — no `internal/driver/brew/` package,
      no `select.go` edit, no apply/capture/plan/verify two-lane wiring.
- [ ] 4.2 (NOT in this change) Adding any dependency, tap, or `brew bundle` / `mas` integration.
- [ ] 4.3 (NOT in this change) Adding a real-brew smoke step to `nix-integration.yml` — recorded as the
      implementation's vehicle, but the workflow is a protected area and is not touched here.
- [ ] 4.4 (NOT in this change) Linuxbrew — brew is scoped as the macOS driver only.
