# Tasks — engine-backend-bootstrap-impl (one arc, three stacked PRs)

> Implementation change. TDD RED→GREEN. No real installer runs in `go test`. Verification each PR:
> `cd go-engine && go test ./...` + `go vet ./...` + `GOOS=windows go build ./...` +
> `openspec validate --all --strict --no-interactive`.

## PR 1 — shared contract package + Homebrew instance

- [x] 1.1 New `go-engine/internal/bootstrap/` package: `Backend` (`BackendBrew`/`BackendNix`),
      `Bootstrapper{Detect, Install, Verify}` seams, `Probe(needed) → (absent, present)`,
      `Provision(absent) → map[Backend]Outcome` (`Installed`/`InstallFailed`/`VerifyFailed`).
- [x] 1.2 Real `Detect` (both backends): brew → `exec.LookPath("brew")` + known prefixes; nix →
      `exec.LookPath("nix")` + the Determinate default path.
- [x] 1.3 Real `Install` (both backends; NEVER run in tests): brew → upstream `install.sh`; nix →
      the Determinate installer. OS credential/Xcode-CLT prompts passed through, never suppressed.
- [x] 1.4 Real `Verify` (both backends): `brew --version` / `nix --version`.
- [x] 1.5 Hermetic `bootstrap` tests with fake `Detect`/`Install`/`Verify`: present→no-op,
      absent+granted→install→verify→available, absent+declined→skip, installer-ok-but-verify-fail→
      unavailable, multiple backends independent; assert no real installer is invoked.
- [x] 1.6 `events`: add `ConsentEvent` to `types.go` + `EmitConsent` to `emitter.go` (no-op when
      disabled), carrying the combined backend set, the plain-language message, and inspectable
      details. Tests for the emitted shape.
- [x] 1.7 New `go-engine/internal/commands/backend_bootstrap.go` (bootstrap.go was taken by the
      `endstate bootstrap` self-install command): `bootstrapBackendsFn` seam, `bootstrapConsent`,
      `bootstrappableOn`, `realEnsureBackends(needed, mutating, consent, emitter) → (available map,
      *envelope.Error)`. Install path is apply-only (`mutating=true`); read-only → no install, no consent.
- [x] 1.8 Default present/available no-op fake for `bootstrapBackendsFn` in `commands/main_test.go`,
      so every existing command test stays byte-identical.
- [x] 1.9 `ApplyFlags` gains `BootstrapBackends`/`NoBootstrap`; flag-parse loop + usage in
      `cmd/endstate/main.go`; pass-through at the `RunApply` call site.
- [x] 1.10 Wire the **brew** lane: gate `newBrewDriverFn()` resolution in `apply.go` through
      `bootstrapBackendsFn([BackendBrew], mutating=true, …)`. Absent+declined → `brewDrv` nil →
      existing visible-skip. Present / no-brew-manifest → byte-identical to today.
- [x] 1.11 `commands` wiring tests: declined → brew lane skipped + run continues (one nix generation,
      brew factory never resolved); realEnsureBackends branch tests + the consent event with the
      combined set + inspectable details.
- [x] 1.12 Delta spec (`specs/engine-backend-bootstrap-impl/spec.md`) for the four PR-1 requirements;
      `openspec validate --all --strict --no-interactive` passes.

> NOTE: documenting the new `consent` event type in `docs/contracts/event-contract.md` is a
> maintainer-gated follow-up (protected area). The event type is a contract-sanctioned non-breaking
> addition (no version bump); it is implemented in code but not yet listed in the contract doc.

## PR 2 — Nix instance + declined-lane restructuring

- [x] 2.1 Wire the **realizer** lane through the bootstrap pre-step on **apply** with ONE combined
      consent over the needed set (`neededBackends`: Nix when restApps or a config stage; brew when
      brew apps). Route on `nixNeeded && avail[Nix]`. (Read-only commands verify/plan/capture keep
      today's behavior — they never install, and surface REALIZER_UNAVAILABLE when Nix is absent; a
      graceful read-only skip is a deferred follow-up, NOT a regression.)
- [x] 2.2 New `runApplyBrewOnly` (apply_brew_only.go) + `configStageApplies`: a declined/unavailable
      Nix with a consented brew lane still installs brew apps (realizer-lane apps → visible skip,
      manual apps still evaluated); Nix-only-and-declined → all skipped, no crash, no generation. Keep
      `runApplyRealizer`'s present-Nix path byte-identical (all existing brew tests have a nix app →
      still routed to runApplyRealizer).
- [x] 2.3 Add the **Nix footprint + no-silent-uninstall + Windows-exempt** requirement to the delta
      spec, with the declined-Nix-but-consented-brew scenario.

## PR 3 — brew-default-for-apps routing flip

- [x] 3.1 Flip routing: a `cask:` darwin ref auto-routes to the brew lane WITHOUT requiring
      `driver: "brew"` (the cask: prefix is the unambiguous GUI-app signal). `partitionBrewLane` routes
      on `driver:"brew"` OR `isCaskRef(refs["darwin"])`; the manifest validator no longer rejects a
      cask: ref without driver:brew (the realizer-never-sees-cask invariant is now upheld by routing).
      Bare darwin refs without a driver stay on the realizer (default lane unchanged). Table tests +
      inverted validator tests.
- [x] 3.2 Add the Cask-auto-routing requirement to the delta spec. It SUPERSEDES `macos-brew-driver`'s
      "A Cask reference without the brew driver is rejected" scenario — reconciled into
      `macos-brew-driver` when that change graduates (maintainer-coordinated).

## Non-tasks (out of scope for this change)

- [ ] N.1 Interactive CLI stdin consent prompt (the GUI event path is the primary audience).
- [ ] N.2 An assisted backend **uninstall** flow (never silently uninstall; point at the official
      uninstaller only).
- [ ] N.3 A Windows bootstrap (winget ships with the OS).
- [ ] N.4 Vendoring or forking any installer (the engine orchestrates the official installer only).
