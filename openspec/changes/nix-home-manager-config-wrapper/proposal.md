## Why

The home-manager config stage (shipped: `nix-home-manager-config`) has one input — `homeManager.flake`, a full flakeref. To use it a user must hand-write a flake: inputs, `homeManagerConfiguration`, pinning, the activation wiring. That is exactly the Nix that blocks adoption. This change makes an existing home-manager **config** first-class: the user supplies only a `home.nix` (their `programs.*` declarations) and the engine generates the surrounding flake itself. The hard Nix (flakes/inputs/pins/identity/activation) becomes invisible; the user writes config, not plumbing. The generated artifact stays **inspectable and ejectable** so power users can see and own exactly what was produced — invisible by default, transparent on demand.

## What Changes

- New manifest input **`homeManager.config`** — a path to a `home.nix` config file (resolved relative to the manifest). Mutually exclusive with `homeManager.flake`.
- When `homeManager.config` is set, the realizer config stage **generates a flake** wrapping that file — pinned `nixpkgs` + `home-manager` (the existing `ENDSTATE_NIXPKGS_PIN` / `ENDSTATE_HOME_MANAGER_PIN`), engine-injected `home.username` / `homeDirectory` / `stateVersion`, written to a stable engine-state location in plain, readable form — and feeds the resulting flakeref to the **existing** activation (no new activation path, no new flag).
- **Inspectable, not hidden:** the generated flake is discoverable + ejectable; `apply --dry-run` reports the generated location and what would activate, without activating; the Provisioning Generation records the activated config. Raw Nix stays in `error.detail`.
- Back-compat: `homeManager.flake` behavior is unchanged.

## Capabilities

### Modified Capabilities

- `nix-home-manager-config`: adds a **config-file input** the engine wraps into a flake (so the user supplies only a `home.nix`), plus an **inspectability guarantee** for engine-generated configuration. Additive — `homeManager.flake` and a default (no-config) apply are unchanged.

## Impact

- `internal/manifest/types.go` — add `Config` to `HomeManagerConfig`; mutual-exclusion validation (`config` XOR `flake`).
- `internal/realizer/nix/` — a flake generator: (`home.nix` path + identity + pins) → a written, readable flake; returns the flakeref.
- `internal/commands/apply_realizer.go` — config-file branch: generate → activate via the existing `ActivateHome`; `--dry-run` reveal.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (additive)**: document `homeManager.config`, the mutual exclusion, and the generated-flake inspectability / dry-run reveal.

## Non-Goals (deferred)

- **Config capture** (machine → `home.nix`) and the **Endstate module catalog** — separate sub-projects.
- The **zero-Nix native schema/catalog** (declare in Endstate's own format → engine writes the `home.nix`). The user still writes home-manager config attributes here; this is the home-manager-native (power-user) tier, and the foundation the catalog later generates *into*.
- **Multiple modules / a config directory** — v1 is a single `home.nix`.
