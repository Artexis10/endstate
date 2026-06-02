## Why

PR #91 added `homeManager.settings` — the user declares configuration in Endstate's own catalog
format and the engine compiles a `home.nix`, wraps it into a generated flake, and activates it.
This closes the **apply** direction for catalog-declared config. However, `capture` does not yet
record catalog settings: it only recovers a `homeManager.config` path (from PR #89) or a
`homeManager.flake` ref — neither is the right output for a settings-applied machine.

The gap: when a user applied via `homeManager.settings`, ran `capture`, and took the captured
manifest to a new machine, the manifest would have *no* `homeManager` block at all (because no
`Config` and no portable `Flake` was recorded). The catalog apply↔capture loop is open.

This change closes it: `capture` recovers the user's declared `homeManager.settings` from the
engine's provisioning history and emits it into the captured manifest, so a captured manifest
round-trips through the #91 apply settings stage.

**The settings are recovered from the engine, not the system.** Home-manager does not persist the
originating catalog declaration in a live install. However, Endstate's `apply` already records what
it activated: the provisioning generation carries `HomeGenRef`. Capture reads that history.

## What Changes

- **Extend `provision.HomeGenRef` with a `Settings` field.** When a settings-applied configuration
  is activated, the engine records the user's declared `*manifest.HomeManagerSettings` in
  `HomeGenRef.Settings`. This is the only automatable source for round-trip capture.
- **Record `Settings` in `apply`.** The settings branch of `resolveHomeFlake` /
  `runApplyRealizer` records `homeRef.Settings = mf.HomeManager.Settings` when the catalog path
  activates. Flake and Config continue to be recorded as before (audit trail unchanged).
- **Emit `settings` in `capture`.** `recoverHomeManager` gains a third preference:
  Settings > Config > Flake. When the most-recent activated generation has Settings set, the
  captured manifest carries `homeManager: { settings: ... }` — a valid manifest the #91 apply
  settings path consumes unchanged.
- **Best-effort and non-destructive.** Exactly like the Config preference added by #89, a history
  read error or absent Settings simply falls through to Config, then Flake, then nil. Capture never
  fails because of this.

## Capabilities

### New Capabilities

- `nix-home-manager-catalog-capture`: `capture` records the user's declared `homeManager.settings`
  (from the engine's provisioning history) and emits it into the manifest, so a settings-applied
  machine round-trips through the #91 apply catalog stage. Only settings applied through Endstate
  are captured; a manual home-manager setup yields nothing until declared + applied once.

### Modified Capabilities

- None. Additive extension of the existing capture→apply loop.

## Impact

- `internal/provision/provision.go` — `HomeGenRef` gains `Settings *manifest.HomeManagerSettings`.
  Import of `internal/manifest` is safe: `manifest` is a stdlib-only leaf with no endstate deps
  (verified by `go list -deps`); the existing guard test checks only `internal/restore`.
- `internal/commands/apply_realizer.go` — settings branch records `homeRef.Settings`.
- `internal/commands/capture_realizer.go` — `recoverHomeManager` prefers Settings > Config > Flake.
- `internal/commands/capture_realizer_test.go` — new hermetic tests for the settings preference.
- `internal/provision/provision_test.go` — new round-trip test for `HomeGenRef.Settings`.
- Zero winget / Windows / package-capture regression.
