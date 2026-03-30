# version-envelope-injection Specification

## Purpose

Defines how CLI and schema versions are sourced at runtime and injected into the JSON envelope. Replaces hardcoded version strings with file-based version reads, establishing `VERSION` and `SCHEMA_VERSION` as the single sources of truth.

## Requirements

### Requirement: VERSION file is the source of truth for cliVersion

The engine SHALL read `cliVersion` from the `VERSION` file at the repo root, never from a hardcoded string.

#### Scenario: cliVersion populated from VERSION file

- **GIVEN** a `VERSION` file at the repo root containing a valid semver string (e.g., `0.1.0`)
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field matches the contents of `VERSION` exactly

#### Scenario: VERSION file format

- **GIVEN** the `VERSION` file at the repo root
- **THEN** it contains exactly one line matching `^\d+\.\d+\.\d+$` with no trailing newline

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

The engine SHALL NOT contain hardcoded `cliVersion` or `schemaVersion` values in any code path that constructs the JSON envelope or capture bundle metadata.

#### Scenario: Grep for hardcoded versions

- **WHEN** the codebase is searched for hardcoded version assignment to envelope fields
- **THEN** all `cliVersion` / `endstateVersion` values trace back to a `VERSION` file read via `config.ReadVersion` (`go-engine/internal/config/version.go`)
- **AND** all `schemaVersion` values trace back to a `SCHEMA_VERSION` file read via `config.ReadSchemaVersion`

### Requirement: Capture bundle metadata uses shared version functions

The capture bundle metadata constructor SHALL use `config.ReadVersion` and `config.ReadSchemaVersion` from `go-engine/internal/config/version.go` instead of inline file reads with hardcoded fallbacks.

#### Scenario: Capture metadata version matches envelope version

- **GIVEN** `VERSION` contains `0.1.0` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** a capture bundle metadata object is created
- **THEN** `endstateVersion` equals `0.1.0`
- **AND** `schemaVersion` equals `1.0`

### Requirement: Capabilities supportedSchemaVersions derived from file

The `supportedSchemaVersions` object in capabilities data SHALL derive its `min` and `max` values from the `SCHEMA_VERSION` file, not from hardcoded strings.

#### Scenario: supportedSchemaVersions reflects current schema

- **GIVEN** `SCHEMA_VERSION` contains `1.0`
- **WHEN** capabilities data is requested
- **THEN** `supportedSchemaVersions.min` equals `1.0`
- **AND** `supportedSchemaVersions.max` equals `1.0`

### Requirement: Schema major bump forces CLI major bump

The bump automation SHALL enforce that a schema major version bump also bumps the CLI major version.

#### Scenario: schema-major bump coupling

- **GIVEN** `VERSION` contains `0.1.0` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** `bump-version.ps1 -Bump schema-major` is run
- **THEN** `SCHEMA_VERSION` becomes `2.0`
- **AND** `VERSION` becomes `1.0.0` (major bumped)

#### Scenario: schema-minor bump does not touch CLI version

- **GIVEN** `VERSION` contains `0.1.0` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** `bump-version.ps1 -Bump schema-minor` is run
- **THEN** `SCHEMA_VERSION` becomes `1.1`
- **AND** `VERSION` remains `0.1.0`

#### Scenario: CLI bump does not touch schema version

- **GIVEN** `VERSION` contains `0.1.0` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** `bump-version.ps1 -Bump patch` is run
- **THEN** `VERSION` becomes `0.1.1`
- **AND** `SCHEMA_VERSION` remains `1.0`

## Implementation References

- `VERSION` — repo root, plain text
- `SCHEMA_VERSION` — repo root, plain text
- `scripts/bump-version.ps1` — version bump automation
- `go-engine/internal/config/version.go` — `ReadVersion` and `ReadSchemaVersion` functions
- `go-engine/internal/envelope/envelope.go` — envelope construction
- `go-engine/internal/bundle/` — capture bundle metadata
- `docs/SEMVER_SYSTEM.md` — full design specification
