# bootstrap-full-sync Specification

## Purpose
Defines the bootstrap command behavior: installing the Go binary and .cmd shim to the user-local Endstate directory, adding it to the user PATH, and ensuring the bootstrapped copy produces identical output.
## Requirements
### Requirement: Bootstrap installs Go binary and .cmd shim

On Windows, `endstate bootstrap` SHALL copy the running Go binary to
`%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` and write a `.cmd` shim at
`%LOCALAPPDATA%\Endstate\bin\endstate.cmd` that delegates to it.

#### Scenario: Binary and shim are present after bootstrap

- **WHEN** `endstate bootstrap` is executed on Windows
- **THEN** `%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` SHALL exist
- **AND** `%LOCALAPPDATA%\Endstate\bin\endstate.cmd` SHALL exist and contain a shim that invokes `lib\endstate.exe`

#### Scenario: Install directory added to user PATH

- **WHEN** `endstate bootstrap` is executed on Windows
- **AND** `%LOCALAPPDATA%\Endstate\bin\` is not already in the user PATH
- **THEN** the directory SHALL be added to the user PATH via the registry (HKCU\Environment\Path)

### Requirement: Bootstrap always force-overwrites the binary

Bootstrap SHALL unconditionally overwrite the installed binary on every platform. It MUST NOT skip the
copy based on existence or timestamp. The single exception is the self-install case on Unix described
in "Bootstrap never truncates the running binary on Unix": when the running binary already resolves to
the install target, the copy is skipped to avoid truncating the binary currently executing.

#### Scenario: Re-bootstrap overwrites stale binary

- **WHEN** the installed binary has been modified since last bootstrap
- **AND** `endstate bootstrap` is executed from a different binary than the installed copy
- **THEN** the modified binary SHALL be replaced with the currently running binary

### Requirement: Bootstrap reports install results

After completing the install, bootstrap SHALL report the install path, shim path, and whether PATH was
modified. The JSON payload shape is identical on every platform: `installPath`, `shimPath`, and
`addedToPath`. On Windows `shimPath` is the `.cmd` shim; on Unix `shimPath` is the symlink and
`addedToPath` is always `false`.

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

### Requirement: Bootstrap installs Go binary and symlink on Unix

On Linux and macOS, `endstate bootstrap` SHALL copy the running Go binary to
`${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin/lib/endstate`, mark it executable (mode `0755`), and
create a symlink at `$HOME/.local/bin/endstate` that points to the installed binary. The reported
`installPath` SHALL be `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin` and the reported `shimPath`
SHALL be the symlink path.

#### Scenario: Binary and symlink are present after bootstrap

- **WHEN** `endstate bootstrap` is executed on Linux or macOS
- **THEN** `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin/lib/endstate` SHALL exist with executable mode `0755`
- **AND** `$HOME/.local/bin/endstate` SHALL be a symlink pointing to that installed binary

#### Scenario: Re-bootstrap re-points the symlink idempotently

- **WHEN** `endstate bootstrap` is executed on Unix and a file or symlink already exists at `$HOME/.local/bin/endstate`
- **THEN** the existing entry SHALL be removed and re-created as a symlink to the installed binary
- **AND** the command SHALL NOT nest the link or fail because the target already exists

### Requirement: Bootstrap never edits PATH or shell configuration on Unix

On Linux and macOS, bootstrap MUST NOT modify the user PATH, any shell rc file, or any other shell
configuration. The reported `addedToPath` SHALL always be `false` on Unix. When `$HOME/.local/bin` is
not already present on PATH, bootstrap SHALL include a one-line hint in the human-readable output
without changing the JSON payload shape.

#### Scenario: addedToPath is always false on Unix

- **WHEN** `endstate bootstrap` completes successfully on Linux or macOS
- **THEN** the JSON envelope `data.addedToPath` SHALL be `false`
- **AND** no shell rc file SHALL have been modified

#### Scenario: PATH hint shown when symlink dir is off PATH

- **WHEN** `endstate bootstrap` runs on Unix and `$HOME/.local/bin` is not on PATH
- **THEN** the human-readable output SHALL include a one-line hint to add it to PATH
- **AND** the JSON payload fields SHALL be unchanged

### Requirement: Bootstrap never truncates the running binary on Unix

Bootstrap on Linux and macOS SHALL skip the binary copy when the currently running binary already
resolves to the install target (a re-bootstrap of the installed copy invoked through its own symlink),
rather than truncating the binary it is executing. The installed binary SHALL remain intact and the
symlink SHALL still point to it.

#### Scenario: Self-bootstrap preserves the installed binary

- **WHEN** the installed `$HOME/.local/bin/endstate` symlink is invoked as `endstate bootstrap`
- **AND** the running binary resolves to `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin/lib/endstate`
- **THEN** bootstrap SHALL skip copying the binary onto itself
- **AND** the installed binary SHALL remain a complete, executable file

