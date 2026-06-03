## ADDED Requirements

### Requirement: The curated catalog maps four additional developer programs to stable home-manager options

The engine SHALL accept the curated home-manager catalog concepts `eza`, `gh`, `lazygit`, and `neovim`
declared in `homeManager.settings`, and SHALL map each to a **stable** home-manager option so the
declaration keeps working when the underlying home-manager option surface changes. The user SHALL NOT
have to author any Nix to declare these.

- `eza` SHALL map to `programs.eza.enable` plus an optional `programs.eza.extraOptions` list of raw
  CLI flag strings (e.g. `["--git", "--icons"]`).
- `gh` SHALL map to `programs.gh.enable` plus an optional `programs.gh.settings` attrset forwarded
  verbatim (gh's own config key namespace).
- `lazygit` SHALL map to `programs.lazygit.enable` plus an optional `programs.lazygit.settings`
  attrset forwarded verbatim (lazygit's own config structure, which may be deeply nested).
- `neovim` SHALL map to `programs.neovim.enable` plus an optional `programs.neovim.extraConfig` raw
  vimscript/lua string.

#### Scenario: A new curated concept is compiled into the generated home.nix

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest
  declares one of the curated concepts `eza`, `gh`, `lazygit`, or `neovim`
- **THEN** the engine SHALL compile it into the corresponding `programs.<name>` option(s) in the
  generated `home.nix`
- **AND** the user SHALL NOT have to author any Nix, `home.nix`, or flake wiring

#### Scenario: eza extraOptions render as a Nix list

- **WHEN** the declaration sets `eza.extraOptions` to a list of flag strings (e.g. `["--git","--icons"]`)
- **THEN** the engine SHALL render them as `programs.eza.extraOptions = [ "--git" "--icons" ];` in the
  generated `home.nix`
- **AND** the rendering SHALL be deterministic, so identical settings produce an identical `home.nix`

#### Scenario: gh and lazygit settings attrsets are forwarded verbatim

- **WHEN** the declaration sets `gh.settings` or `lazygit.settings` to a (possibly nested) attrset
- **THEN** the engine SHALL render it as `programs.gh.settings = { ... };` or
  `programs.lazygit.settings = { ... };` in the generated `home.nix`, with sorted keys for determinism
- **AND** nested attrsets SHALL be rendered recursively as Nix attrsets

#### Scenario: neovim extraConfig uses the stable extraConfig surface

- **WHEN** the declaration sets `neovim.extraConfig` to a raw vimscript/lua string
- **THEN** the engine SHALL render it as `programs.neovim.extraConfig = "...";` in the generated
  `home.nix`, with newlines, quotes, and `${` escaped for valid Nix string syntax
- **AND** the mapping SHALL shield the declaration from home-manager neovim option renames, so the
  curated concept keeps working when the underlying per-feature options change

### Requirement: The four new curated concepts reject unknown sub-keys and conflict with overlapping raw passthrough

The new curated concepts SHALL share the catalog's load-time and compile-time guards: a mistyped
sub-key SHALL be rejected at load, and a raw `programs.<name>` passthrough that targets the same name
as a curated concept SHALL be a clear error rather than a Nix double definition.

#### Scenario: A mistyped sub-key on a new concept is rejected

- **WHEN** a declaration uses one of the new curated concepts with an unrecognized sub-key (for example
  `eza.enabel`, `gh.settigns`, `lazygit.settigns`, or `neovim.extraCfg`)
- **THEN** the engine SHALL reject the manifest with a clear error rather than silently dropping the
  setting

#### Scenario: A raw passthrough overlapping a new curated concept is an error

- **WHEN** a declaration sets both a new curated concept (for example `eza`) and a raw `programs` entry
  for the same name (`programs.eza`)
- **THEN** the engine SHALL reject the declaration with a clear conflict error rather than emitting a
  duplicate `programs.<name>` definition that Nix would reject with an opaque error
