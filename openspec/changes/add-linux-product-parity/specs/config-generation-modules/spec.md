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

### Requirement: Flat schema-v3 settings use generation provenance
A schema-v3 variant with capture or restore declarations and no authored `config` SHALL use the existing generation pipeline through the fixed shorthand identity `default/g1`. Its executable projection SHALL use one per-user path instance rooted at the home coordinate, retain the authored variant for fingerprinting and source evidence, and remove duplicate top-level capture and restore execution. This normalization SHALL NOT change schema-v1/v2 behavior.

#### Scenario: Flat variant captures multiple files
- **WHEN** a flat schema-v3 variant captures two live configuration files
- **THEN** capture SHALL produce one platform-qualified `default/g1` config capture containing both files
- **AND** it SHALL NOT publish those files through a legacy unverified lane

#### Scenario: Home location contains no configuration
- **WHEN** the shorthand detector finds the user home but collection finds no eligible settings payload
- **THEN** the engine SHALL NOT count that location as an installed application or a successful settings capture

#### Scenario: Authored generation identities remain authoritative
- **WHEN** a schema-v3 variant declares its own config sets and generations
- **THEN** the engine SHALL use those declarations without adding the shorthand generation

#### Scenario: Shorthand has no authored generation validation
- **WHEN** the selected authored schema-v3 variant has no `config` and normalizes to `default/g1`
- **THEN** its matrix scenario SHALL use `capture-contract` and emit only `catalog` and `engine-contract` proof
- **AND** exact payload, source snapshot, platform, fingerprint, content, and optional-absence assertions SHALL remain required
- **AND** an independent workflow test SHALL prove actual capture and restore bytes
- **AND** generation and migration scenarios SHALL retain their non-zero production-validation requirements

### Requirement: Legacy module generations are Windows-only
Schema-v1 and schema-v2 flat modules SHALL preserve their existing generation identities and behavior on Windows and SHALL be ineligible for automatic Linux or Darwin detection, capture, or restore.

#### Scenario: Legacy module loaded on Linux
- **WHEN** a Linux host loads the unchanged catalog containing a flat schema-v2 generation module
- **THEN** that module SHALL not emit Linux instances or configuration work
- **AND** the engine SHALL not fabricate a Linux platform identity for it
