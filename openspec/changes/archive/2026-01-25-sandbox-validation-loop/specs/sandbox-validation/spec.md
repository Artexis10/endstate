## ADDED Requirements

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

### Requirement: Batch Sandbox Validation

The system SHALL provide a host-side script `scripts/sandbox-validate-batch.ps1` that validates multiple modules sequentially from a queue file.

#### Scenario: Run batch validation

- **WHEN** user runs `.\scripts\sandbox-validate-batch.ps1`
- **THEN** each app in `sandbox-tests/golden-queue.jsonc` is validated sequentially
- **AND** `sandbox-tests/validation/summary.json` is written with results for all apps
- **AND** `sandbox-tests/validation/summary.md` is written with human-readable summary

#### Scenario: Custom queue file

- **WHEN** user runs `.\scripts\sandbox-validate-batch.ps1 -QueueFile "custom-queue.jsonc"`
- **THEN** the specified queue file is used instead of the default

#### Scenario: Partial failure

- **WHEN** one app in the queue fails validation
- **THEN** batch continues with remaining apps
- **AND** summary shows which apps passed and which failed

### Requirement: Golden Queue File

The system SHALL maintain a tracked queue file `sandbox-tests/golden-queue.jsonc` containing the list of modules to validate in batch mode.

#### Scenario: Queue file format

- **WHEN** queue file is read
- **THEN** it contains a JSON object with `apps` array
- **AND** each entry has `appId` (required) and `wingetId` (required)

### Requirement: Validation Artifacts

The system SHALL write validation artifacts to `sandbox-tests/validation/<appId>/<timestamp>/` for each validation run.

#### Scenario: Artifact structure

- **WHEN** validation completes (pass or fail)
- **THEN** the output directory contains:
  - `result.json` - validation result with status, counts, timestamps
  - `DONE.txt` or `ERROR.txt` - sentinel file
  - `capture/` - captured config files
  - `wipe-backup/` - wiped files (moved, not deleted)
  - `capture-manifest.json` - list of captured files
  - `wipe-manifest.json` - list of wiped files
  - `restore-manifest.json` - list of restored files
  - `verify-manifest.json` - verification results

### Requirement: Host Machine Isolation

The system SHALL NOT modify the host machine during validation. All changes occur inside Windows Sandbox.

#### Scenario: Host isolation

- **WHEN** validation runs
- **THEN** only the mapped output folder receives artifacts
- **AND** no files are modified on the host outside the output folder
