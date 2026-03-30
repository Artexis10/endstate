# bootstrap-full-sync Specification

## Purpose
Defines the bootstrap command behavior: installing the Go binary and .cmd shim to the user-local Endstate directory, adding it to the user PATH, and ensuring the bootstrapped copy produces identical output.

## Requirements

### Requirement: Bootstrap installs Go binary and .cmd shim
`endstate bootstrap` SHALL copy the running Go binary to `%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` and write a `.cmd` shim at `%LOCALAPPDATA%\Endstate\bin\endstate.cmd` that delegates to it.

#### Scenario: Binary and shim are present after bootstrap
- **WHEN** `endstate bootstrap` is executed
- **THEN** `%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` SHALL exist
- **AND** `%LOCALAPPDATA%\Endstate\bin\endstate.cmd` SHALL exist and contain a shim that invokes `lib\endstate.exe`

#### Scenario: Install directory added to user PATH
- **WHEN** `endstate bootstrap` is executed
- **AND** `%LOCALAPPDATA%\Endstate\bin\` is not already in the user PATH
- **THEN** the directory SHALL be added to the user PATH via the registry (HKCU\Environment\Path)

### Requirement: Bootstrap always force-overwrites the binary
Bootstrap SHALL unconditionally overwrite the installed binary. It MUST NOT skip the copy based on existence or timestamp.

#### Scenario: Re-bootstrap overwrites stale binary
- **WHEN** `%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` has been modified since last bootstrap
- **AND** `endstate bootstrap` is executed
- **THEN** the modified binary SHALL be replaced with the currently running binary

### Requirement: Bootstrap reports install results
After completing the install, bootstrap SHALL report the install path, shim path, and whether PATH was modified.

#### Scenario: Successful bootstrap shows install summary
- **WHEN** `endstate bootstrap` completes successfully
- **THEN** the JSON envelope `data` SHALL include `installPath`, `shimPath`, and `addedToPath` fields

#### Scenario: Copy failure is reported
- **WHEN** the binary copy fails during bootstrap (e.g., permission denied)
- **THEN** bootstrap SHALL report the failure with an error message in the JSON envelope

### Requirement: Bootstrapped copy produces identical output to repo-built binary
After bootstrap, running `endstate <command>` from PATH SHALL produce identical functional output to running the binary directly from the repo build output.

#### Scenario: capabilities --json matches after bootstrap
- **WHEN** `endstate bootstrap` has completed
- **AND** `endstate capabilities --json` is run from PATH
- **THEN** the JSON `data` field SHALL match the output of running the binary directly

#### Scenario: configModuleMap present in dry-run after bootstrap
- **WHEN** `endstate bootstrap` has completed
- **AND** `endstate apply --profile <name> --dry-run --json` is run on a manifest with configModules
- **THEN** the JSON envelope `data.configModuleMap` SHALL be present and non-null
