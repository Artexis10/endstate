> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships, no installer is run. "Done" items are the comparison/scoping work completed in
> `design.md`; "open" items are the decisions the human makes on review before any implementation
> proposal is opened.

## 1. Scoping & comparison (done — captured in design.md)

- [x] 1.1 Frame the problem: Windows gets its backend (winget) for free; macOS/Linux do not, and a
      missing backend is a hard stop today (`REALIZER_UNAVAILABLE` / brew-absent). For a non-technical
      audience, "install the package manager first" is the wall this capability removes.
- [x] 1.2 Establish the **invisible-but-inspectable** consent model: hide the *concepts* (never the
      words "Nix"/"Homebrew"), never the *consent*; macOS forces unsuppressable Xcode-CLT/sudo/volume
      prompts, so the only honest shape is one plain-language consent → official installer → verify.
- [x] 1.3 Define the **unified backend-agnostic contract** (detect → consent → official install →
      verify → proceed-or-decline-gracefully) that both the Nix realizer and the Homebrew driver satisfy.
- [x] 1.4 Place the bootstrap as a **pre-step in front of the backend factory gate** in
      apply/capture/verify/plan; present → no-op; declined/failed → skip that lane, continue the run.
- [x] 1.5 Scope the **Nix-specific footprint** the brew §8 sketch does not cover: multi-user daemon,
      macOS APFS Nix Store volume, root, the Determinate installer as the vehicle, and the
      uninstall-the-backend question (recommend: never silently uninstall).
- [x] 1.6 Establish the boundary with `macos-brew-driver`: the Homebrew-specific bootstrap requirement
      stays in brew §8 and graduates from there; this capability owns the **shared contract** + the
      **Nix** instance, without duplicating the brew requirement.
- [x] 1.7 Record the **CI-test wrinkle**: the GH macOS runner has Homebrew (and CI-Nix) preinstalled, so
      the *install* path cannot be exercised in the existing macOS smoke (detect → present → no-op only);
      the install path needs a clean real machine.
- [x] 1.8 Record the consent-UX boundary (CLI source of truth; the GUI renders an engine-emitted consent
      request for the primary audience) and the verify-first gate.
- [x] 1.9 Sketch the delta-spec requirements in `specs/engine-backend-bootstrap/spec.md`.

## 2. Decisions (open — the human ratifies)

- [ ] 2.1 **Decide phase/sequencing.** Bootstrap ships **with** the brew increment (true "zero
      prerequisites" on day one) or as a **fast-follow** once the brew lanes are proven (§ Open Q1; mirrors
      brew Open Q7).
- [ ] 2.2 **Decide one-consent vs per-backend** on a Mac needing both Nix (config) and Homebrew (apps)
      (§ Open Q2).
- [ ] 2.3 **Decide the non-interactive default** (no TTY): skip-the-lane-with-message vs fail-loudly;
      confirm `--bootstrap-backends` / `--no-bootstrap` flag shape (§ Open Q3).
- [ ] 2.4 **Decide the uninstall posture.** Never silently uninstall a backend the engine installed;
      confirm whether the engine ever offers an *assisted* uninstall (pointing at the official
      uninstaller) or stays entirely hands-off (§ Open Q4).
- [ ] 2.5 **Decide the Nix install flavor.** Confirm the **Determinate** multi-user installer (matches
      CI, flake-enabled) over upstream/single-user; decide whether a single-user/no-daemon option is ever
      offered (§ Open Q5).
- [ ] 2.6 **Decide consent disclosure depth** — how much of the daemon/APFS-volume footprint the
      plain-language consent must disclose to stay honest without overwhelming the audience (§ Open Q6).

## 3. Spec hardening (open — before implementation)

- [ ] 3.1 **Spec the consent contract** as testable requirements (present → no-op/no-prompt; absent →
      one consent before install; declined → skip lane + continue; never silent).
- [ ] 3.2 **Spec the verify gate** (post-install verification probe gates use; verify-fail → backend
      unavailable, not half-used).
- [ ] 3.3 **Spec official-installer-only** (Determinate / upstream `install.sh`; orchestrated, not
      vendored; inspectable privileged step).
- [ ] 3.4 **Spec the Nix footprint + no-silent-uninstall** (multi-user daemon + macOS volume; the engine
      does not silently remove a backend it installed; Windows exempt).
- [ ] 3.5 Graduate the ratified subset of `specs/engine-backend-bootstrap/spec.md` into an
      implementation proposal (separate, non-design-only change), coordinated with the brew §8 graduation.

## 4. Non-tasks (explicitly out of scope here)

- [ ] 4.1 (NOT in this change) Any Go / engine implementation — no `internal/bootstrap/` package, no
      `select.go`/`apply.go` pre-step wiring, no consent-event emission.
- [ ] 4.2 (NOT in this change) Running, vendoring, or forking any installer (Determinate / Homebrew
      `install.sh`).
- [ ] 4.3 (NOT in this change) Re-specifying the **Homebrew-specific** bootstrap requirement — it stays
      in `macos-brew-driver` §8; this change provides the shared contract it is one instance of.
- [ ] 4.4 (NOT in this change) A Windows bootstrap — winget ships with the OS; the only Windows angle is
      asserting "no backend bootstrap needed."
- [ ] 4.5 (NOT in this change) An assisted backend **uninstall** flow — recorded as an open decision, not
      scoped here.
