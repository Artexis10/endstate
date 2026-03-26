# Endstate Go Engine

Go rewrite of the Endstate engine — a declarative system provisioning and recovery tool for Windows.

## Module

```
github.com/Artexis10/endstate/go-engine
```

Requires Go 1.22 or later.

## Structure

```
go-engine/
  cmd/endstate/main.go         CLI entrypoint (os.Args parsing, dispatch, JSON envelope output)
  internal/envelope/
    envelope.go                Envelope construction (NewSuccess, NewFailure, Marshal, BuildRunID)
    errors.go                  ErrorCode constants and Error type
    envelope_test.go           Table-driven unit tests
  internal/config/
    version.go                 VERSION and SCHEMA_VERSION file reading
    paths.go                   Repo root resolution, profile dir, ExpandWindowsEnvVars
  internal/manifest/
    types.go                   Manifest, App, RestoreEntry, VerifyEntry types
    loader.go                  JSONC comment stripping, includes resolution, circular detection
    validator.go               Profile validation per profile-contract.md
    loader_test.go             Table-driven manifest and validator tests
  internal/events/
    types.go                   NDJSON event types (Phase, Item, Summary, Error, Artifact)
    emitter.go                 Event emitter (writes NDJSON to stderr)
    emitter_test.go            Event emission and format tests
  internal/driver/
    driver.go                  Driver interface (Detect, Install)
    winget/
      detect.go                Winget package detection via winget list
      winget.go                Winget package installation with exit code parsing
      winget_test.go           Exec-mocked winget driver tests
  internal/commands/
    capabilities.go            capabilities command handler
    verify.go                  verify command (manifest load, detect, results envelope)
    apply.go                   apply command (plan/apply/verify phases, dry-run support)
    commands_test.go           Command tests with mock driver
```

## Build

```bash
cd go-engine
go mod tidy
go build ./cmd/endstate
```

## Run

```bash
# Capabilities handshake (JSON)
./endstate capabilities --json

# Human-readable
./endstate capabilities

# Debug flag resolution
./endstate apply --manifest ./manifest.jsonc --dry-run --debug-cli --json
```

## Test

```bash
go test ./internal/envelope/...
go test ./...
```

## Design Rules

- No external CLI framework. Arg parsing uses `os.Args` only.
- JSON envelope emitted as the last line of stdout, compact (no indentation).
- RunId format: `<command>-YYYYMMDD-HHMMSS-<HOSTNAME>`
- Every function returns `(result, error)`. No panics.
- Version strings read from `VERSION` and `SCHEMA_VERSION` files at repo root.
  `ENDSTATE_ROOT` env var overrides repo root detection.

## Capabilities Response

The `capabilities` command returns the full handshake response expected by the
Endstate GUI, including supported schema versions, commands with their flags,
feature flags, and platform information. See
`docs/contracts/gui-integration-contract.md` for the authoritative schema.
