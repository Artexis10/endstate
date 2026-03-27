# capture-zero-apps-failure Specification

## Purpose
Ensures that a capture operation that finds zero apps to capture is treated as a failure, not a silent success.

## Requirements
### Requirement: Zero Apps Captured Is a Failure

The capture command SHALL treat a result of zero captured apps as an error condition, not a success.

#### Scenario: Capture with no matching apps fails
- **WHEN** `capture` is run and no apps match the capture criteria
- **THEN** the command exits with a non-zero exit code
- **AND** an error message indicates that zero apps were found to capture

#### Scenario: Capture retries before failing
- **WHEN** `capture` initially finds zero apps to capture
- **THEN** the system retries the discovery step before reporting failure
- **AND** the retry count and outcome are logged

#### Scenario: Capture with at least one app succeeds normally
- **WHEN** `capture` is run and at least one app matches the capture criteria
- **THEN** the command proceeds with the capture workflow
- **AND** no retry-on-zero logic is triggered

### Requirement: JSON Envelope Reflects Zero-App Failure

When capture fails due to zero apps, the JSON envelope SHALL clearly communicate the failure reason.

#### Scenario: JSON output on zero-app capture
- **WHEN** `capture --json` is run and zero apps are found after retries
- **THEN** the JSON envelope status indicates failure
- **AND** the envelope includes a reason field indicating zero apps were captured
