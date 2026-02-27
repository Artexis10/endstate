## ADDED Requirements

### Requirement: Capabilities response includes gitCommit
The capabilities `data` object SHALL include a `gitCommit` field containing the short git SHA of HEAD, or `$null` if git is unavailable.

#### Scenario: Git available in repo
- **WHEN** `Get-CapabilitiesData` is called and git is available
- **THEN** `data.gitCommit` SHALL be a non-empty string matching the short SHA of the current HEAD commit

#### Scenario: Git not available
- **WHEN** `Get-CapabilitiesData` is called and git is not on PATH or the directory is not a git repo
- **THEN** `data.gitCommit` SHALL be `$null`

### Requirement: Capabilities response includes gitDirty
The capabilities `data` object SHALL include a `gitDirty` boolean indicating whether the working tree has uncommitted changes.

#### Scenario: Clean working tree
- **WHEN** `Get-CapabilitiesData` is called and `git status --porcelain` returns empty output
- **THEN** `data.gitDirty` SHALL be `$false`

#### Scenario: Dirty working tree
- **WHEN** `Get-CapabilitiesData` is called and `git status --porcelain` returns non-empty output
- **THEN** `data.gitDirty` SHALL be `$true`

#### Scenario: Git not available for dirty check
- **WHEN** `Get-CapabilitiesData` is called and git is not available
- **THEN** `data.gitDirty` SHALL be `$false`

### Requirement: Capabilities response includes bootstrapTimestamp
The capabilities `data` object SHALL include a `bootstrapTimestamp` field containing the ISO 8601 UTC timestamp of when the engine was last bootstrapped, or `$null` if no bootstrap info exists.

#### Scenario: Bootstrap info file exists
- **WHEN** `Get-CapabilitiesData` is called and `engine/version-info.json` exists with a `bootstrapTimestamp` field
- **THEN** `data.bootstrapTimestamp` SHALL be the timestamp string from that file

#### Scenario: Bootstrap info file missing
- **WHEN** `Get-CapabilitiesData` is called and `engine/version-info.json` does not exist
- **THEN** `data.bootstrapTimestamp` SHALL be `$null`

### Requirement: New fields are documented in CLI JSON contract
The `docs/contracts/cli-json-contract.md` capabilities response section SHALL document `gitCommit`, `gitDirty`, and `bootstrapTimestamp` fields with their types and nullable behavior.

#### Scenario: Contract documentation updated
- **WHEN** a developer reads `docs/contracts/cli-json-contract.md`
- **THEN** the capabilities response example and field table SHALL include `gitCommit`, `gitDirty`, and `bootstrapTimestamp`
