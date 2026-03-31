## ADDED Requirements

### Requirement: CI enforces VERSION file matches release-please manifest

The CI pipeline SHALL fail if the VERSION file contents do not match the version in `.release-please-manifest.json`.

#### Scenario: VERSION matches manifest

- **GIVEN** `VERSION` contains `1.7.2`
- **AND** `.release-please-manifest.json` contains `{ ".": "1.7.2" }`
- **WHEN** CI runs the version drift check
- **THEN** the check passes

#### Scenario: VERSION drifted from manifest

- **GIVEN** `VERSION` contains `1.5.1`
- **AND** `.release-please-manifest.json` contains `{ ".": "1.7.2" }`
- **WHEN** CI runs the version drift check
- **THEN** the check fails with a message identifying both values
