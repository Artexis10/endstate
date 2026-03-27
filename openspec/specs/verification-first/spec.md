# verification-first Specification

## Purpose
Defines that success in Endstate is determined by observable post-execution state, not by whether a command appeared to run without errors.

## Requirements
### Requirement: Success Means Verified State

A command SHALL report success only when post-execution verification confirms the desired state is achieved, not merely that the command exited zero.

#### Scenario: Apply verifies package presence after install
- **WHEN** `apply` completes an install operation for a package
- **THEN** the verifier confirms the package is present on the system
- **AND** the apply result for that entry reflects the verification outcome, not just the install exit code

#### Scenario: Restore verifies file state after copy
- **WHEN** `apply --EnableRestore` completes a file restore operation
- **THEN** the verifier confirms the target file exists and matches expectations
- **AND** a failed verification is reported even if the copy command succeeded

### Requirement: Verification Failures Are Surfaced

Failed verifications SHALL be clearly reported and SHALL NOT be silently swallowed.

#### Scenario: Failed verification appears in output
- **WHEN** a verifier detects that expected state was not achieved
- **THEN** the failure is included in the command output
- **AND** the overall command result reflects the failure

#### Scenario: JSON envelope includes verification results
- **WHEN** `apply --json` is run
- **THEN** the JSON envelope contains per-entry verification status
- **AND** entries with failed verification are distinguishable from those that passed
