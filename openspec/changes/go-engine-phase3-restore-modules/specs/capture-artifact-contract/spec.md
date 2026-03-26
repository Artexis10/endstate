## ADDED Requirements

### Requirement: Go engine capture produces artifacts matching capture contract
The Go engine capture command SHALL produce bundles that satisfy all capture artifact contract invariants: INV-CAPTURE-2 (manifest file exists and is non-empty), INV-CAPTURE-3 (manifest contains apps array), INV-BUNDLE-1 (zip is self-contained), INV-BUNDLE-3 (config failures don't block app capture).

#### Scenario: Go capture bundle satisfies self-contained invariant
- **WHEN** the Go engine creates a capture bundle with config modules
- **THEN** the zip contains all referenced config files and no external file references exist
