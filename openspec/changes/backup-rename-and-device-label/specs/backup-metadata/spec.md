## ADDED Requirements

### Requirement: A backup's label is mutable via rename

The engine SHALL support changing a backup's display label by its id, without affecting the backup's identity (its backend id) or its versions. `backup rename --backup-id <id> --name <label>` SHALL issue `PATCH /api/backups/:id` with the new name and report the persisted result. A missing `--backup-id` or a blank `--name` SHALL be rejected before any backend call.

#### Scenario: Rename changes the label, not the identity
- **WHEN** `backup rename --backup-id <id> --name "Gaming Rig"` is invoked
- **THEN** the engine issues `PATCH /api/backups/<id>` with `{ name: "Gaming Rig" }`
- **AND** returns the backup's unchanged id and its new label

#### Scenario: Rename requires an id
- **WHEN** `backup rename` is invoked without `--backup-id`
- **THEN** it fails with a validation error and makes no backend call

#### Scenario: Rename requires a non-empty name
- **WHEN** `backup rename --backup-id <id> --name "   "` is invoked
- **THEN** it fails with a validation error and makes no backend call

### Requirement: backup push default name uses the device label

When `backup push` creates a backup because neither `--backup-id` nor `--name` was supplied AND the account has no existing backups, the engine SHALL label the new backup with a device label derived from the OS host name (trimmed), instead of the literal `default`. When the host name is empty or unavailable, it SHALL fall back to `default`; computing the label SHALL NOT fail the push. The push-resolution order is unchanged: explicit `--backup-id` verbatim; non-empty `--name` creates a backup with that name; no-id/no-name with existing backups appends to the first backup.

#### Scenario: Create on an empty account uses the device label
- **WHEN** `backup push` is invoked with neither `--backup-id` nor `--name`, the account has no backups, and the host name is `HUGO-DESKTOP`
- **THEN** a new backup labeled `HUGO-DESKTOP` is created, not `default`

#### Scenario: Host name unavailable falls back to default
- **WHEN** the same create path runs but the host name is empty or errors
- **THEN** the new backup is labeled `default` and the push still succeeds

#### Scenario: Explicit name is still honored verbatim
- **WHEN** `backup push --name <label>` is invoked
- **THEN** the new backup is labeled `<label>` (the device label is not substituted)

### Requirement: Rename support is advertised as a capability

The capabilities envelope SHALL advertise rename support so a client can gate its rename affordance against engine support. Rename reuses existing flags (`--backup-id`, `--name`), so it SHALL be advertised as an explicit feature flag rather than relied upon to be probed via the flag list.

#### Scenario: Capabilities advertises rename
- **WHEN** a client invokes `capabilities --json`
- **THEN** `features.hostedBackup.rename` is `true`
