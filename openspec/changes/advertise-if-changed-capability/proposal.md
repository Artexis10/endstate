## Why

The GUI needs a contract-backed capability gate to decide whether the hosted-backup auto-backup flow can be triggered with `--if-changed` (skip upload when the manifest hash is unchanged). Currently `--if-changed` is listed as a flag in `commands.backup.flags`, but the GUI cannot safely distinguish "flag exists" from "flag is semantically implemented and safe to rely on". A first-class `features.hostedBackup.ifChanged` boolean lets the GUI gate the auto-backup affordance without parsing the flag list.

## What Changes

- Add `ifChanged` (boolean) to `features.hostedBackup` in the capabilities envelope — set unconditionally `true` because `--if-changed` is already implemented in the engine.
- Update `docs/contracts/gui-integration-contract.md` capabilities handshake example to reflect the current `hostedBackup` features shape including `ifChanged`.
- No schema version bump (additive field, backward-compatible per contract §3).

## Capabilities

### New Capabilities

- `advertise-if-changed-capability`: `features.hostedBackup.ifChanged` is the canonical gate for the GUI's conditional auto-backup flow.

### Modified Capabilities

(none — additive field only, no existing spec behavior changes)

## Impact

- `go-engine/internal/commands/capabilities.go` — `HostedBackupFeature.IfChanged bool` field, set `true`
- `docs/contracts/gui-integration-contract.md` — additive example update
- No breaking changes for existing GUI consumers (unknown fields are ignored per contract §3)
