## Why

A restore of a large saved setup returns many config resolutions that need nothing from the person: settings saved before the engine checked versions, and settings whose app is absent or already up to date. The GUI showed each group as its own block with the same "Compatibility unknown" heading, which read as duplicated noise and hid the rows that need a decision.

The GUI contract said the GUI never maps resolutions to its own copy. A summary line for these rows needs a contract that allows it and says which rows qualify.

## What Changes

- A consumer may fold resolutions that need no decision into one summary line that it writes and computes from the rows.
- A row qualifies only when its status is not `failed`, `rolled_back` or `rollback_failed`, and its resolution is `legacy_unverified`, or `unknown` with the reason `target_not_detected`, `already_up_to_date`, `restore_filtered` or `restore_not_enabled`.
- Every other row is shown on its own with the engine's label, message and remediation. An `unknown` reason that is new or absent is shown on its own.
- Each folded row's engine label, message and remediation stay available verbatim behind progressive disclosure.
- The engine output does not change.

## Capabilities

### Modified Capabilities

- `apply-restore-envelope`: adds the rule for which resolutions a consumer may fold.

## Impact

- `docs/contracts/gui-integration-contract.md`, "Locked Default Labels".
- The GUI implements the rule in its config generation presentation (endstate-gui).
- An engine change that adds an `unknown` reason needs no GUI change: the GUI shows the new reason on its own until the contract adds it to this list.
