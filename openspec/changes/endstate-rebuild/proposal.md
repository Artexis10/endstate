## Why

Rebuilding a machine from an Endstate profile is the product's headline promise, yet today it is a multi-step chore: the operator must locate the manifest inside a capture bundle, run `apply`, remember `--enable-restore`, then run `verify` — and the capture zip the engine happily *produces* is not directly consumable by the Go engine at all. The "apply from zip" behavior specified in `openspec/specs/capture-bundle-zip.md` ("Apply from zip extracts to temp directory and cleans up") was only ever implemented in the retired PowerShell engine; `bundle.ExtractBundle`/`bundle.IsBundle` exist in the Go engine but have no production caller. A recipient handed a `MyProfile.zip` has no single command that turns it into a provisioned machine.

`endstate rebuild --from <bundle.zip|manifest.jsonc>` collapses the fresh-machine flow into one command that composes the existing plan → install → restore → verify pipeline, and wires the dead bundle-extraction code into production so a capture zip is finally a first-class input.

## What Changes

- **New `rebuild` command** — `endstate rebuild --from <path>` runs the full fresh-machine pipeline: (optionally) extract a bundle, install apps, restore configuration, then verify. Restore is ON by default; a live (non-`--dry-run`, non-`--no-restore`) run requires `--confirm`. Local file input only; URL input is rejected.
- **Bundle extraction wired into production** — `rebuild` is the first production caller of `bundle.IsBundle`/`bundle.ExtractBundle`. A `.zip` input is extracted to a temp directory whose lifetime spans the entire install+restore+verify pipeline and is cleaned up afterward; a bare `.jsonc` input is used directly with no extraction.
- **New `CONFIRMATION_REQUIRED` error code** — returned before any mutation when a live run is requested without `--confirm`. Remediation directs the operator to `--confirm` or `--dry-run`.
- **Capability advertisement** — `rebuild` appears in `commands.rebuild` with its flag set so clients can gate a one-click rebuild affordance on engine support.
- **Verify failures are data** — a rebuild whose post-install verification reports drift still returns a success envelope and exit 0 (precedent: `schedule run`). Only infrastructure/input errors flip `success` to false.

## Capabilities

### New Capabilities
- `endstate-rebuild`: the rebuild command — input resolution and validation, the confirmation gate, the extract → apply → verify composition, temp-dir lifetime, the `--no-restore`/`--dry-run` lanes, verify-as-data semantics, and the capability advert.

### Modified Capabilities
- `capture-bundle-zip`: zip consumption in the Go engine now happens via `rebuild`. The previously-PowerShell-only "apply from zip extracts to temp directory and cleans up" behavior is realized by the Go engine's `rebuild` command (extract → apply → verify → cleanup).

## Impact

- `go-engine/internal/commands/rebuild.go` — new orchestrator (`RunRebuild`, `RebuildFlags`, `RebuildResult`).
- `go-engine/internal/envelope/errors.go` — new `CONFIRMATION_REQUIRED` error code.
- `go-engine/cmd/endstate/main.go` — `--from`/`--no-restore` parsing, usage/help, dispatch (protected area — this change is the explicit instruction).
- `go-engine/internal/commands/capabilities.go` — `rebuild` entry in the commands map.
- `docs/contracts/cli-json-contract.md` — new `rebuild` command section + `CONFIRMATION_REQUIRED` in the error-code table (additive; no schema bump).
- `docs/contracts/event-contract.md` — note that `rebuild` composes the apply and verify event streams (no new event types; schema stays v1).
- `readme.md` — fresh-machine quickstart block + a `rebuild` row in the CLI commands table.
- `go-engine/internal/commands/rebuild_test.go` — hermetic round-trip and lane coverage.
- Backward-compatible: a new command; no existing behavior changes.
