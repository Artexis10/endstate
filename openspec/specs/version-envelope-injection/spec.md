# version-envelope-injection Specification

## Purpose

Defines how CLI and schema versions are sourced at runtime and injected into the JSON envelope. Replaces hardcoded version strings: `cliVersion` comes from the compile-time ldflags version (falling back to the `.release-please-manifest.json` value), and `schemaVersion` comes from the `SCHEMA_VERSION` file.

## Requirements

### Requirement: SCHEMA_VERSION file is the source of truth for schemaVersion

The engine SHALL read `schemaVersion` from the `SCHEMA_VERSION` file at the repo root, never from a hardcoded string.

#### Scenario: schemaVersion populated from SCHEMA_VERSION file

- **GIVEN** a `SCHEMA_VERSION` file at the repo root containing a valid major.minor string (e.g., `1.0`)
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `schemaVersion` field matches the contents of `SCHEMA_VERSION` exactly

#### Scenario: SCHEMA_VERSION file format

- **GIVEN** the `SCHEMA_VERSION` file at the repo root
- **THEN** it contains exactly one line matching `^\d+\.\d+$` with no trailing newline

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

### Requirement: Capture bundle metadata uses shared version functions

The capture bundle metadata constructor SHALL use `config.ReadVersion` and `config.ReadSchemaVersion` from `go-engine/internal/config/version.go` instead of inline file reads with hardcoded fallbacks.

#### Scenario: Capture metadata version matches envelope version

- **GIVEN** `config.ReadVersion` resolves to `2.12.1` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** a capture bundle metadata object is created
- **THEN** `endstateVersion` equals `2.12.1`
- **AND** `schemaVersion` equals `1.0`

### Requirement: Capabilities supportedSchemaVersions derived from file

The `supportedSchemaVersions` object in capabilities data SHALL derive its `min` and `max` values from the `SCHEMA_VERSION` file, not from hardcoded strings.

#### Scenario: supportedSchemaVersions reflects current schema

- **GIVEN** `SCHEMA_VERSION` contains `1.0`
- **WHEN** capabilities data is requested
- **THEN** `supportedSchemaVersions.min` equals `1.0`
- **AND** `supportedSchemaVersions.max` equals `1.0`

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

## Implementation References

- `.release-please-manifest.json` — repo root; CLI version source of truth (release-please)
- `SCHEMA_VERSION` — repo root, plain text
- `go-engine/internal/config/version.go` — `ReadVersion` (ldflags → manifest) and `ReadSchemaVersion`
- `go-engine/internal/envelope/envelope.go` — envelope construction
- `go-engine/internal/bundle/` — capture bundle metadata
- `docs/SEMVER_SYSTEM.md` — full design specification
