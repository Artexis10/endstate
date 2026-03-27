# config-module-synthesis Specification

## Purpose
Ensures that synthesized config module entries are transparent to the user, clearly distinguishable from explicitly authored entries.

## Requirements
### Requirement: Synthesized Entries Are Marked

Any config module entry that is synthesized (auto-generated rather than explicitly authored) SHALL carry a clear marker indicating its origin.

#### Scenario: Synthesized entry includes origin marker
- **WHEN** the engine synthesizes a config module entry during apply or capture
- **THEN** the entry includes a `_synthesized` or equivalent marker field
- **AND** the marker indicates the synthesis source (e.g., which process generated it)

#### Scenario: Authored entries do not carry synthesis marker
- **WHEN** a config module entry is explicitly authored by the user
- **THEN** the entry does not contain a synthesis marker
- **AND** it is distinguishable from synthesized entries

### Requirement: Synthesis Is Deterministic

Given the same inputs, config module synthesis SHALL produce identical output.

#### Scenario: Repeated synthesis produces identical entries
- **WHEN** synthesis runs twice with the same source modules and manifest
- **THEN** the resulting entries are identical in content and order
- **AND** no non-deterministic fields (e.g., random IDs, unstable timestamps) differ between runs
