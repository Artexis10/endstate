## ADDED Requirements

### Requirement: Sandbox Validation Evidence Classification

The legacy Windows Sandbox harness SHALL identify direct-copy capture/restore results as curation evidence and SHALL NOT claim production-engine proof unless it invokes the workflow-built engine and satisfies the verified module matrix evidence contract.

#### Scenario: Legacy Sandbox harness reports PASS

- **WHEN** the Sandbox harness performs capture, restore, or verification through its own PowerShell implementation
- **THEN** the result MAY report `harness-validated` or `curation-validated`
- **AND** it SHALL NOT report `engine-contract`, `config-roundtrip-v1`, `config-roundtrip-v2`, `live-install`, or `live-config-roundtrip`

#### Scenario: Sandbox result contains skipped or zero operations

- **WHEN** a Sandbox run skips an unsupported restore/verifier operation or captures/restores zero selected settings
- **THEN** it SHALL NOT be promoted to a passing verified-module-matrix proof level

#### Scenario: Sandbox invokes the production engine

- **WHEN** a future Sandbox path invokes the workflow-built engine for the full required journey
- **THEN** it MAY claim only the proof levels whose non-vacuous assertions and evidence fields satisfy the verified module matrix contract
