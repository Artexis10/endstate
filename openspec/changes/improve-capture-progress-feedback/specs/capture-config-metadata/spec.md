## ADDED Requirements

### Requirement: Bundle and envelope metadata share one collection pass

When capture creates a settings bundle, the engine SHALL derive the bundle contents and capture-envelope config metadata from the same module collection results.

#### Scenario: Matched module is collected

- **WHEN** a matched module contributes files, registry keys, or registry values to the bundle
- **THEN** that module is collected exactly once during the capture run
- **AND** its envelope paths, filesCaptured count, status, and errors reflect the same collection result used for the bundle

#### Scenario: Matched module has no capturable data

- **WHEN** a matched module contributes no files, registry keys, or registry values
- **THEN** it appears once in configModules with status `skipped`
- **AND** the bundle metadata and envelope agree that it was skipped

#### Scenario: Sensitive values are excluded

- **WHEN** the collection pass excludes sensitive values
- **THEN** `counts.sensitiveExcludedCount` is derived from that pass
- **AND** capture does not repeat collection to recompute the count

#### Scenario: Bundle publication fails after collection

- **WHEN** settings collection succeeds but a later bundle publication step fails
- **THEN** capture retains the JSONC fallback artifact
- **AND** the envelope retains the available module report and capture errors from the completed collection pass
