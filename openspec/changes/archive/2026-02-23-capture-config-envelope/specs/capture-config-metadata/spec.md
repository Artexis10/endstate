## ADDED Requirements

### Requirement: configModules Array in Capture JSON Envelope

The capture command SHALL include a `configModules` array in the JSON envelope `data` object when bundle capture includes matched config modules, enabling GUI association of config modules with parent apps.

#### Scenario: configModules in capture JSON envelope

- **WHEN** `capture --WithConfig --json` produces a bundle with matched config modules
- **THEN** the JSON envelope `data` object contains a `configModules` array
- **AND** each element includes: id (string), appId (string), displayName (string), status (string), filesCaptured (integer), wingetRefs (string[])
- **AND** status is one of: "captured", "skipped", "error"
- **AND** wingetRefs contains the module's matches.winget array (may be empty)

#### Scenario: appId derived from module ID

- **WHEN** a config module with id "apps.vscode" is captured
- **THEN** the configModules entry has appId "vscode"
- **AND** appId is always the module id with "apps." prefix stripped

#### Scenario: wingetRefs from module matches

- **WHEN** a config module has matches.winget = ["Microsoft.VisualStudioCode"]
- **THEN** the configModules entry has wingetRefs = ["Microsoft.VisualStudioCode"]

- **WHEN** a config module has no matches.winget field
- **THEN** the configModules entry has wingetRefs = []

#### Scenario: configModules absent when no config capture

- **WHEN** `capture --json` is run WITHOUT `--WithConfig`
- **THEN** the JSON envelope `data` does NOT contain a `configModules` field

#### Scenario: configModules includes all matched modules regardless of status

- **WHEN** capture matches 3 modules but only 2 have files on disk
- **THEN** configModules contains 3 entries
- **AND** 2 have status "captured" and 1 has status "skipped"
