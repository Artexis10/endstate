## Why

A restore of a large saved setup returns many config resolutions that need nothing from the person: settings saved before the engine checked versions, and settings whose app is absent or already up to date. The GUI showed each group as its own block with the same "Compatibility unknown" heading, which read as duplicated noise and hid the rows that need a decision.

The GUI contract said the GUI never maps resolutions to its own copy. A summary line for these rows needs a contract that allows it and says which rows qualify.

## What Changes

- A consumer may fold resolutions that need no decision into one summary line that it writes and computes from the rows.
- A row qualifies only when its status is not `failed`, `rolled_back` or `rollback_failed`, its reason is absent or one of `target_not_detected`, `already_up_to_date`, `restore_filtered` and `restore_not_enabled`, and its resolution is `legacy_unverified`, or `unknown` with one of those reasons.
- Every other row is shown on its own with the engine's label, message and remediation. A failure reason on a stream event, which has no status, keeps the row on its own.
- The preview and the results keep each folded row's engine copy verbatim behind progressive disclosure. A live progress list may show the summary line alone.
- The contract paragraph and the two places that forbade GUI copy or GUI logic on details now name this exception.
- The engine output does not change.

## Capabilities

### Modified Capabilities

- `apply-restore-envelope`: adds the rule for which resolutions a consumer may fold.

## Impact

- `docs/contracts/gui-integration-contract.md`, "Locked Default Labels" and the advanced-details paragraph.
- `docs/contracts/restore-safety-contract.md`, the generation-aware GUI rendering paragraph.
- The GUI implements the rule in its config generation presentation (endstate-gui).
- An engine change that adds an `unknown` reason needs no GUI change: the GUI shows the new reason on its own until the contract adds it to this list.
