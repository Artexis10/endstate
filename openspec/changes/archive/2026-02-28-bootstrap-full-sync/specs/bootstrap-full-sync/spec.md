## ADDED Requirements

### Requirement: Bootstrap copies all runtime directories
`endstate bootstrap` SHALL copy all six runtime directories from the source repo to the bootstrap destination: `engine/`, `modules/`, `payload/`, `restorers/`, `drivers/`, `verifiers/`.

#### Scenario: drivers/ and verifiers/ are present after bootstrap
- **WHEN** `endstate bootstrap -RepoRoot <repo-path>` is executed
- **THEN** `%LOCALAPPDATA%\Endstate\bin\drivers\` SHALL exist and contain all files from `<repo-path>\drivers\`
- **AND** `%LOCALAPPDATA%\Endstate\bin\verifiers\` SHALL exist and contain all files from `<repo-path>\verifiers\`

#### Scenario: All six directories are copied
- **WHEN** `endstate bootstrap -RepoRoot <repo-path>` is executed
- **THEN** `engine/`, `modules/`, `payload/`, `restorers/`, `drivers/`, `verifiers/` SHALL all be present under `%LOCALAPPDATA%\Endstate\bin\`

### Requirement: Bootstrap always force-overwrites directory contents
Bootstrap SHALL unconditionally overwrite all synced directories. It MUST NOT skip files based on existence or timestamp. When source and destination resolve to different paths, the destination directory SHALL be removed and replaced entirely.

#### Scenario: Re-bootstrap overwrites stale files
- **WHEN** a file in `%LOCALAPPDATA%\Endstate\bin\engine\` has been modified since last bootstrap
- **AND** `endstate bootstrap -RepoRoot <repo-path>` is executed
- **THEN** the modified file SHALL be replaced with the repo version

#### Scenario: Self-copy detection preserves running script
- **WHEN** bootstrap is invoked from the installed location (source path == destination path)
- **THEN** directory copy SHALL be skipped (files are already in place)
- **AND** the entrypoint `endstate.ps1` self-copy guard SHALL remain unchanged

### Requirement: Bootstrap reports copy statistics
After completing all file copies, bootstrap SHALL display a summary reporting the total number of files copied and the number of directories synced.

#### Scenario: Successful bootstrap shows file count
- **WHEN** `endstate bootstrap -RepoRoot <repo-path>` completes successfully
- **THEN** output SHALL include a line matching pattern `[SYNC] Copied N files across M directories`

#### Scenario: Copy failure is reported
- **WHEN** a file copy fails during bootstrap (e.g., permission denied)
- **THEN** bootstrap SHALL report the failure with the file path and error message
- **AND** bootstrap SHALL continue copying remaining files (not abort on first failure)

### Requirement: Bootstrapped copy produces identical output to repo copy
After bootstrap, running `endstate <command>` from PATH SHALL produce identical functional output to running `$env:ENDSTATE_ALLOW_DIRECT='1'; <repo>\bin\endstate.ps1 <command>` from the repo.

#### Scenario: capabilities --json matches after bootstrap
- **WHEN** `endstate bootstrap -RepoRoot <repo-path>` has completed
- **AND** `endstate capabilities --json` is run from PATH
- **THEN** the JSON `data` field SHALL match the output of `$env:ENDSTATE_ALLOW_DIRECT='1'; <repo>\bin\endstate.ps1 capabilities --json`

#### Scenario: configModuleMap present in dry-run after bootstrap
- **WHEN** `endstate bootstrap -RepoRoot <repo-path>` has completed
- **AND** `endstate apply --profile <name> --dry-run --json` is run on a manifest with configModules
- **THEN** the JSON envelope `data.configModuleMap` SHALL be present and non-null
