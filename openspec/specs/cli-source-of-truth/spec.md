# cli-source-of-truth Specification

## Purpose
Establishes that the CLI engine is the authoritative source of all provisioning logic and state, and that the GUI is a thin presentation layer with no independent business logic.

## Requirements
### Requirement: CLI Owns All Business Logic

All provisioning, capture, restore, and verification logic SHALL reside in the CLI engine. The GUI SHALL NOT implement or duplicate this logic.

#### Scenario: GUI delegates apply to CLI
- **WHEN** the user triggers an apply operation from the GUI
- **THEN** the GUI invokes the CLI `apply` command
- **AND** the GUI does not perform any install, restore, or verify operations itself

#### Scenario: GUI delegates capture to CLI
- **WHEN** the user triggers a capture operation from the GUI
- **THEN** the GUI invokes the CLI `capture` command
- **AND** the GUI does not read or bundle config files itself

### Requirement: GUI Renders CLI Output

The GUI SHALL present data produced by the CLI without transforming its semantics.

#### Scenario: GUI displays CLI JSON envelope
- **WHEN** the CLI produces a JSON envelope result
- **THEN** the GUI renders the envelope data to the user
- **AND** the GUI does not alter success/failure semantics from the envelope

#### Scenario: GUI does not cache stale state
- **WHEN** the CLI reports updated state via streaming or JSON output
- **THEN** the GUI reflects the latest CLI output
- **AND** the GUI does not display locally cached results that contradict CLI output
