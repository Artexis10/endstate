## ADDED Requirements

### Requirement: Platform variants qualify schema-v3 generation identity
For schema-v3 modules, configuration generation identity SHALL include the explicit platform between module ID and config-set ID. Generation fingerprints and catalog-history validation SHALL cover the canonical platform-variant definition. Version evidence SHALL select generations only within the target platform variant unless an authored cross-platform mapping explicitly names another variant.

#### Scenario: Same logical set evolves differently by platform
- **WHEN** a portable module declares `preferences/g1` for Windows and `preferences/g1` for Linux
- **THEN** the identities SHALL be `<module>/windows/preferences/g1` and `<module>/linux/preferences/g1`
- **AND** a fingerprint accepted by one variant SHALL NOT be accepted by the other implicitly

#### Scenario: Target detection stays platform-scoped
- **WHEN** restore on Linux discovers package/path evidence for the Linux variant
- **THEN** it SHALL resolve only Linux generations by default
- **AND** Windows or Darwin declaration order SHALL not influence selection

### Requirement: Legacy module generations are Windows-only
Schema-v1 and schema-v2 flat modules SHALL preserve their existing generation identities and behavior on Windows and SHALL be ineligible for automatic Linux or Darwin detection, capture, or restore.

#### Scenario: Legacy module loaded on Linux
- **WHEN** a Linux host loads the unchanged catalog containing a flat schema-v2 generation module
- **THEN** that module SHALL not emit Linux instances or configuration work
- **AND** the engine SHALL not fabricate a Linux platform identity for it
