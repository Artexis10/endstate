## ADDED Requirements

### Requirement: Each restore item status has one meaning

The engine SHALL keep the restore item statuses `restored`, `skipped_up_to_date`, `skipped_missing_source`, and `failed`, and SHALL give each one meaning:

- `restored`: the action changed the target. In a dry run, the action would change the target and nothing was changed.
- `skipped_up_to_date`: the target already has the desired state, so nothing needed to change. An optional `delete-glob` action whose target directory is absent reports this status, because there is nothing to delete.
- `skipped_missing_source`: an optional input is not included in the saved setup.
- `failed`: the action could not complete. A required input that the saved setup does not include fails with the plain message "The saved setup does not include this required file."; the item's `source` field carries the path.

#### Scenario: An absent optional cleanup directory is up to date

- **WHEN** an optional `delete-glob` restore action targets a directory that does not exist
- **THEN** its restore item reports `skipped_up_to_date`

#### Scenario: An optional file absent from the saved setup is skipped

- **WHEN** an optional copy restore action names a source that the saved setup does not include
- **THEN** its restore item reports `skipped_missing_source`

#### Scenario: A required file absent from the saved setup fails

- **WHEN** a required copy restore action names a source that the saved setup does not include
- **THEN** its restore item reports `failed` with `error` "The saved setup does not include this required file."
- **AND** its `source` field carries the missing path
