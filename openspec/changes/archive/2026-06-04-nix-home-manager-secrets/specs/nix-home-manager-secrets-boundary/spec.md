## ADDED Requirements

### Requirement: Documented-boundary secrets are referenced, never embedded

The engine SHALL emit only a reference to secret material — never the material itself — when a
home-manager configuration declares `homeManager.secrets`. A secret entry SHALL NEVER be read into
the generated tree: its content SHALL NOT appear in the generated `flake.nix`, the compiled or
copied-in `home.nix`, the generated `secrets.nix`, or any staged file. The reference (the declared
path, wired as an out-of-store symlink that resolves at activation) is all that is emitted, so the
generated directory stays safe to read and to commit, and the secret never enters the `/nix/store`.
(Phase 1 is path-only; env-exposed secrets are deferred — see the backend requirement below.)

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

### Requirement: Secrets compose with the engine-generated modes and are rejected with flake mode

The engine SHALL treat `homeManager.secrets` as a sibling of `settings`, `config`, and `flake` that
composes with the engine-generated modes (`settings` and `config`) by wiring the reference sinks
into the generated configuration as a separate module. The engine SHALL reject `homeManager.secrets`
combined with a pure `homeManager.flake` input at load with a clear error, because the user's
external flake owns its own secrets and the engine generates nothing to inject reference sinks into.

#### Scenario: Secrets compose with settings mode

- **WHEN** a manifest declares `homeManager.settings` together with `homeManager.secrets`
- **THEN** the manifest SHALL load successfully
- **AND** the engine SHALL wire the secret reference sinks into the generated configuration without
  altering the compiled settings

#### Scenario: Secrets compose with config mode without touching the user's home.nix

- **WHEN** a manifest declares `homeManager.config` together with `homeManager.secrets`
- **THEN** the engine SHALL stage the secret reference sinks as a separate module beside the wrapped
  flake
- **AND** the user's copied-in `home.nix` SHALL remain unchanged

#### Scenario: Secrets with flake mode are rejected at load

- **WHEN** a manifest declares `homeManager.flake` together with `homeManager.secrets`
- **THEN** the engine SHALL reject the manifest at load with a clear error
- **AND** it SHALL direct the user to declare secrets under `settings` or `config`

### Requirement: Secrets backend is explicitly declared and defaults to boundary

The engine SHALL accept only the documented-boundary backend in Phase 1: a secret entry's `backend`
SHALL be empty (defaulting to `"boundary"`) or exactly `"boundary"`. An unsupported backend SHALL be
rejected at load with a clear error, and the engine SHALL NOT fall back to embedding the secret.
Each entry SHALL declare a `path` reference and a non-empty, unique `name`. Env-exposed secrets are
deferred in Phase 1 and SHALL be rejected at load.

#### Scenario: Boundary backend (explicit or default) is accepted

- **WHEN** a `homeManager.secrets` entry declares `backend: "boundary"` or omits `backend`
- **THEN** the engine SHALL accept the entry
- **AND** generate the documented-boundary reference wiring for it

#### Scenario: An unsupported backend is rejected at load

- **WHEN** a `homeManager.secrets` entry declares a `backend` other than `"boundary"`
- **THEN** the engine SHALL reject the manifest at load with a clear "unsupported backend" error
- **AND** it SHALL NOT generate any configuration that embeds the secret

#### Scenario: An entry without a path reference is rejected

- **WHEN** a `homeManager.secrets` entry declares no `path`
- **THEN** the engine SHALL reject the manifest at load with a clear error

#### Scenario: An env-exposed secret is rejected in Phase 1

- **WHEN** a `homeManager.secrets` entry declares an `env`
- **THEN** the engine SHALL reject the manifest at load with a clear "not yet supported" error
- **AND** it SHALL direct the user to declare the secret as a `path` reference

### Requirement: Capture carries secret references and never the material

The engine SHALL carry the declared secret references — `name`, `path`/`env`, and `backend` — into
the captured manifest when capture recovers a secrets-bearing home-manager configuration, and SHALL
NOT carry the secret material. The references are recovered from the engine's provisioning history,
which records references only, so the apply↔capture loop SHALL NOT be a leak path.

#### Scenario: Captured manifest references the source, never the material

- **WHEN** `capture` runs on a machine whose provisioning history records a secrets-bearing
  home-manager configuration
- **THEN** the captured manifest SHALL include the `homeManager.secrets` references alongside the
  recovered `settings`/`config`
- **AND** the captured manifest SHALL NOT contain any secret material (a sentinel at a secret's
  content location SHALL be absent from the captured manifest)
