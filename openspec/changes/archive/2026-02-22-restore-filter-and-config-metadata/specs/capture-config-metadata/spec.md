## ADDED Requirements

### Requirement: Rich Config Capture Metadata

The capture command SHALL include per-module metadata in its output to enable GUI display of module-level config information.

#### Scenario: configCapture.modules in capture result

- **WHEN** `capture --WithConfig --json` is run with matched config modules
- **THEN** the result includes a configCapture object with a modules array
- **AND** each module entry includes: id (string), displayName (string), entries (integer), files (array of strings)

#### Scenario: displayName falls back to module ID

- **WHEN** a config module is captured
- **THEN** the displayName field uses the module's displayName from module.jsonc
- **AND** displayName is always present (it is a required field in the module schema)

#### Scenario: entries count reflects restore entry count

- **WHEN** a config module with 3 restore entries is captured
- **THEN** the module entry in configCapture.modules has entries: 3

#### Scenario: files array lists source paths

- **WHEN** a config module with capture.files entries is captured
- **THEN** the files array lists the relative dest paths from capture.files
