## Why

The PowerShell engine has reached its performance and maintainability ceiling. Go provides single-binary distribution, cross-platform compilation, strong typing, and significantly faster startup — critical for a CLI tool invoked frequently during machine provisioning. This is a language rewrite (not a behavior change), so all existing contracts and specs remain the acceptance gates.

## What Changes

- New Go project scaffolded in `go-engine/` implementing the Endstate CLI
- `capabilities` command: full JSON handshake response per gui-integration-contract
- `verify` command: manifest loading, winget detection, structured results envelope
- `apply` command: plan/apply/verify phases with winget install, dry-run support, event streaming
- JSONC manifest loading with comment stripping, includes resolution, circular detection
- NDJSON event emission to stderr (phase, item, summary, error, artifact events)
- JSON envelope output on stdout matching cli-json-contract exactly
- Winget driver: detect via `winget list`, install via `winget install`, exit code parsing
- Profile validation matching profile-contract rules

## Capabilities

### New Capabilities
- `go-engine-foundation`: Go binary producing identical JSON envelope (stdout) and NDJSON event (stderr) output as the PowerShell engine for capabilities, verify, and apply commands

### Modified Capabilities
<!-- No requirement changes — this is a language rewrite that must satisfy all existing specs identically -->

## Impact

- New `go-engine/` directory at repo root (no changes to existing PowerShell code)
- New Go module: `github.com/Artexis10/endstate/go-engine`
- Depends on Go toolchain (1.22+) for building
- Consumers (GUI, scripts) can swap `bin/endstate.ps1` for the Go binary with no protocol changes
- All existing contracts (`cli-json-contract`, `event-contract`, `gui-integration-contract`, `profile-contract`) enforced as-is
