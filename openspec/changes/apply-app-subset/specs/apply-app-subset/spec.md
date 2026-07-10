## ADDED Requirements

### Requirement: apply can be limited to a subset of manifest apps

`apply --only <id[,id...]>` SHALL limit the run to manifest apps whose `id` is in the list. The filter SHALL be applied to the manifest's app set before planning, so plan generation, driver execution, config-module expansion, restore scoping, verification, event emission, and summary counts all operate on the filtered set exactly as if the manifest contained only those apps. Omitting `--only` SHALL leave behavior unchanged.

#### Scenario: Subset installs only the selected apps
- **WHEN** `apply --only git-git,7zip-7zip` runs against a manifest with five apps
- **THEN** only `git-git` and `7zip-7zip` are planned and executed
- **AND** the summary counts reflect two apps, not five

#### Scenario: Subset preview via dry-run
- **WHEN** `apply --only git-git --dry-run` is invoked
- **THEN** the preview reflects only the selected app and nothing is executed

#### Scenario: Restore scope follows the subset
- **WHEN** `apply --only git-git --enable-restore` runs against a manifest where both `git-git` and another app have matching config modules
- **THEN** only config modules matched to `git-git` are considered for restore

### Requirement: Subset selection is validated before execution

Ids in `--only` that match no manifest app SHALL fail the run with a validation error that names the unknown ids, before any planning or execution. A `--only` value that selects zero apps SHALL likewise be rejected.

#### Scenario: Unknown id is a pre-execution error
- **WHEN** `apply --only git-git,not-a-real-id` is invoked
- **THEN** the run fails with a validation error naming `not-a-real-id`
- **AND** nothing is installed or modified

#### Scenario: Empty selection is rejected
- **WHEN** `apply --only ""` is invoked
- **THEN** the run fails with a validation error

### Requirement: Subset selection composes safely with other flags

`apply --only` combined with `--prune` SHALL be rejected with a validation error: prune converges the machine to the exact manifest set, and pruning against a deliberate subset would classify every unselected app as removable drift.

#### Scenario: only + prune is rejected
- **WHEN** `apply --only git-git --prune --confirm` is invoked
- **THEN** the run fails with a validation error and no plan is executed

### Requirement: Subset support is advertised as a capability

The capabilities envelope SHALL list `--only` in `commands.apply.flags` so clients can gate a per-app selection UI on engine support.

#### Scenario: Capabilities advertises the flag
- **WHEN** a client invokes `capabilities --json`
- **THEN** `commands.apply.flags` includes `--only`
