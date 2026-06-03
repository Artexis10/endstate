## Why

`homeManager.settings` now has a curated set of nine concepts (`git`, `shell`, `direnv`, `starship`,
`fzf`, `zoxide`, `bat`, `tmux`, `ssh`). Each curated concept maps to a **stable** home-manager option,
insulating the user from option renames that would silently break a raw `programs` passthrough entry.

A common developer shell still lacks coverage for four widely-used tools: `eza` (a modern `ls`
replacement), `gh` (GitHub CLI), `lazygit` (a terminal UI for git), and `neovim` (the modal editor).
Users who declare these today must drop to the raw `programs` passthrough and lose the rename-insulation
the curated layer exists to provide. Extending the curated set is the natural next step of the catalog arc.

## What Changes

- **Add four curated concepts**, each mapped to a stable home-manager surface:
  - `eza` → `programs.eza.enable` + `programs.eza.extraOptions` (a `[]string` of raw CLI flags —
    the stable surface that avoids binding to per-feature home-manager option names that churn).
  - `gh` → `programs.gh.enable` + `programs.gh.settings` (a raw attrset passthrough — gh's own
    config key namespace, which is stable because it mirrors the gh CLI's config format directly).
  - `lazygit` → `programs.lazygit.enable` + `programs.lazygit.settings` (a raw attrset passthrough —
    lazygit's own config structure, stable for the same reason as gh).
  - `neovim` → `programs.neovim.enable` + `programs.neovim.extraConfig` (raw vimscript/lua string —
    the stable `extraConfig` surface, exactly like `tmux`/`ssh`, insulating from per-option renames).
- **Each concept is a typed field** on `HomeManagerSettings` with its own small struct, so the existing
  `UnmarshalJSON` + `DisallowUnknownFields` guard catches mistyped sub-keys at load.
- **Each concept registers in `curatedPrograms`**, so a raw `programs.<name>` passthrough that targets
  the same name conflicts loudly rather than producing a Nix double-definition.
- **`eza.extraOptions`** requires a small `stringSliceToAny` helper in `home_catalog.go` (next to the
  existing `stringMapToAny`) so `nixValue` renders the `[]string` as a Nix list literal.
- **`gh.settings` / `lazygit.settings`** are `map[string]any` (not `map[string]string`) because
  lazygit's config is deeply nested — `nixValue` already handles arbitrarily nested maps.
- **Example manifest** under `manifests/examples/home-manager-settings.jsonc` gains `eza` + `neovim`
  entries. The existing raw `programs.htop` passthrough demo is left intact (htop is NOT curated).

## Capabilities

### New Capabilities

- `nix-home-manager-catalog`: the curated catalog accepts four additional concepts (`eza`, `gh`,
  `lazygit`, `neovim`), each mapped to a stable home-manager option, so a user declaring these in
  Endstate's format is insulated from home-manager option renames without dropping to the raw passthrough.
  Mistyped sub-keys are rejected at load; a raw passthrough colliding with a curated name is a clear error.

### Modified Capabilities

- None. Purely additive to the existing catalog compile path; all nine prior curated concepts, the raw
  passthrough, and the files map are unchanged.

## Impact

- `internal/manifest/types.go` — `HomeManagerSettings` gains `Eza` (`*EzaSettings`), `Gh`
  (`*GhSettings`), `Lazygit` (`*LazygitSettings`), `Neovim` (`*NeovimSettings`); four small typed
  structs added.
- `internal/realizer/nix/home_catalog.go` — `curatedPrograms` gains `eza`/`gh`/`lazygit`/`neovim`;
  `CompileHomeNix` renders each new concept; `stringSliceToAny` helper added.
- `internal/manifest/home_settings_test.go` — load + unknown-sub-key rejection tests for the new
  concepts.
- `internal/realizer/nix/home_catalog_test.go` — render + raw-overlap conflict tests for all four new
  concepts.
- `manifests/examples/home-manager-settings.jsonc` — gains `eza` + `neovim` curated entries.
- Zero winget / Windows / package-capture regression; the catalog path is Nix-only.
