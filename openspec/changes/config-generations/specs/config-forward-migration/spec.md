## ADDED Requirements

### Requirement: Migration Graph Is Explicit, Forward-Only, and Unambiguous
Each migration edge SHALL connect two generations in the same config set and SHALL target a strictly higher generation order. Catalog validation SHALL reject duplicate edges, unknown generations, same/backward edges, cycles, and more than one route between a reachable source/target pair.

#### Scenario: Multi-step forward path is valid
- **WHEN** a config set declares the unique path `g1 -> g2 -> g3`
- **THEN** the engine may plan that ordered migration chain from `g1` to `g3`

#### Scenario: Backward edge is rejected
- **WHEN** a migration edge targets a lower generation order
- **THEN** catalog validation rejects the module

#### Scenario: Ambiguous routes are rejected
- **WHEN** two distinct migration paths can reach the same target from one source
- **THEN** catalog validation rejects the module rather than selecting a path by declaration order

### Requirement: Migration Operations Use an Engine-Owned Allowlist
Migration definitions SHALL contain only engine-defined declarative operations. The first release SHALL support staging-relative `file-copy`, `file-move`, `file-delete`, `json-set`, `json-delete`, `json-move`, `ini-set`, `ini-delete`, and `ini-move`. Shells, commands, executable paths, dynamic plugins, generic regex replacement, and host-absolute writes SHALL be rejected.

#### Scenario: Allowed JSON migration loads
- **WHEN** a forward edge uses only documented JSON and file operations with safe relative paths
- **THEN** catalog validation accepts the operation definitions

#### Scenario: Shell command is rejected
- **WHEN** a module or bundle declares a shell, PowerShell, batch, or executable migration step
- **THEN** catalog validation rejects it
- **AND** the engine never starts that process

#### Scenario: Operation cannot escape staging
- **WHEN** an operation uses an absolute path or traversal outside the config-set staging root
- **THEN** validation rejects the migration before execution

### Requirement: Every Migration Runs on a Copy and Is Validated
The engine SHALL verify payload integrity, copy the config set to a fresh staging root, apply migration edges only within staging, validate every edge output, and validate the final target generation before resolving host writes. Supported validation primitives SHALL include `file-exists`, `json-parse`, `json-path-exists`, `ini-parse`, and `ini-key-exists`.

#### Scenario: Original captured payload remains unchanged
- **WHEN** a migration succeeds or fails
- **THEN** the bytes under the bundle payload root remain unchanged

#### Scenario: Intermediate edge validation fails
- **WHEN** validation after `g1 -> g2` fails in a planned `g1 -> g2 -> g3` chain
- **THEN** the engine stops the chain
- **AND** discards staging
- **AND** reports reason `staging_validation_failed`
- **AND** performs no host target mutation for that config set

### Requirement: Config-Set Commit Is Journaled and Transactional
After staging succeeds, the engine SHALL create all required backups, atomically persist a `pending` journal intent, commit the config set, verify the target generation, and atomically mark the intent `committed`. A journal-intent write failure SHALL abort before the first host target mutation. Failure to record completion SHALL trigger immediate rollback.

#### Scenario: Journal intent precedes mutation
- **WHEN** a migrated config set is ready to commit
- **THEN** backup paths and planned target actions are durably journaled before the first target write

#### Scenario: Journal write fails
- **WHEN** the engine cannot durably write journal intent
- **THEN** the config-set commit fails
- **AND** reports reason `journal_intent_failed`
- **AND** no target file or registry value is changed

#### Scenario: Completion record fails
- **WHEN** target commit and validation succeed but the engine cannot mark journal intent committed
- **THEN** the engine immediately rolls the config set back
- **AND** retains reason `journal_completion_failed`
- **AND** does not report the set as successfully committed

### Requirement: Failed Commit Rolls Back Its Config Set
If a config-set commit or final validation fails after mutation begins, the engine SHALL immediately restore that set's pre-run state from its journal. A successful rollback SHALL permit safe independent sets to continue. An incomplete rollback SHALL stop all later config-set mutation in that run. The result SHALL retain the original failure as `reason` and SHALL report rollback outcome as terminal status `rolled_back` or `rollback_failed`.

#### Scenario: Second target write fails
- **WHEN** a config-set transaction commits its first target and fails on its second
- **THEN** the engine restores the first target to its pre-run state
- **AND** reports terminal status `rolled_back`
- **AND** retains the second-write failure as the primary reason

#### Scenario: Independent set continues
- **WHEN** one config-set transaction fails and rolls back successfully
- **THEN** another non-overlapping config set with a valid plan may continue

#### Scenario: Incomplete rollback blocks later sets
- **WHEN** a config-set transaction fails after mutation begins
- **AND** rollback cannot prove complete restoration
- **THEN** the config set reports terminal status `rollback_failed`
- **AND** the engine starts no later config-set mutation in that run

### Requirement: Pending Journal Intents Are Recovered Before New Mutation
Before any restore-capable command performs new config mutation, the engine SHALL scan for pending migration intents and attempt idempotent rollback using their recorded backups and target actions. If recovery cannot complete, the command SHALL fail with `recovery_required` before any new config mutation.

#### Scenario: Process died during commit
- **WHEN** a later restore-capable run finds a pending intent left after process death
- **THEN** the engine attempts rollback before planning any new target mutation
- **AND** records the intent `rolled_back` when recovery succeeds

#### Scenario: Pending recovery fails
- **WHEN** a pending intent cannot be rolled back completely
- **THEN** the new command fails with reason `recovery_required`
- **AND** performs no new config mutation

### Requirement: Restore Journal Records Generation Lineage
Migration journal entries SHALL record the capture ID, target instance ID, source generation, target generation, ordered migration path, capture-time module revision, restore-time module revision, concrete target actions, validation outcome, and rollback outcome.

#### Scenario: Revert uses concrete actions
- **WHEN** a completed migrated restore is reverted
- **THEN** revert restores the concrete pre-run target state from the journal
- **AND** does not attempt to reverse the forward migration graph

### Requirement: Running Applications Are Never Killed Automatically
If a selected generation declares that its application must be closed and the engine detects it running, migration/restore SHALL report reason `app_running` and SHALL NOT kill the process or mutate that config set.

#### Scenario: Required app is running
- **WHEN** a config set requires application closure and the process is active
- **THEN** the set is skipped with reason `app_running`
- **AND** the engine leaves the process and target configuration unchanged

### Requirement: Unsupported Formats Are Reported, Not Guessed
If reaching a target generation requires a transformation outside the engine operation allowlist, the module SHALL have no executable migration path and the engine SHALL report the set as incompatible or unknown.

#### Scenario: Opaque binary format has no primitive
- **WHEN** a proprietary binary config change cannot be expressed by supported operations
- **THEN** the engine does not attempt byte-level guessing
- **AND** reports that no supported migration path exists
