# path-exists-matcher Specification

## Purpose
Enable config module matching for apps not discoverable via winget, PATH executables, or uninstall registry entries. The `pathExists` matcher checks filesystem paths for known application artifacts (executables, config files) and matches the module when any specified path exists.

## Requirements
### Requirement: pathExists Matcher in Config Module Matches

Config modules SHALL support an optional `pathExists` array in the `matches` object. Each entry is a path string containing environment variables (expanded at runtime). If any path in the array exists on the filesystem, the module matches with reason `pathExists:<pattern>`.

#### Scenario: Match when path exists

- **GIVEN** a config module with `matches.pathExists` containing `["%ProgramFiles%\\Adobe\\Adobe Lightroom Classic\\lightroom.exe"]`
- **AND** that file exists on disk
- **WHEN** module matching is evaluated (via `modules.MatchModulesForApps` in `go-engine/internal/modules/matcher.go`)
- **THEN** the module appears in results with matchReason `pathExists:%ProgramFiles%\Adobe\Adobe Lightroom Classic\lightroom.exe`

#### Scenario: No match when no paths exist

- **GIVEN** a config module with `matches.pathExists` containing paths that do not exist
- **AND** no other matchers (winget, exe, uninstallDisplayName) match
- **WHEN** module matching is evaluated
- **THEN** the module does not appear in results

#### Scenario: Environment variable expansion

- **GIVEN** a config module with `matches.pathExists` containing `["%APPDATA%\\SomeApp\\config.xml"]`
- **WHEN** matching is evaluated
- **THEN** `%APPDATA%` is expanded via environment variable expansion before checking existence

#### Scenario: Multiple paths — any match is sufficient

- **GIVEN** a config module with `matches.pathExists` containing two paths
- **AND** only the second path exists
- **WHEN** module matching is evaluated
- **THEN** the module matches with one matchReason for the existing path
- **AND** only the first matching path is reported (early exit)

#### Scenario: Empty pathExists array

- **GIVEN** a config module with `matches.pathExists` as an empty array `[]`
- **WHEN** module matching is evaluated
- **THEN** the module does not match via pathExists (no paths to check)

#### Scenario: Coexistence with other matchers

- **GIVEN** a config module with both `matches.winget` and `matches.pathExists`
- **AND** the app is installed via winget AND the pathExists path exists
- **WHEN** module matching is evaluated
- **THEN** both match reasons are reported: `winget:<id>` and `pathExists:<path>`

## Invariants

### INV-PATHEXISTS-1: Optional Field
- `pathExists` is optional in the `matches` object
- Modules without `pathExists` continue to work exactly as before
- Schema validation accepts but does not require `pathExists`

### INV-PATHEXISTS-2: Path Expansion
- All paths in `pathExists` are expanded via environment variable expansion before checking existence
- Supports `%VAR%` syntax and `~` home expansion

### INV-PATHEXISTS-3: Additive Matching
- `pathExists` matches are OR'd with other matcher results (winget, exe, uninstallDisplayName)
- A module matches if ANY matcher produces a result

### INV-PATHEXISTS-4: Early Exit
- When multiple paths are specified, matching stops at the first existing path
- Only one `pathExists` match reason is reported per module

## Affected Components
- `modules.MatchModulesForApps` in `go-engine/internal/modules/matcher.go` — pathExists matching block
- `modules.LoadCatalog` in `go-engine/internal/modules/catalog.go` — module schema validation (pathExists must be array of strings)
