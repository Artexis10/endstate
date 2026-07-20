## ADDED Requirements

### Requirement: GUI Consumes Only Contract-Defined Envelope Fields

The GUI SHALL read only envelope fields that `docs/contracts/cli-json-contract.md` defines for the command it invoked. It SHALL NOT read fields defined for a different command, and SHALL NOT treat a contract-defined field as deprecated in favor of a field the contract does not define for that command.

This closes the drift that let the GUI read `counts` (a `capture` field) and `items` (a `generations` field) from the apply envelope while marking apply's real `summary` field legacy — leaving final-state reconciliation permanently inert because the field it reconciled against was always absent.

#### Scenario: GUI reads apply results from contract-defined fields

- **WHEN** the GUI processes an apply envelope
- **THEN** it SHALL read app results from `actions[]` and aggregates from `summary`
- **AND** it SHALL NOT read `items` or `counts` from that envelope

#### Scenario: Cross-command field borrowing is rejected

- **WHEN** a field is defined by the contract for one command only
- **THEN** the GUI SHALL NOT read that field from a different command's envelope

#### Scenario: Absent optional field does not silently disable behavior

- **WHEN** the GUI depends on an envelope field to perform reconciliation or display
- **AND** that field is absent from the envelope
- **THEN** the GUI SHALL NOT silently skip the behavior
- **AND** the condition SHALL be surfaced as a diagnosable state rather than rendering unreconciled data as final

### Requirement: GUI Preserves Engine-Supplied Result Semantics

When the GUI derives display state from engine results, it SHALL preserve the engine-supplied identity and status of each item. It SHALL NOT discard engine-supplied fields during internal transformation, and SHALL NOT present a non-terminal status as a final result.

#### Scenario: Engine-supplied display name survives transformation

- **WHEN** the GUI reconciles a streamed event against an envelope result
- **THEN** the engine-supplied display name SHALL be preserved in the reconciled record
- **AND** the GUI SHALL NOT fall back to the raw package ref for an item the engine named

#### Scenario: Non-terminal status is never rendered as a final result

- **WHEN** a run has completed and an item's last known status is non-terminal (for example `to_install` or `installing`)
- **THEN** the GUI SHALL reconcile that item against the envelope's authoritative result before rendering
- **AND** SHALL NOT present the non-terminal status as the item's outcome
