# sandbox-validation Specification

## Purpose
TBD - created by archiving change sandbox-validation-loop. Update Purpose after archive.
## Requirements
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

### Requirement: Winget Bootstrap (Strategy A)

The system SHALL attempt to bootstrap winget inside Windows Sandbox when it is not available.

#### Scenario: Winget missing in Sandbox

- **WHEN** validation starts and winget is not available
- **THEN** the system attempts to download and install App Installer from aka.ms/getwinget
- **AND** each bootstrap step is logged
- **AND** winget availability is re-checked after bootstrap

#### Scenario: Bootstrap succeeds

- **WHEN** winget bootstrap succeeds
- **THEN** validation continues with winget install

#### Scenario: Bootstrap fails with fallback available

- **WHEN** winget bootstrap fails
- **AND** the app entry in golden-queue.jsonc has `installer` metadata
- **THEN** the system uses the offline installer fallback

#### Scenario: Bootstrap fails without fallback

- **WHEN** winget bootstrap fails
- **AND** no `installer` metadata exists for the app
- **THEN** validation fails with actionable error message
- **AND** error message instructs user to add installer metadata

### Requirement: Offline Installer Fallback (Strategy B)

The system SHALL support offline installation as a fallback when winget is unavailable.

#### Scenario: Offline installer metadata format

- **WHEN** an app entry in golden-queue.jsonc includes `installer` metadata
- **THEN** the metadata contains:
  - `path` (required): relative path to installer under `sandbox-tests/installers/`
  - `silentArgs` (required): command-line arguments for silent install
  - `exePath` (optional): path to executable for install verification

#### Scenario: Offline install execution

- **WHEN** offline fallback is triggered
- **THEN** the installer is executed with silentArgs inside Sandbox
- **AND** install success is verified using exePath if provided

### Requirement: Sandbox Networking

The system SHALL enable networking in the generated .wsb configuration to allow winget bootstrap downloads.

#### Scenario: Networking enabled

- **WHEN** validate.wsb is generated
- **THEN** the configuration includes `<Networking>Default</Networking>` or omits the tag (networking enabled by default)

