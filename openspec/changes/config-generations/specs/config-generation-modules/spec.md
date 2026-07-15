## ADDED Requirements

### Requirement: Module Schema Supports Optional Configuration Generations
The module loader SHALL accept existing unversioned modules as schema v1 and SHALL accept generation-aware modules only when they declare `moduleSchemaVersion: 2`. Schema-v1 modules SHALL remain loadable without modification and SHALL be classified as unversioned rather than assigned a trusted generation.

#### Scenario: Existing flat module remains valid
- **WHEN** the catalog loads a current module without `moduleSchemaVersion`
- **THEN** the module loads through the schema-v1 adapter
- **AND** its existing capture, restore, verify, and secrets definitions retain their behavior
- **AND** the module is marked unversioned for compatibility reporting

#### Scenario: Generation block requires schema v2
- **WHEN** a module declares config generations without `moduleSchemaVersion: 2`
- **THEN** catalog validation rejects the module with a structured schema error

### Requirement: Generations Are Scoped Per Config Set
A schema-v2 module SHALL declare one or more config sets, each with a stable ID and one or more stable generations. Generation identity SHALL be the tuple `<moduleId>/<configSetId>/<generationId>` and SHALL NOT imply compatibility with a generation in another config set.

#### Scenario: Config sets evolve independently
- **WHEN** an application instance maps `preferences` to `g2` and `presets` to `g1`
- **THEN** the engine records and resolves those generations independently
- **AND** a migration decision for `preferences` does not alter the decision for `presets`

#### Scenario: Generation identity is never inferred across sets
- **WHEN** two config sets both contain a generation named `g1`
- **THEN** the engine treats them as distinct generations

### Requirement: Generation IDs and Order Are Stable
Each generation SHALL declare a positive integer `order` and SHALL have an engine-computed canonical fingerprint. Released generation IDs SHALL be immutable and SHALL NOT be reused with different capture, restore, validation, or semantic meaning. A changed definition SHALL explicitly list accepted historical source fingerprints before the current catalog can interpret captures made from them. Catalog history validation SHALL reject silent reuse. Order SHALL establish forward direction only and SHALL NOT create implicit compatibility or migration edges.

#### Scenario: Higher order without an edge is not migratable
- **WHEN** source generation `g1` has order 1 and target generation `g2` has order 2 but no migration edge connects them
- **THEN** the engine does not infer a migration
- **AND** resolution reports `migration_path_missing`

#### Scenario: Same ID with changed definition is not silently accepted
- **WHEN** a released generation ID has a different current fingerprint and does not explicitly accept the captured historical fingerprint
- **THEN** catalog/history validation or restore resolution rejects silent reinterpretation
- **AND** restore reason is `source_generation_definition_changed`

#### Scenario: Historical fingerprint is explicitly accepted
- **WHEN** a current generation explicitly accepts a captured historical fingerprint
- **THEN** the engine may use current trusted rules for that historical source identity
- **AND** reports both the captured fingerprint and current module revision

### Requirement: Engine-Owned Instance Detection Supports Package and Versioned Paths
Schema-v2 modules SHALL declare instance detectors from the engine-supported allowlist. The first release SHALL support `package` detectors and `path` detectors with engine-expanded globs and optional named version extraction. Detector results SHALL be normalized, deduplicated, deterministically ordered, and assigned stable instance IDs.

#### Scenario: Package detector preserves version evidence
- **WHEN** a matched package record includes a backend, package ref, and installed version
- **THEN** the detector emits an instance containing the backend/ref, raw version, normalized version when parseable, and a stable instance ID

#### Scenario: Path detector expands side-by-side versions
- **WHEN** a path detector glob matches two versioned configuration roots
- **THEN** the engine emits two separate instances
- **AND** capture does not silently select the lexically newest or highest-version path

#### Scenario: Wildcard is not treated literally
- **WHEN** a path detector contains an engine-supported wildcard
- **THEN** the engine expands the wildcard before existence checks and capture

### Requirement: Version Evidence Selects Exactly One Generation
Generation matching SHALL operate on preserved raw version evidence and an explicitly normalized numeric dotted version when available. A config set SHALL resolve only when exactly one generation rule matches. Declaration order SHALL NOT break ties.

#### Scenario: Exactly one version range matches
- **WHEN** an instance version is `27.4` and exactly one generation declares a matching numeric range
- **THEN** the engine selects that generation

#### Scenario: No generation matches
- **WHEN** no generation rule matches an instance's version evidence
- **THEN** the engine reports reason `unknown_generation`
- **AND** does not select a fallback generation

#### Scenario: Multiple generation rules match
- **WHEN** more than one generation rule matches the same config set and instance
- **THEN** the engine reports reason `ambiguous_generation`
- **AND** does not use declaration order as a tie-breaker

### Requirement: Instance Placeholders Are Restricted
Generation capture and restore paths MAY use only documented instance placeholders. The engine SHALL reject unknown placeholders, placeholder expansion outside allowed roots, absolute portable destinations, and path traversal.

#### Scenario: Allowed instance root expands
- **WHEN** a path uses `${instance.root}` from the selected path detector
- **THEN** the engine expands it to that instance's detected root

#### Scenario: Traversal is rejected
- **WHEN** a generation definition attempts to escape a payload or staging root with `..`
- **THEN** catalog validation rejects the module before capture or restore

### Requirement: Module Revision Is Engine Computed
The engine SHALL compute the module revision as SHA-256 over canonical parsed module JSON with loader-only fields excluded and SHALL compute a canonical fingerprint for each generation definition. Comments, whitespace, and line endings SHALL NOT affect either value.

#### Scenario: Cosmetic JSONC edits preserve revision
- **WHEN** two module files parse to the same canonical data but differ only in comments, whitespace, or line endings
- **THEN** their computed module revisions are identical

#### Scenario: Semantic rule edit changes revision
- **WHEN** a generation, detector, migration, capture, restore, or validation rule changes
- **THEN** the computed module revision changes

#### Scenario: Generation definition edit changes fingerprint
- **WHEN** the canonical meaning of one generation changes
- **THEN** that generation's fingerprint changes
