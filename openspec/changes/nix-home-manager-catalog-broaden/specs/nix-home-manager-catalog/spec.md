## ADDED Requirements

### Requirement: The curated catalog maps additional developer programs to stable home-manager options

The engine SHALL accept the curated home-manager catalog concepts `fzf`, `zoxide`, `bat`, `tmux`, and
`ssh` declared in `homeManager.settings`, and SHALL map each to a **stable** home-manager option so the
declaration keeps working when the underlying home-manager option surface changes. The user SHALL NOT
have to author any Nix to declare these.

- `fzf` and `zoxide` SHALL map to `programs.fzf.enable` and `programs.zoxide.enable` respectively.
- `bat` SHALL map to `programs.bat.enable` plus an optional `programs.bat.config` key→value attrset.
- `tmux` SHALL map to `programs.tmux.enable` plus an optional `programs.tmux.extraConfig` (a raw
  `tmux.conf` string).
- `ssh` SHALL map to `programs.ssh.enable` plus an optional `programs.ssh.extraConfig` (a raw ssh
  config string).

#### Scenario: A new curated concept is compiled into the generated home.nix

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest declares
  one of the curated concepts `fzf`, `zoxide`, `bat`, `tmux`, or `ssh`
- **THEN** the engine SHALL compile it into the corresponding `programs.<name>` option(s) in the
  generated `home.nix`
- **AND** the user SHALL NOT have to author any Nix, `home.nix`, or flake wiring

#### Scenario: The mapping uses a stable surface

- **WHEN** the declaration uses `tmux` or `ssh`
- **THEN** the engine SHALL render the raw configuration string through `extraConfig` rather than a
  per-feature typed option
- **AND** the mapping SHALL shield the declaration from home-manager option renames, so the curated
  concept keeps working when the underlying per-feature options change

#### Scenario: An enable toggle can pin a program off

- **WHEN** a curated toggle concept (`fzf`, `zoxide`) declares `enable: false`
- **THEN** the engine SHALL render an explicit `programs.<name>.enable = false` in the generated
  `home.nix`

#### Scenario: The bat config attrset is forwarded and deterministic

- **WHEN** the declaration sets `bat.config` to a set of key→value entries
- **THEN** the engine SHALL render them as `programs.bat.config` in the generated `home.nix`
- **AND** the rendering SHALL be deterministic, so identical settings produce an identical `home.nix`

### Requirement: New curated concepts reject unknown sub-keys and conflict with overlapping raw passthrough

The broadened curated concepts SHALL share the catalog's load-time and compile-time guards: a mistyped
sub-key SHALL be rejected at load, and a raw `programs.<name>` passthrough that targets the same name as
a curated concept SHALL be a clear error rather than a Nix double definition.

#### Scenario: A mistyped sub-key on a new concept is rejected

- **WHEN** a declaration uses a new curated concept with an unrecognized sub-key (for example
  `bat.confgi`, `tmux.extraConfigg`, `ssh.extarConfig`, or `fzf.enabel`)
- **THEN** the engine SHALL reject the manifest with a clear error rather than silently dropping the
  setting

#### Scenario: A raw passthrough overlapping a new curated concept is an error

- **WHEN** a declaration sets both a new curated concept (for example `fzf`) and a raw `programs` entry
  for the same name (`programs.fzf`)
- **THEN** the engine SHALL reject the declaration with a clear conflict error rather than emitting a
  duplicate `programs.<name>` definition
