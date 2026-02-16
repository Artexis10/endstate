## MODIFIED Requirements

### Requirement: Single-App Sandbox Validation

The system SHALL provide a host-side script `scripts/sandbox-validate.ps1` that validates a module's capture/restore cycle in Windows Sandbox without modifying the host machine.

#### Scenario: Validate git module

- **WHEN** user runs `.\scripts\sandbox-validate.ps1 -AppId git`
- **THEN** Windows Sandbox launches and executes: install → seed → capture → wipe → restore → verify
- **AND** artifacts are written to `sandbox-tests/validation/git/<timestamp>/`
- **AND** `result.json` contains status "PASS" or "FAIL"
- **AND** `DONE.txt` is written on success or `ERROR.txt` on failure
- **AND** a one-line summary is printed: `PASS: git` or `FAIL: git`

#### Scenario: Validate with explicit WingetId

- **WHEN** user runs `.\scripts\sandbox-validate.ps1 -AppId git -WingetId "Git.Git"`
- **THEN** the specified WingetId is used for installation instead of module lookup

#### Scenario: Custom output directory

- **WHEN** user runs `.\scripts\sandbox-validate.ps1 -AppId git -OutDir "C:\temp\validation"`
- **THEN** artifacts are written to the specified directory

#### Scenario: Module not found

- **WHEN** user runs `.\scripts\sandbox-validate.ps1 -AppId nonexistent`
- **THEN** script exits with error and prints "Module not found: nonexistent"

#### Scenario: Install success detected from stdout content

- **WHEN** winget install completes and stdout contains "Successfully installed"
- **THEN** the install SHALL be treated as successful regardless of exit code value
- **AND** if exit code was not integer 0, a NOTE log line SHALL record the original exit code value
- **AND** `installExitCode` in result.json SHALL be overridden to 0

#### Scenario: Install success detected from exit code zero

- **WHEN** winget install completes with integer exit code 0
- **THEN** the install SHALL be treated as successful

#### Scenario: Already installed detected from stdout content

- **WHEN** winget install output contains "already installed"
- **THEN** the install SHALL be treated as already-installed and validation continues

#### Scenario: Already installed detected from exit code

- **WHEN** winget install completes with exit code -1978335135
- **THEN** the install SHALL be treated as already-installed and validation continues

#### Scenario: Null or non-integer exit code with successful output

- **WHEN** `$proc.ExitCode` returns null, empty, AutomationNull, or a non-integer value
- **AND** stdout contains "Successfully installed"
- **THEN** the install SHALL be treated as successful
- **AND** `installExitCode` SHALL be set to 0

#### Scenario: Null or non-integer exit code without successful output

- **WHEN** `$proc.ExitCode` returns null, empty, AutomationNull, or a non-integer value
- **AND** stdout does NOT contain "Successfully installed" or "already installed"
- **THEN** the install SHALL be treated as failed

#### Scenario: Exit code type diagnostics logged

- **WHEN** winget install completes
- **THEN** the exit code value and its .NET type name SHALL be logged for diagnostics

#### Scenario: ProgressPreference set at script scope

- **WHEN** the sandbox-validate.ps1 script starts execution
- **THEN** `$ProgressPreference` SHALL be set to `SilentlyContinue` before any execution after the param block

## ADDED Requirements

### Requirement: Defensive Exit Code Reading

The sandbox-side script SHALL read `$proc.ExitCode` defensively, catching exceptions that may occur when the process handle has been garbage-collected or is otherwise unavailable.

#### Scenario: ExitCode read succeeds

- **WHEN** `$proc.ExitCode` is accessible
- **THEN** the value SHALL be captured and used for secondary success detection

#### Scenario: ExitCode read throws exception

- **WHEN** accessing `$proc.ExitCode` throws an exception
- **THEN** the exception SHALL be caught and logged as a WARNING
- **AND** exit code SHALL default to `$null`
- **AND** output-based success detection SHALL proceed normally
