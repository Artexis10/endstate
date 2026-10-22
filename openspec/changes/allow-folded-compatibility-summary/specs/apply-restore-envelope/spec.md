## ADDED Requirements

### Requirement: Consumers May Fold Resolutions That Need No Decision
A consumer MAY fold config resolutions that need no decision into one summary line. The consumer writes that line and SHALL compute every count in it from the folded rows. A row SHALL qualify only when its `status` is not `failed`, `rolled_back` or `rollback_failed`, and its `resolution` is `legacy_unverified`, or `unknown` with `reason` `target_not_detected`, `already_up_to_date`, `restore_filtered` or `restore_not_enabled`. The consumer SHALL show every other row on its own with the engine-authored `label`, `message` and `remediation`, including an `unknown` row whose reason is absent or not in this list. For each folded row, the consumer SHALL keep the engine-authored `label`, `message` and `remediation` available verbatim through progressive disclosure.

#### Scenario: Legacy and absent-target rows fold into one line
- **WHEN** a restore result contains `legacy_unverified` rows with status `skipped` and an `unknown` row with reason `target_not_detected`
- **THEN** a consumer may show one summary line for those rows
- **AND** the engine label, message and remediation of each row remain available verbatim behind disclosure

#### Scenario: A row that needs a decision is never folded
- **WHEN** a restore result contains an `unknown` row with reason `ambiguous_target_instance`, or an `incompatible` or `migrate` row
- **THEN** the consumer shows that row on its own with the engine label, message and remediation

#### Scenario: A failed row is never folded
- **WHEN** a `legacy_unverified` row has status `failed`, `rolled_back` or `rollback_failed`
- **THEN** the consumer shows that row on its own

#### Scenario: An unlisted reason is shown on its own
- **WHEN** an `unknown` row has a reason that this requirement does not list, or no reason
- **THEN** the consumer shows that row on its own
