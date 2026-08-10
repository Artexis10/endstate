## MODIFIED Requirements

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

#### Scenario: Push reports success only for a committed generation
- **WHEN** `endstate backup push` finishes uploading a generation to Hosted Backup
- **THEN** the engine reports success only if the version was committed and is therefore durable
- **AND** a generation whose upload or commit did not complete is never reported as protected
- **AND** the failure remediation describes the generation's actual state rather than an assumed cleanup

## ADDED Requirements

### Requirement: Cloud Recovery Is Verified By A Documented Release Drill

Hosted Backup's end-to-end recovery path SHALL be verified before release by a documented, deterministic drill with explicit pass/fail criteria, executed against a staging backend on a clean machine. A full clean-machine restore cannot run in ordinary CI — the failure modes it catches are invisible on any machine that has already pushed — so the drill is a release gate rather than a per-commit check, and it complements rather than replaces the hermetic unit suite.

The drill SHALL cover the complete sequence — signup, capture, push, server-side confirmation that the version is committed, total wipe of the machine, recovery using only the email address and the 24-word BIP39 phrase, pull, and a byte-level comparison against the original — and SHALL pass only when the restored tree is byte-identical to the captured one.

#### Scenario: Drill proves recovery from nothing but email and recovery phrase
- **WHEN** the drill wipes the machine and runs `endstate backup recover` with only the email address and the 24-word recovery phrase
- **THEN** the session is re-established and the data-encryption key is unwrapped
- **AND** the subsequent pull restores the backed-up generation

#### Scenario: Drill confirms the version is committed before wiping
- **WHEN** the drill lists the backup's versions after pushing
- **THEN** the pushed versionId is present in the listing
- **AND** the listing reports exactly one version for a single push
- **AND** the version carries a non-empty manifest hash

#### Scenario: Drill fails when the restored bytes differ
- **WHEN** the restored tree differs from the original in any file path or any file's SHA-256
- **THEN** the drill result is FAIL
- **AND** the release is blocked
