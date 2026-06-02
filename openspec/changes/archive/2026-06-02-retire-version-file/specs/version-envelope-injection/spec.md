## ADDED Requirements

### Requirement: Release-please manifest is the source of truth for cliVersion

The engine SHALL derive `cliVersion` from the release-please version manifest
(`.release-please-manifest.json`) or, in a released build, from the version embedded at compile time
via ldflags (which the release workflow derives from the git tag) — never from a hardcoded string and
never from a separate `VERSION` file.

#### Scenario: cliVersion from embedded ldflags version

- **GIVEN** an engine binary built with the version embedded via ldflags (e.g. `2.12.1`)
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field equals the embedded version

#### Scenario: cliVersion falls back to the release-please manifest

- **GIVEN** an engine invocation with no ldflags-embedded version (e.g. `go run`)
- **AND** a `.release-please-manifest.json` at the repo root whose root package (`.`) is a valid
  semver string
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field equals that manifest version

#### Scenario: cliVersion fallback when no version source is available

- **GIVEN** no embedded version and no readable `.release-please-manifest.json`
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field is the documented fallback (`0.0.0-dev`)

## MODIFIED Requirements

### Requirement: No hardcoded version strings in envelope construction

The engine SHALL NOT contain hardcoded `cliVersion` or `schemaVersion` values in any code path that
constructs the JSON envelope or capture bundle metadata.

#### Scenario: Grep for hardcoded versions

- **WHEN** the codebase is searched for hardcoded version assignment to envelope fields
- **THEN** all `cliVersion` / `endstateVersion` values trace back to `config.ReadVersion`
  (`go-engine/internal/config/version.go`), which returns the ldflags-embedded version or the
  `.release-please-manifest.json` value
- **AND** all `schemaVersion` values trace back to a `SCHEMA_VERSION` file read via
  `config.ReadSchemaVersion`

## REMOVED Requirements

### Requirement: VERSION file is the source of truth for cliVersion

**Reason**: The `VERSION` file is retired. The source of truth for `cliVersion` migrated to the
release-please manifest (and compile-time ldflags from the git tag); the implementation
(`config.ReadVersion`) has never read the `VERSION` file in this model, and the file was a stale
orphan (see issue #54).
**Migration**: Consumers needing the engine version SHALL read it from `cliVersion` on any `--json`
envelope (or from `.release-please-manifest.json` at the repo root for tooling).

### Requirement: Schema major bump forces CLI major bump

**Reason**: `scripts/bump-version.ps1` is retired. CLI versioning is owned by release-please
(conventional commits → version bump + tag + CHANGELOG); a manual bumper would conflict with it.
**Migration**: Bump the CLI version via conventional commits (release-please). Edit `SCHEMA_VERSION`
by hand when the schema changes; a schema-major change that is also a breaking engine change SHALL be
landed with a corresponding `feat!`/`fix!` (major) conventional commit.
