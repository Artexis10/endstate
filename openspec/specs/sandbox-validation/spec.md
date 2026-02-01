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

The system SHALL attempt to bootstrap winget inside Windows Sandbox when it is not available, including installing required dependencies.

#### Scenario: Winget missing in Sandbox

- **WHEN** validation starts and winget is not available
- **THEN** the system attempts to:
  1. Download and install Windows App Runtime 1.8 redistributable (x64)
  2. Download and install VCLibs dependency
  3. Download and install App Installer from aka.ms/getwinget
- **AND** each bootstrap step is logged to `winget-bootstrap.log`
- **AND** winget availability is re-checked after bootstrap

#### Scenario: Windows App Runtime install

- **WHEN** winget bootstrap starts
- **THEN** the system downloads Windows App Runtime 1.8 redistributable from Microsoft
- **AND** installs the x64 framework package via Add-AppxPackage
- **AND** logs success or failure with full error details

#### Scenario: Bootstrap dependency failure

- **WHEN** Windows App Runtime or VCLibs installation fails
- **THEN** the error is logged with full exception details
- **AND** bootstrap continues to attempt remaining steps
- **AND** final winget availability determines success/failure

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

### Requirement: Download Progress Transparency

The system SHALL provide live download progress updates during long-running download stages to prevent the appearance of a hung process.

#### Scenario: STEP.txt progress updates during downloads

- **WHEN** a download stage is in progress (download-deps, download-appinstaller)
- **THEN** `STEP.txt` is updated at least once every 5 seconds
- **AND** the update includes: `stage=<stageName> <pct>% (<mbNow>MB/<mbTotal>MB)` when total is known
- **AND** the update includes: `stage=<stageName> <mbNow>MB` when total is unknown

#### Scenario: Both downloads use WebClient async with progress

- **WHEN** downloading DesktopAppInstaller_Dependencies.zip or Microsoft.DesktopAppInstaller.msixbundle
- **THEN** the download MUST use WebClient.DownloadFileAsync with progress polling
- **AND** Content-Length MUST be obtained via HEAD request when possible
- **AND** progress updates MUST be written to STEP.txt at least every 3 seconds

#### Scenario: Host displays live progress

- **WHEN** the host script polls `STEP.txt` (every 1 second)
- **THEN** any change in content is immediately displayed to the user
- **AND** the user sees download progress values change at least twice during download stages
- **AND** a 15-second heartbeat fallback is shown only if no progress updates occurred recently
- **AND** the heartbeat includes elapsed time and last known stage

### Requirement: Host Fail-Fast Guard

The system SHALL detect when Windows Sandbox exits unexpectedly and fail immediately with a clear message.

#### Scenario: Sandbox exits before completion

- **WHEN** sandbox processes (WindowsSandboxClient, WindowsSandboxServer, VmmemWSB) are not running
- **AND** `STARTED.txt` was seen (script began execution)
- **AND** neither `DONE.txt` nor `ERROR.txt` exist
- **THEN** the host script exits immediately with error
- **AND** prints: "Sandbox exited before producing DONE/ERROR"

### Requirement: Required Artifacts on PASS

The system SHALL produce a complete set of artifacts when validation passes.

#### Scenario: PASS artifacts

- **WHEN** validation completes with status PASS
- **THEN** the output directory contains:
  - `STARTED.txt` - script startup marker with timestamp and PID
  - `STEP.txt` - last stage marker (updated during progress)
  - `winget-bootstrap.log` - bootstrap log with DOWNLOAD OK lines and Winget version
  - `install.log` - install command and output (MUST always exist)
  - `result.json` - status PASS and completedAt timestamp
  - `DONE.txt` - completion sentinel

#### Scenario: result.json required fields

- **WHEN** result.json is written
- **THEN** it MUST contain:
  - `status` - "PASS" or "FAIL"
  - `startedAt` - ISO 8601 timestamp
  - `completedAt` - ISO 8601 timestamp
  - `wingetVersion` - version string from winget --version
  - `installExitCode` - integer exit code from winget install
  - `installCommand` - single-line string (no embedded newlines)
  - `postInstallSmokeOk` - boolean indicating smoke test passed
  - `postInstallSmokeOutput` - string with smoke test output (last ~2000 chars)
  - `policyBlockDetected` - boolean indicating WDAC/Smart App Control block
  - `policyBlockEvidence` - string with evidence if policy block detected

#### Scenario: winget-bootstrap.log content

- **WHEN** winget bootstrap succeeds
- **THEN** `winget-bootstrap.log` contains:
  - At least one `DOWNLOAD OK` line per successful download
  - `Winget version:` line with version string

### Requirement: Deterministic Sandbox Teardown

The system MUST terminate the Windows Sandbox session after run completion to prevent resource leaks and multi-session interference.

#### Scenario: Post-run cleanup on PASS

- **WHEN** validation completes with PASS (DONE.txt produced)
- **THEN** the host script MUST call the teardown helper before exiting
- **AND** no sandbox processes remain running after script exit

#### Scenario: Post-run cleanup on FAIL

- **WHEN** validation completes with FAIL (ERROR.txt produced)
- **THEN** the host script MUST call the teardown helper before exiting
- **AND** no sandbox processes remain running after script exit

#### Scenario: Post-run cleanup on timeout

- **WHEN** validation times out without producing DONE.txt or ERROR.txt
- **THEN** the host script MUST call the teardown helper before exiting
- **AND** no sandbox processes remain running after script exit

#### Scenario: Pre-run guard closes existing session

- **WHEN** the host script detects an existing sandbox session before launch
- **THEN** the host script MUST close the existing session using the teardown helper
- **AND** then proceed to launch the new validation run

#### Scenario: Teardown process detection

- **WHEN** the teardown helper checks for running sandbox processes
- **THEN** it MUST check for the following process names:
  - `WindowsSandboxRemoteSession` (primary on Windows 11 24H2+)
  - `WindowsSandboxClient`, `WindowsSandboxServer`, `WindowsSandbox` (compat)
  - `VmmemWSB`, `vmmemWindowsSandbox`, `vmmemCmZygote` (VM fallback)

#### Scenario: Primary teardown path

- **WHEN** `WindowsSandboxRemoteSession` is running
- **THEN** the teardown helper MUST stop it first via `Stop-Process -Force`
- **AND** allow a brief grace period for VM processes to terminate automatically
- **AND** then force-stop any remaining sandbox processes as fallback

#### Scenario: Acceptable lingering process

- **WHEN** teardown completes
- **AND** only `vmmemCmZygote` remains with 0 working set
- **THEN** this is NOT treated as a teardown failure
- **AND** the script exits normally

#### Acceptance Criteria: Verify no processes remain

To verify deterministic teardown:
1. Run: `.\scripts\sandbox-validate.ps1 -AppId git`
2. Wait for script to complete (PASS or FAIL)
3. Run: `Get-Process | ? { $_.Name -match 'sandbox|wsb|wdag|vmmem' } | select Name,Id`
4. Expected: No processes returned, OR only `vmmemCmZygote` with 0 working set (orphaned VM stub)

#### Scenario: Host proof summary output

- **WHEN** validation completes (PASS or FAIL)
- **THEN** the host script MUST print a proof summary including:
  - `startedAt` timestamp
  - `completedAt` timestamp
  - `wingetVersion`
  - `installExitCode`
  - `postInstallSmokeOk`
  - `policyBlockDetected`
  - `status` (PASS/FAIL)

### Requirement: Post-Install Smoke Test

The system MUST verify that the installed application can execute without policy blocks after winget install completes.

#### Scenario: Smoke test execution

- **WHEN** winget install completes successfully
- **THEN** the system MUST run a smoke test stage before proceeding to seed
- **AND** for git: run `git --version`, `where.exe git`, and optionally `bash.exe --version`
- **AND** capture all output and exit codes in `smoke.log`

#### Scenario: Smoke test passes

- **WHEN** all smoke test commands succeed without policy block indicators
- **THEN** `postInstallSmokeOk` is set to `true` in result.json
- **AND** validation continues to the seed stage

#### Scenario: PASS requires smoke test success

- **WHEN** validation completes with status PASS
- **THEN** `postInstallSmokeOk` MUST be `true`
- **AND** `policyBlockDetected` MUST be `false`

### Requirement: WDAC/Smart App Control Detection

The system MUST detect and fail explicitly when Windows security policies block application execution.

#### Scenario: Policy block patterns

- **WHEN** smoke test output contains any of these patterns:
  - `0xC0E90002` (WDAC block code)
  - `Code Integrity`
  - `blocked`
  - `cannot verify publisher`
  - `Bad Image`
  - `not allowed to run`
  - `Windows Defender Application Control`
  - `Smart App Control`
  - `AppLocker`
- **THEN** `policyBlockDetected` is set to `true`
- **AND** `policyBlockEvidence` contains the matched pattern and context

#### Scenario: Policy block causes FAIL

- **WHEN** `policyBlockDetected` is `true`
- **THEN** validation MUST fail with status "FAIL"
- **AND** `ERROR.txt` MUST be written with policy block details
- **AND** the error message MUST explain WDAC/Smart App Control as the likely cause

#### Scenario: Required artifacts include smoke.log

- **WHEN** validation completes (PASS or FAIL)
- **THEN** `smoke.log` MUST exist in the output directory
- **AND** it MUST contain the smoke test commands executed and their output

