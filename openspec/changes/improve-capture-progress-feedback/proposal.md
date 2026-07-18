## Why

Capture can spend most of a 20-second-plus run inside package inventory and settings collection without emitting meaningful progress. It also currently emits the non-contract item status `captured`, repeats settings collection after the bundle has already collected the same data, and advertises Store inclusion while its real enumerator explicitly excludes the `msstore` source.

## What Changes

- Add a schema-v1 `progress` event with `phase: "capture"` and an ordered stage key so consumers can show real engine progress without inventing percentages.
- Emit applicable `inventory`, `settings`, and `packaging` stages at truthful work boundaries across capture backends.
- Correct detected package item events to the existing contract form `status: "present"`, `reason: "detected"`.
- Return a bundle collection report and derive capture-envelope config metadata from that same collection pass, removing the duplicate settings scan without weakening metadata.
- **BREAKING**: Make both the WinGet community repository and Microsoft Store source part of default capture behavior in GUI and direct CLI use; add `--exclude-store-apps` as the explicit opt-out and retain `--include-store-apps` as a deprecated compatibility no-op.
- Preserve `msstore` source identity in captured profiles and route detection, installation, verification, and best-effort uninstall through the same source.
- Carry source-aware package coordinates through planning and provisioning-generation history so mixed-source detection and later rollback cannot lose source identity.
- Continue capture with a machine-readable warning when `msstore` is unavailable but another selected package source succeeds.
- Keep runtime filtering unchanged.

## Capabilities

### New Capabilities

- `capture-progress-streaming`: Defines additive capture progress events, stage semantics, ordering, and forward compatibility.
- `msstore-package-lifecycle`: Defines default Store enumeration, source preservation, restoration routing, opt-out behavior, and partial-source warnings.

### Modified Capabilities

- `capture-config-metadata`: Requires envelope config-module metadata to be derived from the same collection pass used to create the bundle.
- `provisioning-generation`: Adds source-aware installed/removed package history while retaining legacy ref-only arrays.

## Impact

- `go-engine/internal/events` gains the additive progress event and emitter support.
- Windows and non-Windows capture paths emit applicable stages and contract-valid detected-item statuses.
- Windows capture, manifest handling, and Winget lifecycle operations gain source-aware `msstore` behavior.
- Planner batching and provisioning generations gain additive source-aware package coordinates while retaining legacy ref-only fields for compatibility.
- `go-engine/internal/bundle` exposes a result-bearing API while retaining compatibility for existing callers.
- `go-engine/internal/commands/capture*.go` consumes the bundle report rather than collecting settings twice.
- `docs/contracts/event-contract.md`, tests, and the matching GUI change are updated together.
- No event schema version bump; the manifest source field is additive, while default capture contents intentionally expand.
