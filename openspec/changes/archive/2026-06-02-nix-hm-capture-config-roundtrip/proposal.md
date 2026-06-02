## Why

Two home-manager features landed together: **config capture** (#86 — capture emits the activated
flake recovered from the Provisioning Generation) and the **config-file wrapper** (#87 — a manifest
can declare `homeManager.config`, a path to a `home.nix`, which the engine wraps into a generated,
pinned flake under `state/` and activates).

They compose with a gap. When a machine is configured via `homeManager.config`, apply records the
**engine-generated** flakeref (a machine-local `state/home-manager/<user>#<user>` path, regenerated
each apply) in `provision.HomeGenRef.Flake`. Capture (#86) then emits that as `homeManager.flake` —
which **does not round-trip**: the path is ephemeral and machine-local, so applying the captured
manifest on another machine points at a flake that doesn't exist. The `homeManager.flake` case
(#81) round-trips fine; only the new `config` case is affected.

The fix: capture must emit the **originally declared** input — `homeManager.config` for a
config-declared apply, `homeManager.flake` for a flake-declared one — not the engine's generated
artifact.

## What Changes

- **Record the declared input.** `provision.HomeGenRef` gains a `Config` field. When apply activates a
  `homeManager.config` (the generated-flake path), it records the user's original `config` value
  alongside the activated `Flake` (kept for audit). A direct `homeManager.flake` apply records only
  `Flake` (unchanged).
- **Capture emits the declared input.** `recoverHomeManager` emits `homeManager.config` when the
  most-recent config generation recorded a `Config`, else `homeManager.flake`. So a config-applied
  machine round-trips to its `home.nix`, and a flake-applied machine round-trips to its flake.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `nix-home-manager-capture`: capture now preserves the originally-declared home-manager input
  (`config` or `flake`) rather than emitting an engine-generated flakeref for `config`-declared
  applies — so both input forms round-trip through the apply config stage.

## Impact

- `internal/provision/provision.go` — add `Config string` (omitempty) to `HomeGenRef`.
- `internal/commands/apply_realizer.go` — record `homeRef.Config = mf.HomeManager.Config` when the
  config stage activated a generated (config-derived) flake.
- `internal/commands/capture_realizer.go` — `recoverHomeManager` prefers a recorded `Config` over the
  generated `Flake`.
- Tests: `apply_realizer_test.go` (config-apply records `Config`), `capture_realizer_test.go`
  (capture emits `homeManager.config`; flake case unchanged).
- **Zero regression to the flake path** (#81/#86) and the winget/package paths. Realizer-only.
