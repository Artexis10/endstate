## ADDED Requirements

### Requirement: Restore Target Selects a Specific Detected Instance
Apply, standalone restore, and rebuild SHALL support repeatable `--restore-target <captureId>=<targetInstanceId>` arguments for resolving ambiguous generation-aware captures. Module-level `--restore-filter` behavior SHALL remain unchanged and SHALL apply before per-capture target mapping.

#### Scenario: Explicit target selects one side-by-side instance
- **WHEN** two compatible target instances exist and the caller supplies a valid `--restore-target` mapping
- **THEN** only the mapped instance receives that captured config set

#### Scenario: No explicit target for unambiguous set
- **WHEN** exactly one viable target instance exists
- **THEN** restore may proceed without `--restore-target`

#### Scenario: Invalid target mapping fails preflight
- **WHEN** a mapping is malformed, duplicates a capture mapping, or references an unknown capture ID
- **THEN** the command returns an input error before installation or config mutation

#### Scenario: Mapped target cannot be used after install
- **WHEN** a syntactically valid target ID is absent or incompatible after final post-install detection
- **THEN** only the affected config set is skipped with `mapped_target_not_detected` or `mapped_target_incompatible`
- **AND** successful application installation remains intact

#### Scenario: Module filter excludes mapped capture
- **WHEN** `--restore-filter` excludes the module owning a supplied target mapping
- **THEN** that capture remains excluded
- **AND** the mapping does not bypass the module filter

#### Scenario: Capabilities advertise restore-target
- **WHEN** `capabilities --json` is run
- **THEN** the apply, restore, and rebuild command capabilities include `--restore-target`
