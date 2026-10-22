## ADDED Requirements

### Requirement: Consumers May Fold Resolutions That Need No Decision
As the one exception to the verbatim-rendering rule of "Restore Envelope Includes Config Generation Resolutions", a consumer MAY fold config resolutions that need no decision into one summary line that the consumer writes. The line SHALL state only counts computed from the folded rows and what their `resolution` values establish.

A row SHALL qualify only when all of these hold:
- its `status`, when present, is not `failed`, `rolled_back` or `rollback_failed`;
- its `reason` is absent or one of `target_not_detected`, `already_up_to_date`, `restore_filtered` and `restore_not_enabled`;
- its `resolution` is `legacy_unverified`, or `unknown` with one of those reasons.

A `direct` row that meets the first two conditions MAY show as its engine `label` alone. The consumer SHALL show every other row on its own with the engine-authored `label`, `message` and `remediation`. A `config-resolution` stream event has no `status`, so a failure reason on the event SHALL keep the row on its own. In the preview and the results, the consumer SHALL keep each folded row's `label`, `message` and `remediation` available verbatim through progressive disclosure. A live progress list during a run MAY show the summary line alone, because the results that follow carry that copy.

#### Scenario: Legacy and absent-target rows fold into one line
- **WHEN** a restore result contains `legacy_unverified` rows with status `skipped` and reason `restore_not_enabled`, and an `unknown` row with reason `target_not_detected`
- **THEN** a consumer may show one summary line for those rows
- **AND** the engine label, message and remediation of each row remain available verbatim behind disclosure in the results

#### Scenario: A compatible row may show its label alone
- **WHEN** a `direct` row has status `planned` and no reason
- **THEN** the consumer may show only its engine label

#### Scenario: A compatible row with a failure reason is shown in full
- **WHEN** a `direct` row has reason `app_running`
- **THEN** the consumer shows that row on its own with the engine label, message and remediation

#### Scenario: A row that needs a decision is never folded
- **WHEN** a restore result contains an `unknown` row with reason `ambiguous_target_instance`, or an `incompatible` or `migrate` row
- **THEN** the consumer shows that row on its own with the engine label, message and remediation

#### Scenario: A failed row is never folded
- **WHEN** a `legacy_unverified` row has status `failed`, `rolled_back` or `rollback_failed`
- **THEN** the consumer shows that row on its own

#### Scenario: A failure reason on a stream event is never folded
- **WHEN** a `config-resolution` event has resolution `legacy_unverified` and reason `target_collision`, or resolution `direct` and reason `backup_failed`
- **THEN** the consumer shows that event on its own with the engine message

#### Scenario: An unlisted reason is shown on its own
- **WHEN** an `unknown` row has a reason that this requirement does not list, or no reason
- **THEN** the consumer shows that row on its own
