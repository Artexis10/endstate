## MODIFIED Requirements

### Requirement: Compile-time ldflags are the primary source for cliVersion; VERSION file is the dev-mode fallback

The engine SHALL resolve `cliVersion` by checking compile-time ldflags first, then falling back to the `VERSION` file at the repo root, then to the fallback constant `"0.0.0-dev"`.

#### Scenario: cliVersion populated from ldflags

- **GIVEN** the binary was compiled with `-ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.version=1.7.2"`
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field equals `1.7.2` regardless of VERSION file contents

#### Scenario: cliVersion falls back to VERSION file when ldflags unset

- **GIVEN** the binary was compiled without `-X config.version` ldflags (e.g., `go run`)
- **AND** a `VERSION` file at the repo root contains a valid semver string (e.g., `0.1.0`)
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field matches the contents of `VERSION` exactly

#### Scenario: cliVersion falls back to constant when neither ldflags nor file available

- **GIVEN** the binary was compiled without `-X config.version` ldflags
- **AND** no `VERSION` file is readable at the repo root
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `cliVersion` field equals `"0.0.0-dev"`

#### Scenario: VERSION file format

- **GIVEN** the `VERSION` file at the repo root
- **THEN** it contains exactly one line matching `^\d+\.\d+\.\d+$` with no trailing newline

### Requirement: Compile-time ldflags are the primary source for schemaVersion; SCHEMA_VERSION file is the dev-mode fallback

The engine SHALL resolve `schemaVersion` by checking compile-time ldflags first, then falling back to the `SCHEMA_VERSION` file at the repo root, then to the fallback constant `"1.0"`.

#### Scenario: schemaVersion populated from ldflags

- **GIVEN** the binary was compiled with `-ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.schemaVersion=2.0"`
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `schemaVersion` field equals `2.0` regardless of SCHEMA_VERSION file contents

#### Scenario: schemaVersion falls back to SCHEMA_VERSION file when ldflags unset

- **GIVEN** the binary was compiled without `-X config.schemaVersion` ldflags (e.g., `go run`)
- **AND** a `SCHEMA_VERSION` file at the repo root contains a valid major.minor string (e.g., `1.0`)
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `schemaVersion` field matches the contents of `SCHEMA_VERSION` exactly

#### Scenario: schemaVersion falls back to constant when neither ldflags nor file available

- **GIVEN** the binary was compiled without `-X config.schemaVersion` ldflags
- **AND** no `SCHEMA_VERSION` file is readable at the repo root
- **WHEN** any command is run with `--json`
- **THEN** the JSON envelope `schemaVersion` field equals `"1.0"`

#### Scenario: SCHEMA_VERSION file format

- **GIVEN** the `SCHEMA_VERSION` file at the repo root
- **THEN** it contains exactly one line matching `^\d+\.\d+$` with no trailing newline

### Requirement: No hardcoded version strings in envelope construction

The engine SHALL NOT contain hardcoded `cliVersion` or `schemaVersion` values in any code path that constructs the JSON envelope or capture bundle metadata. Compile-time ldflags injection is NOT considered "hardcoding" because the values trace to VERSION files at build time via build scripts.

#### Scenario: Grep for hardcoded versions

- **WHEN** the codebase is searched for hardcoded version assignment to envelope fields
- **THEN** all `cliVersion` / `endstateVersion` values trace back to `config.ReadVersion` or `config.EmbeddedVersion` (`go-engine/internal/config/version.go`)
- **AND** all `schemaVersion` values trace back to `config.ReadSchemaVersion` or `config.EmbeddedSchemaVersion`
- **AND** the only compile-time injection point is the `-ldflags -X` mechanism targeting `config.version` and `config.schemaVersion` package-level variables

## ADDED Requirements

### Requirement: Compiled binaries SHALL embed version via ldflags

Build scripts that produce release or bootstrapped binaries SHALL pass `-ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.version=<VERSION> -X github.com/Artexis10/endstate/go-engine/internal/config.schemaVersion=<SCHEMA_VERSION>"` to `go build`, reading values from the `VERSION` and `SCHEMA_VERSION` files at build time.

#### Scenario: Release build embeds version from VERSION file

- **GIVEN** `VERSION` contains `1.7.2` and `SCHEMA_VERSION` contains `1.0`
- **WHEN** the release build script compiles the engine binary
- **THEN** the binary SHALL have `config.version` set to `1.7.2`
- **AND** `config.schemaVersion` set to `1.0`

#### Scenario: Dev build without ldflags still works

- **GIVEN** a developer runs `go run ./cmd/endstate` without specifying ldflags
- **WHEN** the engine resolves versions
- **THEN** it SHALL fall back to VERSION/SCHEMA_VERSION file reads
- **AND** behavior is identical to pre-change behavior

### Requirement: Embedded version accessors expose ldflags state

The `config` package SHALL export `EmbeddedVersion()` and `EmbeddedSchemaVersion()` functions that return the raw ldflags-injected value, or empty string if ldflags were not set at compile time.

#### Scenario: EmbeddedVersion returns ldflags value when set

- **GIVEN** the binary was compiled with `-X config.version=1.7.2`
- **WHEN** `config.EmbeddedVersion()` is called
- **THEN** it returns `"1.7.2"`

#### Scenario: EmbeddedVersion returns empty string when unset

- **GIVEN** the binary was compiled without `-X config.version`
- **WHEN** `config.EmbeddedVersion()` is called
- **THEN** it returns `""`

#### Scenario: EmbeddedSchemaVersion returns ldflags value when set

- **GIVEN** the binary was compiled with `-X config.schemaVersion=2.0`
- **WHEN** `config.EmbeddedSchemaVersion()` is called
- **THEN** it returns `"2.0"`

#### Scenario: EmbeddedSchemaVersion returns empty string when unset

- **GIVEN** the binary was compiled without `-X config.schemaVersion`
- **WHEN** `config.EmbeddedSchemaVersion()` is called
- **THEN** it returns `""`
