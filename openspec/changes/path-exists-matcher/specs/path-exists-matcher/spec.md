## ADDED Requirements

### Requirement: pathExists Matcher in Config Module Matches
Config modules SHALL support an optional `pathExists` array in the `matches` object. Each entry is a path string with environment variable placeholders expanded at runtime. If any path in the array exists on the filesystem, the module matches with reason `pathExists:<pattern>`.

#### Scenario: Match when path exists
- **WHEN** a config module has `matches.pathExists` containing a path that exists on disk
- **THEN** the module SHALL appear in matching results with matchReason `pathExists:<expanded-path>`

#### Scenario: No match when no paths exist
- **WHEN** a config module has `matches.pathExists` containing paths that do not exist
- **AND** no other matchers (winget, exe, uninstallDisplayName) match
- **WHEN** module matching is evaluated
- **THEN** the module SHALL NOT appear in results

#### Scenario: Environment variable expansion
- **WHEN** a config module has `matches.pathExists` containing `["%APPDATA%\\SomeApp\\config.xml"]`
- **WHEN** matching is evaluated
- **THEN** `%APPDATA%` SHALL be expanded to the actual env var value before checking existence

#### Scenario: Multiple paths — any match is sufficient
- **WHEN** a config module has `matches.pathExists` containing two paths and only the second exists
- **THEN** the module SHALL match via the existing path (early exit after first match)

#### Scenario: Empty pathExists array does not match
- **WHEN** a config module has `matches.pathExists` as an empty array `[]`
- **THEN** the module SHALL NOT match via pathExists

#### Scenario: pathExists is additive — coexists with other matchers
- **WHEN** a config module has both `matches.winget` and `matches.pathExists` and both match
- **THEN** both match reasons SHALL be reported: `winget:<id>` and `pathExists:<path>`

### Requirement: pathExists is optional in module schema
Config modules WITHOUT a `pathExists` field SHALL continue to match exactly as before. The field is optional and schema validation SHALL accept modules that omit it.

#### Scenario: Module without pathExists is unaffected
- **WHEN** a config module has no `matches.pathExists` field
- **THEN** module matching behaves identically to before this capability was introduced
- **AND** schema validation SHALL NOT reject the module
