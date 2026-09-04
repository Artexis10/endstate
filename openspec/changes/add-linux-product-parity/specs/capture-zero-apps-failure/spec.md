## MODIFIED Requirements

### Requirement: Zero Apps Captured Is a Failure
The capture command SHALL treat a result with zero captured applications and zero captured configuration sets as an error condition, not a success. A capture with zero resolved package apps MAY succeed when it contains at least one valid selected settings capture, because package normalization and settings portability are independent. The engine SHALL retry only sources whose adapter contract defines a transient-empty retry; it SHALL NOT rerun every successful discovery source blindly.

#### Scenario: Capture with no useful state fails
- **WHEN** `capture` completes discovery and no selected application or configuration set can be captured
- **THEN** the command SHALL exit with a non-zero exit code
- **AND** the error SHALL indicate that no useful portable state was found

#### Scenario: Settings-only capture succeeds
- **WHEN** no package inventory item can be resolved but at least one selected trusted config module captures a valid settings payload
- **THEN** the command SHALL produce a valid settings-only artifact
- **AND** the empty apps list SHALL NOT by itself fail the command

#### Scenario: Transient-empty source retries according to its adapter contract
- **WHEN** a source that defines transient-empty retry initially returns zero inventory
- **THEN** the engine SHALL retry that source according to its bounded adapter policy before deciding the source result
- **AND** the retry count and outcome SHALL be reported in source diagnostics

#### Scenario: Capture with at least one app succeeds normally
- **WHEN** `capture` resolves and selects at least one application
- **THEN** the command SHALL proceed with the capture workflow
- **AND** no global retry-on-zero behavior SHALL rerun unrelated successful sources

### Requirement: JSON Envelope Reflects Zero-App Failure
When capture fails because it found neither applications nor configuration sets, the JSON envelope SHALL clearly communicate the zero-useful-state reason and discovery counts. When a settings-only capture succeeds, the envelope SHALL report success with zero apps and non-zero configuration counts.

#### Scenario: JSON output on zero-useful-state capture
- **WHEN** `capture --json` finds no selected application or configuration after bounded source retries
- **THEN** the JSON envelope SHALL indicate failure
- **AND** it SHALL include a stable reason plus discovered/resolved/unresolved/ignored/settings counts

#### Scenario: JSON output on settings-only capture
- **WHEN** `capture --json` creates a valid settings-only artifact
- **THEN** the JSON envelope SHALL indicate success
- **AND** it SHALL report zero included apps and at least one included configuration set
