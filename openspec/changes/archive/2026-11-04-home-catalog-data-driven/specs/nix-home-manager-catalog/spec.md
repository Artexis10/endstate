## ADDED Requirements

### Requirement: The curated catalog maps eleven dotfiles/CLI-tier programs to stable home-manager options

The engine SHALL accept the curated home-manager catalog concepts `ripgrep`, `fd`, `zsh`, `bash`,
`helix`, `kitty`, `alacritty`, `wezterm`, `jujutsu`, `atuin`, and `yazi` declared in
`homeManager.settings`, and SHALL map each to a **stable** home-manager option so the declaration
keeps working when the underlying home-manager option surface changes. The user SHALL NOT have to
author any Nix to declare these.

- `ripgrep` SHALL map to `programs.ripgrep.enable` plus an optional `programs.ripgrep.arguments`
  list of raw CLI flag strings (e.g. `["--smart-case", "--hidden"]`).
- `fd` SHALL map to `programs.fd.enable` plus an optional `programs.fd.extraOptions` list of raw
  CLI flag strings (e.g. `["--hidden", "--no-ignore"]`).
- `zsh` SHALL map to `programs.zsh.enable` plus an optional `programs.zsh.initContent` raw
  `.zshrc` body (the consolidated, stable zsh init surface).
- `bash` SHALL map to `programs.bash.enable` plus an optional `programs.bash.initExtra` raw
  `.bashrc` body.
- `helix`, `kitty`, `alacritty`, `jujutsu`, `atuin`, and `yazi` SHALL each map to
  `programs.<name>.enable` plus an optional `programs.<name>.settings` attrset forwarded verbatim
  (each tool's own config namespace, which may be deeply nested).
- `wezterm` SHALL map to `programs.wezterm.enable` plus an optional `programs.wezterm.extraConfig`
  raw Lua configuration string.

#### Scenario: A dotfiles-tier curated concept is compiled into the generated home.nix

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest
  declares one of the curated concepts `ripgrep`, `fd`, `zsh`, `bash`, `helix`, `kitty`,
  `alacritty`, `wezterm`, `jujutsu`, `atuin`, or `yazi`
- **THEN** the engine SHALL compile it into the corresponding `programs.<name>` option(s) in the
  generated `home.nix`
- **AND** the user SHALL NOT have to author any Nix, `home.nix`, or flake wiring

#### Scenario: List-valued second fields render as a Nix list

- **WHEN** the declaration sets `ripgrep.arguments` or `fd.extraOptions` to a list of flag strings
  (e.g. `["--smart-case","--hidden"]`)
- **THEN** the engine SHALL render them as `programs.ripgrep.arguments = [ "--smart-case" "--hidden" ];`
  or `programs.fd.extraOptions = [ "--hidden" "--no-ignore" ];` in the generated `home.nix`
- **AND** the rendering SHALL be deterministic, so identical settings produce an identical `home.nix`

#### Scenario: Settings attrsets are forwarded verbatim

- **WHEN** the declaration sets `helix.settings`, `kitty.settings`, `alacritty.settings`,
  `jujutsu.settings`, `atuin.settings`, or `yazi.settings` to a (possibly nested) attrset
- **THEN** the engine SHALL render it as `programs.<name>.settings = { ... };` in the generated
  `home.nix`, with sorted keys for determinism, and nested attrsets rendered recursively

#### Scenario: Raw string second fields use a stable extraConfig/init surface

- **WHEN** the declaration sets `zsh.initContent`, `bash.initExtra`, or `wezterm.extraConfig` to a
  raw configuration string
- **THEN** the engine SHALL render it as `programs.<name>.<field> = "...";` in the generated
  `home.nix`, with newlines, quotes, and `${` escaped for valid Nix string syntax
- **AND** the mapping SHALL shield the declaration from home-manager option renames, so the curated
  concept keeps working when the underlying per-feature options change

#### Scenario: An enable toggle with no second field pins the program with only enable

- **WHEN** a dotfiles-tier curated concept declares `enable` (true or false) with no optional
  second field value
- **THEN** the engine SHALL render exactly `programs.<name>.enable = <bool>;` and SHALL NOT emit an
  empty second-field statement

### Requirement: The dotfiles-tier curated concepts reject unknown sub-keys and conflict with overlapping raw passthrough

The dotfiles-tier curated concepts SHALL share the catalog's load-time and compile-time guards: a
mistyped sub-key SHALL be rejected at load, and a raw `programs.<name>` passthrough that targets the
same name as a curated concept SHALL be a clear error rather than a Nix double definition.

#### Scenario: A mistyped sub-key on a dotfiles-tier concept is rejected

- **WHEN** a declaration uses one of the dotfiles-tier curated concepts with an unrecognized
  sub-key (for example `ripgrep.arguemnts`, `zsh.initContnet`, `kitty.settigns`, or
  `wezterm.extraCfg`)
- **THEN** the engine SHALL reject the manifest with a clear error rather than silently dropping the
  setting

#### Scenario: A raw passthrough overlapping a dotfiles-tier concept is an error

- **WHEN** a declaration sets both a dotfiles-tier curated concept (for example `ripgrep`) and a raw
  `programs` entry for the same name (`programs.ripgrep`)
- **THEN** the engine SHALL reject the declaration with a clear conflict error rather than emitting a
  duplicate `programs.<name>` definition that Nix would reject with an opaque error

### Requirement: Curated catalog emission is data-driven and behavior-preserving

The engine SHALL render every uniform curated concept (every concept that owns a `programs.<name>`
entry and maps to a bare `enable` plus at most one stable second field) from a single descriptor
table via one generic emit loop, rather than a bespoke per-concept emit block. The genuinely
non-uniform concepts `git` (nested `user`/`init` via `extraConfig`) and `shell` (which maps to
`home.*`, not a `programs.<name>` entry) MAY remain bespoke. The raw-overlap guard set SHALL be
derived from the same descriptor table so that registering a curated program automatically
registers its conflict with an overlapping raw passthrough.

#### Scenario: The generated home.nix is unchanged by the table-driven refactor

- **WHEN** a manifest declares any combination of the previously-supported curated concepts and the
  raw `programs` passthrough
- **THEN** the engine SHALL produce a `home.nix` byte-identical to the one the prior per-concept
  emit blocks produced, with the same statement order and the same determinism guarantees

#### Scenario: Registering a curated program also registers its raw-overlap guard

- **WHEN** a new curated program is added as one descriptor-table row
- **THEN** a raw `programs.<name>` passthrough targeting that program's name SHALL be rejected as a
  conflict without any separate edit to the raw-overlap guard set
