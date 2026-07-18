## ADDED Requirements

### Requirement: Capture emits additive progress events

The engine SHALL emit schema-v1 progress events with `event: "progress"`, `phase: "capture"`, and a supported stage value, without user-facing message text or completion percentages.

#### Scenario: Capture inventory starts

- **WHEN** capture is about to enumerate installed packages
- **THEN** it emits a progress event with stage `inventory`
- **AND** the opening capture phase event precedes that progress event

#### Scenario: Capture collects settings

- **WHEN** capture is about to run a matched settings-module collection pass
- **THEN** it emits a progress event with stage `settings`

#### Scenario: Capture writes its artifact

- **WHEN** capture is about to create or atomically publish its output artifact
- **THEN** it emits a progress event with stage `packaging`
- **AND** the event precedes the artifact write work it describes

#### Scenario: No settings work applies

- **WHEN** a capture path has no matched settings modules or is sanitized
- **THEN** it omits the `settings` stage
- **AND** any emitted stages retain the order `inventory`, `settings`, `packaging`

### Requirement: Capture progress preserves event-stream compatibility

Capture progress events SHALL be additive within event schema v1 and SHALL preserve existing stream ordering guarantees.

#### Scenario: Full capture stream completes

- **WHEN** capture emits progress and item events successfully
- **THEN** the first event remains the capture phase event
- **AND** applicable progress stages are monotonic
- **AND** the final event remains the capture summary event

#### Scenario: Older consumer reads the stream

- **WHEN** a schema-v1 consumer does not recognize the progress event type
- **THEN** it can ignore that event without losing item, artifact, summary, or envelope truth

### Requirement: Capture item statuses remain canonical

The engine SHALL emit detected package items with status `present` and reason `detected` across supported capture backends.

#### Scenario: Installed package is captured

- **WHEN** capture includes a package discovered by a supported backend
- **THEN** its item event has status `present`
- **AND** its reason is `detected`
- **AND** the item event status is not `captured`
