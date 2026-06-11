> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships, no installer is run. "Done" items are the comparison/scoping work completed in
> `design.md`; "open" items are the decisions the human makes on review before any implementation
> proposal is opened.

> CLOSED 2026-06-11: all §2 decisions were ratified 2026-06-03 and confirmed by the shipped
> implementation (`engine-backend-bootstrap-impl`, #125; contract follow-ups #127). This change's
> sketched delta spec is superseded by the impl change's delta, which graduates into the
> `engine-backend-bootstrap` main spec — so this change archives without a spec sync (--skip-specs).
> §4 non-tasks stay unchecked; the assisted-uninstall open decision (2.4/4.5) is recorded as deferred
> scope in docs/roadmap/roadmap.md.

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

- [x] 2.1 **Decide phase/sequencing.** RATIFIED: fast-follow — the bootstrap shipped as its own arc
      (#125) after the brew lanes were proven (#117/#121).
- [x] 2.2 **Decide one-consent vs per-backend.** RATIFIED: one combined consent over the needed set.
- [x] 2.3 **Decide the non-interactive default.** RATIFIED: skip-the-lane-with-message;
      `--bootstrap-backends` / `--no-bootstrap` flag shape shipped as designed.
- [x] 2.4 **Decide the uninstall posture.** RATIFIED: never silently uninstall; the engine stays
      hands-off today — an *assisted* uninstall remains an open deferred decision (see roadmap).
- [x] 2.5 **Decide the Nix install flavor.** RATIFIED: the Determinate multi-user installer; no
      single-user/no-daemon option offered.
- [x] 2.6 **Decide consent disclosure depth.** RATIFIED: plain-language message (no product names)
      plus an inspectable details field carrying the exact installer commands.

## 3. Spec hardening (open — before implementation)

- [x] 3.1 **Spec the consent contract** — graduated via the impl change's delta ("A missing backend is
      bootstrapped only with explicit consent").
- [x] 3.2 **Spec the verify gate** — graduated via the impl delta ("A bootstrapped backend is verified
      working before use").
- [x] 3.3 **Spec official-installer-only** — graduated via the impl delta ("The engine orchestrates the
      official installer, never a vendored fork").
- [x] 3.4 **Spec the Nix footprint + no-silent-uninstall** — graduated via the impl delta ("...heavier
      footprint and is never silently removed", Windows-exempt scenario included).
- [x] 3.5 Graduate into an implementation proposal — shipped as `engine-backend-bootstrap-impl` (#125),
      coordinated with the brew §8 graduation as planned.

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
