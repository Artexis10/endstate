## MODIFIED Requirements

### Requirement: Restore Requires Explicit Flag

Restore operations SHALL NOT execute unless the user has given explicit command-line consent: the `-EnableRestore` flag for `apply` and `restore`, or the `--confirm` flag for `rebuild`. `rebuild` exists to perform a full restore-enabled rebuild, so its consent covers the whole run: without `--confirm`, a live rebuild SHALL be refused before any mutation; `--no-restore` opts a rebuild out of restore entirely.

#### Scenario: Apply without EnableRestore skips all restore entries
- **WHEN** `apply` is run without `--EnableRestore`
- **THEN** all restore entries in the manifest are skipped
- **AND** no config files are written to disk by the restore stage

#### Scenario: Apply with EnableRestore executes restore entries
- **WHEN** `apply --EnableRestore` is run
- **THEN** restore entries in the manifest are executed
- **AND** config files are written according to restore strategies

#### Scenario: Standalone restore requires EnableRestore
- **WHEN** `restore` is run without `--EnableRestore`
- **THEN** no restore entries are executed
- **AND** the command exits without modifying config files

#### Scenario: Live rebuild requires confirm
- **WHEN** `rebuild --from <path>` is run without `--confirm`, `--dry-run`, or `--no-restore`
- **THEN** the run is refused with `CONFIRMATION_REQUIRED` before any mutation
- **AND** no restore entries are executed

#### Scenario: Rebuild without restore needs no consent
- **WHEN** `rebuild --from <path> --no-restore` is run
- **THEN** apps are installed and verified but no restore entries are executed

### Requirement: Flag Cannot Be Defaulted or Inferred

The `-EnableRestore` flag SHALL NOT be set by default, by environment variable, or by manifest content. `rebuild`'s `--confirm` consent SHALL likewise be given only explicitly on the command line — never defaulted, inferred from the environment, or supplied by manifest or bundle content.

#### Scenario: Environment variable does not enable restore
- **WHEN** `apply` is run without `--EnableRestore` but with any environment variables set
- **THEN** restore is not triggered
- **AND** only the explicit CLI flag activates restore

#### Scenario: Bundle content cannot self-authorize a rebuild
- **WHEN** `rebuild --from <bundle.zip>` is run without `--confirm` against a bundle whose manifest declares restore entries
- **THEN** the run is refused with `CONFIRMATION_REQUIRED`
- **AND** nothing in the bundle can substitute for the command-line flag
