## ADDED Requirements

### Requirement: Per-package source survives provisioning history

Provisioning generations for package-driver applies SHALL retain source-aware added and removed package records in addition to the existing ref-only compatibility arrays.

#### Scenario: Source-aware package is installed

- **WHEN** apply installs a Winget package with a resolved source
- **THEN** the generation records its ref and normalized source in `addedPackages`
- **AND** the existing `addedRefs` array continues to record the ref

#### Scenario: Source-aware package is rolled back

- **WHEN** best-effort rollback removes a package using source-aware generation history
- **THEN** the rollback generation records its ref and source in `removedPackages`
- **AND** the existing `removedRefs` array continues to record the ref

#### Scenario: Legacy generation is read

- **WHEN** a generation omits source-aware package arrays
- **THEN** the engine continues to read its legacy ref arrays
- **AND** applies backend-specific compatibility source resolution before rollback
