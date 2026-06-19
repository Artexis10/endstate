## Why

`homeManager.settings` curates 13 home-manager concepts today (`git`, `shell`, `direnv`,
`starship`, `fzf`, `zoxide`, `bat`, `tmux`, `ssh`, `eza`, `gh`, `lazygit`, `neovim`). Each was
added with its own typed struct AND its own bespoke `if s.X != nil { ... }` emit block in
`CompileHomeNix`. Every block follows the same shape — emit `programs.<name>.enable`, then emit
an optional STABLE second field (a raw string via `extraConfig`, an attrset via `settings`/
`config`, or a `listOf str` via `extraOptions`). The marginal cost of each new program is now an
emit block that is pure boilerplate, which both inflates the diff and is the natural place a
copy-paste bug (wrong program name, wrong field) would hide.

To push the config-restore moat into the dotfiles/CLI tier at the LOWEST marginal cost per
program, the per-concept emit blocks must collapse into a single descriptor table + one generic
emit loop. After that, adding a program is one typed field + one table row — no new emit branch.

## What Changes

- **Data-driven emission (the gate).** Replace the per-concept emit blocks in `CompileHomeNix`
  with a single `curatedTable` descriptor table and one generic emit loop. Each row is
  `{ Name, StableField, Kind, get }`: the home-manager `programs.<Name>` it owns, the
  rename-proof second option it targets, that field's `fieldKind` (`none`/`string`/`stringMap`/
  `anyMap`/`stringSlice`), and a getter pulling `(present, enable, second)` off the settings
  struct. `curatedPrograms` (the raw-overlap guard set) is derived from the table. The two
  genuinely non-uniform concepts stay bespoke and are NOT table rows: `git` (nested user/init
  via `extraConfig`) and `shell` (maps to `home.*`, not a `programs.<name>` entry). The
  generated `home.nix` is byte-identical — the refactor is behavior-preserving (all existing
  golden tests stay green).

- **Add 11 dotfiles/CLI-tier curated concepts**, each one typed field + one table row, each
  mapped to a stable home-manager surface: `ripgrep` (`arguments`), `fd` (`extraOptions`),
  `zsh` (`initContent`), `bash` (`initExtra`), `helix`/`kitty`/`alacritty`/`jujutsu`/`atuin`/
  `yazi` (`settings` attrset), and `wezterm` (`extraConfig` Lua string).

- **Document the bare-attrset passthrough.** Tools that map cleanly to `programs.<name>.enable`
  with no rename-insulation need (e.g. `lsd`, `btop`, `jq`, `broot`) already work via the raw
  `programs` passthrough with zero curation — the example manifest now demonstrates this rather
  than growing a struct per trivially-toggled tool.

## Capabilities

### Modified Capabilities

- `nix-home-manager-catalog`: the curated catalog gains 11 dotfiles/CLI-tier concepts, each
  mapped to a stable home-manager option, so a user declaring these in Endstate's format is
  insulated from home-manager option renames without dropping to the raw passthrough. Mistyped
  sub-keys are rejected at load; a raw passthrough colliding with a curated name is a clear
  error. The catalog's internal emission is now table-driven (the same observable behavior).

## Impact

- `internal/manifest/types.go` — `HomeManagerSettings` gains 11 fields (`Ripgrep`, `Fd`, `Zsh`,
  `Bash`, `Helix`, `Kitty`, `Alacritty`, `Wezterm`, `Jujutsu`, `Atuin`, `Yazi`) + 11 small typed
  structs. The `UnmarshalJSON` + `DisallowUnknownFields` typo-rejection covers them automatically.
- `internal/realizer/nix/home_catalog.go` — per-concept emit blocks replaced by `curatedTable` +
  a generic loop; `curatedPrograms` derived from the table; `fieldKind`, `secondFieldEmpty`,
  `renderSecondField` helpers added.
- `internal/manifest/home_settings_test.go` — load + unknown-sub-key rejection tests for the 11.
- `internal/realizer/nix/home_catalog_test.go` — render + raw-overlap + enable-only tests for the 11.
- `manifests/examples/home-manager-settings.jsonc` — gains `ripgrep`/`fd`/`zsh`/`kitty` curated
  entries + an `lsd` raw-passthrough demo.
- Zero winget / Windows / package-capture regression; the catalog path is Nix-only and never
  reached on the winget path.
