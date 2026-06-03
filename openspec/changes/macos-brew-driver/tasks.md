> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships, no dependency is added. "Done" items are the comparison/scoping work completed in
> `design.md`; "open" items are the decisions the human makes on review before any implementation
> proposal is opened.

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

- [ ] 2.1 **Decide the ref scheme.** Confirm the `cask:` prefix (Scheme A) over a typed `darwinKind`
      field (Scheme B), or choose B for validatability.
- [ ] 2.2 **Decide the routing default.** Confirm "realizer is the darwin default; `driver: "brew"`
      opts in per app," or whether brew should be the default for *apps* (realizer only for config).
- [ ] 2.3 **Decide the no-Nix adoption bar.** Whether a brew-only manifest must be installable on a Mac
      without Nix, and whether that lands in Phase 1 or Phase 3 (it makes the realizer optional when no
      Nix/config work is declared).
- [ ] 2.4 **Decide capture attribution** for a package present in both Nix and brew, and confirm
      "provisioning history first, then `brew leaves`/`brew list --cask`."
- [ ] 2.5 **Decide version strictness.** Accept brew's weak/advisory pin, or require precise pinning
      (and reject formulae the tap does not version), matching the winget unavailable-pin-is-failure
      rule.
- [ ] 2.6 **Confirm non-destructive cask uninstall** (non-`--zap`) for best-effort rollback.
- [ ] 2.7 **Decide the routing default through the non-technical lens** — realizer-default+brew-opt-in
      (conservative), brew-default-for-apps+nix-for-config (audience-aligned), or the GUI-default middle
      path (§3 / Open Question 2).
- [ ] 2.8 **Decide the invisible Homebrew bootstrap** — confirm detect→consent→install→verify, the
      one-prompt consent model, and whether it lands in Phase 1 or a fast-follow (§8 / Open Question 7).

## 3. Spec hardening (open — before implementation)

- [ ] 3.1 **Spec the formula/Cask install** as a testable requirement (a `cask:` ref installs a Cask;
      a bare ref installs a formula).
- [ ] 3.2 **Spec capture of both kinds** (formulae via `brew leaves`, casks via `brew list --cask`,
      versions best-effort).
- [ ] 3.3 **Spec brew as best-effort** (no whole-set generation, no native rollback; rollback reuses
      the winget best-effort-uninstall pattern).
- [ ] 3.4 **Spec the darwin per-app routing** (realizer default, `driver: "brew"` opt-in, two-lane
      coexistence; conflicting `cask:` ref without brew driver fails loudly).
- [ ] 3.5 Graduate the ratified subset of `specs/macos-brew-driver/spec.md` into an implementation
      proposal (separate, non-design-only change).
- [ ] 3.6 **Spec the bootstrap** (absent → consent → official install → verify; decline → skip + continue;
      failure → clear error; present → no-op) — sketched in `spec.md`, harden on ratification.

## 4. Non-tasks (explicitly out of scope here)

- [ ] 4.1 (NOT in this change) Any Go / engine implementation — no `internal/driver/brew/` package,
      no `select.go` edit, no apply/capture/plan/verify two-lane wiring.
- [ ] 4.2 (NOT in this change) Adding any dependency, tap, or `brew bundle` / `mas` integration.
- [ ] 4.3 (NOT in this change) Adding a real-brew smoke step to `nix-integration.yml` — recorded as the
      implementation's vehicle, but the workflow is a protected area and is not touched here.
- [ ] 4.4 (NOT in this change) Linuxbrew — brew is scoped as the macOS driver only.
