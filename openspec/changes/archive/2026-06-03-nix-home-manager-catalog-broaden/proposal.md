## Why

PR #91 introduced `homeManager.settings` — the user declares configuration in Endstate's own catalog
format and the engine compiles a `home.nix`, wraps it in a generated flake, and activates it. The
curated set shipped with four concepts (`git`, `shell`, `direnv`, `starship`) plus a raw `programs`
passthrough and a `files` map. Each curated concept is the value of the catalog: it maps to a **stable**
home-manager option, so the declaration keeps working across home-manager option renames — the raw
passthrough does not buy that insulation.

The curated set is too small to cover a common developer shell. A user who wants `fzf`, `zoxide`, `bat`,
`tmux`, or `ssh` today must drop to the raw `programs` passthrough, losing the rename-insulation the
curated layer exists to provide. Broadening the curated set is the next step of the catalog arc.

## What Changes

- **Add five curated concepts**, each mapped to a stable home-manager surface:
  - `fzf` → `programs.fzf.enable` (a `ProgramToggle`, exactly like `direnv`/`starship`).
  - `zoxide` → `programs.zoxide.enable` (a `ProgramToggle`).
  - `bat` → `programs.bat.enable` + `programs.bat.config` (a `key→value` attrset forwarded verbatim).
  - `tmux` → `programs.tmux.enable` + `programs.tmux.extraConfig` (the raw `tmux.conf` string — the
    stable surface that dodges tmux option-rename churn).
  - `ssh` → `programs.ssh.enable` + `programs.ssh.extraConfig` (the raw ssh-config string — the stable
    surface).
- **Each concept is a typed field** on `HomeManagerSettings`, so the existing unknown-field rejection
  (`UnmarshalJSON` + `DisallowUnknownFields`) catches a mistyped sub-key (e.g. `bat.confgi`) at load —
  a silent drop would mean a setting that mysteriously never applies.
- **Each concept that owns a `programs.<name>` entry is registered in `curatedPrograms`**, so a raw
  `programs.<name>` passthrough that targets the same name conflicts loudly (the existing conflict
  error), rather than producing a Nix double-definition.
- **Deterministic rendering preserved** — the new concepts render fixed statements; the `bat.config`
  attrset is sorted; identical settings still yield identical `home.nix`.
- **Example manifest** under `manifests/examples/home-manager-settings.jsonc` showing curated concepts
  (including the new ones) + a raw `programs` block + a `files` entry (with a staged source file).

## Capabilities

### New Capabilities

- `nix-home-manager-catalog`: the curated home-manager catalog accepts the additional concepts `fzf`,
  `zoxide`, `bat`, `tmux`, and `ssh`, each mapped to a stable home-manager option, so a user declaring
  these in Endstate's format is insulated from home-manager option renames without dropping to the raw
  passthrough. Mistyped sub-keys are rejected at load; a raw passthrough colliding with a curated name
  is a clear error.

### Modified Capabilities

- None. Purely additive to the existing catalog compile path; the four prior curated concepts, the raw
  passthrough, and the files map are unchanged.

## Impact

- `internal/manifest/types.go` — `HomeManagerSettings` gains `Fzf`/`Zoxide` (`*ProgramToggle`), `Bat`
  (`*BatSettings`), `Tmux` (`*TmuxSettings`), `SSH` (`*SSHSettings`); three small typed structs added.
- `internal/realizer/nix/home_catalog.go` — `curatedPrograms` gains `fzf`/`zoxide`/`bat`/`tmux`/`ssh`;
  `CompileHomeNix` renders each new concept following the existing git/direnv/starship patterns.
- `internal/manifest/home_settings_test.go` — load + unknown-sub-key rejection tests for the new
  concepts.
- `internal/realizer/nix/home_catalog_test.go` — render + raw-overlap conflict tests; two pre-existing
  tests that used `bat`/`fzf` as raw-passthrough placeholders switch to a non-curated name (`htop`).
- `manifests/examples/home-manager-settings.jsonc` (+ a staged `hm-settings-assets/` source — kept out
  of the gitignored `payload/` tree so the example is self-consistent in the repo) — example manifest.
- Zero winget / Windows / package-capture regression; the catalog path is Nix-only.
