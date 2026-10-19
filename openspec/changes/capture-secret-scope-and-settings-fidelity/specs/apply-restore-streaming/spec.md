## ADDED Requirements

### Requirement: Restore-item messages use plain language

The engine SHALL author each `restore-item` message in plain language from the item status and the run's dry-run flag. A dry run changes nothing, so its messages SHALL say what would happen and SHALL NOT claim that a restore happened. Reason codes such as `already_up_to_date`, `source_missing`, and `restore_failed` SHALL stay unchanged.

| Status | Live run message | Dry-run message |
|--------|------------------|-----------------|
| `restoring` | `Restoring settings` | `Checking settings` |
| `restored` | `Settings restored` | `Would restore settings` |
| `skipped_up_to_date` | `Already up to date` | `Already up to date` |
| `skipped_missing_source` | `Not included in the saved setup` | `Not included in the saved setup` |
| `failed` | the item's error text | the item's error text |

Each `restore-item` event SHALL carry an optional `label` that holds the restore entry's authored `reason` when the entry declares one, for every restorer type, in a live run and a dry run; the envelope's `restoreItems[]` entry SHALL carry the same `label`.

#### Scenario: A preview never claims a restore

- **WHEN** `apply --enable-restore --dry-run --events jsonl` previews a copy restore whose target would change
- **THEN** the `restoring` event message is `Checking settings`
- **AND** the terminal `restored` event message is `Would restore settings`

#### Scenario: A live run reports the restore

- **WHEN** `apply --enable-restore --events jsonl` restores a copy action
- **THEN** the terminal `restored` event message is `Settings restored`
