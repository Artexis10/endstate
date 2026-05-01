## MODIFIED Requirements

### Requirement: Config module matching bundles captured configs into zip
After the app scan, the engine SHALL match each captured app against the config module catalog via `matches.winget` AND `matches.pathExists`, and copy matched modules' `capture.files` into the zip under `configs/<module-id>/`. A module is included if it matches by any matcher.

#### Scenario: Winget-matched module files are bundled
- **WHEN** a captured app has a matching config module via `matches.winget`
- **THEN** the module's `capture.files` are copied into the zip at `configs/<module-id>/<filename>`

#### Scenario: pathExists-matched module files are bundled
- **WHEN** an app was not matched via winget but its config module has `matches.pathExists` pointing to an existing path
- **THEN** the module's `capture.files` are also copied into the zip at `configs/<module-id>/<filename>`

#### Scenario: Sensitive files are never bundled
- **WHEN** a config module's `sensitive.files` list contains a file that would otherwise be captured
- **THEN** that file SHALL NOT appear in the zip

#### Scenario: ExcludeGlobs are respected
- **WHEN** a file matches a pattern in `capture.excludeGlobs`
- **THEN** that file SHALL be skipped and SHALL NOT appear in the zip
