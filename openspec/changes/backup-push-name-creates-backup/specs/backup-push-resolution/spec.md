## ADDED Requirements

### Requirement: backup push target resolution

`backup push` SHALL resolve the backup it writes a version to as follows: an explicit `--backup-id` is used verbatim; otherwise a non-empty `--name` SHALL create a new backup labeled with that name; otherwise (no id and no name) it SHALL append to the user's first existing backup, or create a backup named `default` if the user has none. A non-empty `--name` with no `--backup-id` SHALL NOT append to a pre-existing backup.

#### Scenario: Explicit backup id is used verbatim
- **WHEN** `backup push --backup-id <id>` is invoked
- **THEN** the version is written to backup `<id>` without listing or creating backups

#### Scenario: Named push with existing backups creates a new backup
- **WHEN** `backup push --name <label>` (no `--backup-id`) is invoked and the account already has one or more backups
- **THEN** a new backup labeled `<label>` is created and its id returned
- **AND** no existing backup receives the version

#### Scenario: Named push on an empty account creates the named backup
- **WHEN** `backup push --name <label>` is invoked and the account has no backups
- **THEN** a new backup labeled `<label>` is created

#### Scenario: No id and no name appends to the first backup
- **WHEN** `backup push` is invoked with neither `--backup-id` nor `--name` and at least one backup exists
- **THEN** the version is appended to the first backup

#### Scenario: No id, no name, no backups creates a default
- **WHEN** `backup push` is invoked with neither `--backup-id` nor `--name` and the account has no backups
- **THEN** a new backup labeled `default` is created
