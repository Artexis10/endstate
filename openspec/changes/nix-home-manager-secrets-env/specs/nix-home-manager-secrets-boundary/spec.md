## MODIFIED Requirements

### Requirement: Documented-boundary secrets are referenced, never embedded

The engine SHALL emit only a reference to secret material — never the material itself — when a
home-manager configuration declares `homeManager.secrets`. A secret entry SHALL NEVER be read into
the generated tree: its content SHALL NOT appear in the generated `flake.nix`, the compiled or
copied-in `home.nix`, the generated `secrets.nix`, or any staged file. The reference (the declared
path, wired as an out-of-store symlink that resolves at activation, or an environment variable that
holds the file path) is all that is emitted, so the generated directory stays safe to read and to
commit, and the secret never enters the `/nix/store`.

#### Scenario: A path entry emits a home.file source reference

- **WHEN** the engine generates the home-manager configuration for a `homeManager.secrets` entry
  that declares a `path`
- **THEN** the generated configuration SHALL contain
  `home.file.<homeRelTarget(name)>.source = <reference>;` pointing at that path
- **AND** it SHALL NOT contain the file's content

#### Scenario: A sentinel at a secret path is absent from the whole generated tree

- **WHEN** a `homeManager.secrets` entry's `path` points at a file containing a unique sentinel and
  the engine generates the home-manager configuration
- **THEN** the sentinel SHALL be absent from every generated artifact (the `flake.nix`, the
  `home.nix`, the `secrets.nix`, and any staged file)
- **AND** the generated `secrets.nix` SHALL still reference the secret path

### Requirement: Secrets backend is explicitly declared and defaults to boundary

The engine SHALL accept only the documented-boundary backend: a secret entry's `backend` SHALL be
empty (defaulting to `"boundary"`) or exactly `"boundary"`. An unsupported backend SHALL be rejected
at load with a clear error, and the engine SHALL NOT fall back to embedding the secret. Each entry
SHALL declare a `path` reference and a non-empty, unique `name`. An entry MAY additionally declare an
`env` variable name, in which case the engine emits a reference to the file `path` through that
variable (never the secret value). An `env` without a `path` SHALL be rejected at load, and an `env`
name that is not a valid identifier (`^[A-Za-z_][A-Za-z0-9_]*$`) SHALL be rejected at load.

#### Scenario: Boundary backend (explicit or default) is accepted

- **WHEN** a `homeManager.secrets` entry declares `backend: "boundary"` or omits `backend`
- **THEN** the engine SHALL accept the entry
- **AND** generate the documented-boundary reference wiring for it

#### Scenario: An unsupported backend is rejected at load

- **WHEN** a `homeManager.secrets` entry declares a `backend` other than `"boundary"`
- **THEN** the engine SHALL reject the manifest at load with a clear "unsupported backend" error
- **AND** it SHALL NOT generate any configuration that embeds the secret

#### Scenario: An entry without a path reference is rejected

- **WHEN** a `homeManager.secrets` entry declares neither a `path` nor an `env`
- **THEN** the engine SHALL reject the manifest at load with a clear error

#### Scenario: An env-exposed secret with a path is accepted

- **WHEN** a `homeManager.secrets` entry declares an `env` together with a `path`
- **THEN** the engine SHALL accept the manifest at load
- **AND** generate a reference to the file `path` through that environment variable

#### Scenario: An env-exposed secret without a path is rejected

- **WHEN** a `homeManager.secrets` entry declares an `env` but no `path`
- **THEN** the engine SHALL reject the manifest at load with a clear error
- **AND** it SHALL direct the user to declare the file via a `path` reference

#### Scenario: An invalid env name is rejected

- **WHEN** a `homeManager.secrets` entry declares an `env` name that is not a valid identifier
  (`^[A-Za-z_][A-Za-z0-9_]*$`)
- **THEN** the engine SHALL reject the manifest at load with a clear error
- **AND** it SHALL NOT generate any configuration from the rejected entry

## ADDED Requirements

### Requirement: An env-exposed secret references the file path, never the value

The engine SHALL expose an env-exposed secret as an environment variable that holds the secret's
file `path` — the `*_FILE` path-reference convention — and SHALL NEVER place the secret value into
the generated configuration. For a `homeManager.secrets` entry declaring both an `env` name and a
`path`, the engine SHALL emit `home.sessionVariables.<env> = "<path>";` referencing the file path,
and SHALL NOT emit a `home.file` sink for that entry. The no-embed guarantee SHALL hold by
construction: the engine SHALL NOT read the file at the secret's `path`, so its content SHALL be
absent from every generated artifact. Path-only and env-exposed entries SHALL be emitted
deterministically (sorted by name) so the generated configuration is stable.

#### Scenario: An env+path entry emits a sessionVariable referencing the path

- **WHEN** the engine generates the home-manager configuration for a `homeManager.secrets` entry
  declaring an `env` name and a `path`
- **THEN** the generated `secrets.nix` SHALL contain
  `home.sessionVariables.<env> = "<path>";` referencing that path
- **AND** it SHALL NOT emit a `home.file` sink for that entry

#### Scenario: A sentinel at an env secret's path is absent from the whole generated tree

- **WHEN** an env+path `homeManager.secrets` entry's `path` points at a file containing a unique
  sentinel and the engine generates the home-manager configuration
- **THEN** the sentinel SHALL be absent from every generated artifact (the `flake.nix`, the
  `home.nix`, the `secrets.nix`, and any staged file)
- **AND** the generated `secrets.nix` SHALL still reference the secret path through the
  sessionVariable

#### Scenario: Mixed path-only and env+path entries are emitted deterministically

- **WHEN** a `homeManager.secrets` list mixes a path-only entry and an env+path entry, in any input
  order
- **THEN** the engine SHALL emit both references sorted by name
- **AND** the generated `secrets.nix` SHALL be byte-identical regardless of the input order
