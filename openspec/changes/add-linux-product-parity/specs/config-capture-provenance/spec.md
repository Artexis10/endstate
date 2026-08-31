## ADDED Requirements

### Requirement: Schema-v3 capture provenance records the selected platform variant
A config capture produced from a schema-v3 module SHALL record the non-empty source platform and the exact platform-qualified generation/module fingerprint used to collect it. The persisted evidence SHALL be sufficient to distinguish identically named config sets and generations in other variants without storing machine-local roots.

#### Scenario: Linux live config is captured
- **WHEN** a Linux schema-v3 variant captures a config set
- **THEN** the capture SHALL record source platform `linux`, the platform-qualified source generation, and the variant-covered fingerprint
- **AND** the module snapshot SHALL remain inspectable under the existing authority rules

#### Scenario: Platform evidence is missing or inconsistent
- **WHEN** a schema-v3 capture omits the platform or names a platform inconsistent with its generation/fingerprint
- **THEN** provenance validation SHALL reject the capture
- **AND** it SHALL NOT fall back to legacy unverified restore

### Requirement: Restore enforces same-platform provenance unless compatibility is authored
Restore SHALL resolve a schema-v3 capture through the same platform variant by default. A different target platform SHALL be eligible only when the trusted current module declares an explicit source-to-target mapping or migration that accepts the captured platform-qualified fingerprint. Restore SHALL report source and target variants in its resolution data.

#### Scenario: Same-platform fingerprint is accepted
- **WHEN** a valid Linux capture is restored through the compatible current Linux variant
- **THEN** normal generation resolution SHALL proceed
- **AND** resolution SHALL report source and target platform `linux`

#### Scenario: Cross-platform mapping is absent
- **WHEN** a valid Linux capture is offered to a Darwin or Windows target variant without authored compatibility
- **THEN** restore SHALL skip it with reason `platform_mapping_missing`
- **AND** it SHALL write no target configuration

#### Scenario: Explicit mapping accepts the source
- **WHEN** the trusted target variant declares a validated mapping from the captured source platform/generation/fingerprint
- **THEN** restore MAY materialize and validate the target representation
- **AND** the resolution record SHALL name the mapping plus both platform variants
