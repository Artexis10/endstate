# Proposal: capture-pin

## Why

The version-pinning machinery is shipped end-to-end but dormant on its input side: the manifest schema carries a per-app `version`, the winget driver installs exact versions (`winget install --version`), and `apply --repin` converges drift — yet no capture-produced manifest ever populates the field that drives all of it. Capture even parses installed versions today (the `winget list` pass that builds display names) and throws them away. A captured manifest therefore reproduces "latest-of-everything", not the machine that was captured — the gap the 2026-07 market brief names as blocking the self-rebuild wedge.

## What Changes

- **New opt-in `capture --pin` flag** — the winget capture path writes each app's installed version (from the `winget list` snapshot capture already takes) into the emitted manifest's per-app `version` field. Best-effort: an app whose version the snapshot does not expose gets no `version` field, and capture never fails over a missing version. Without `--pin`, output is byte-identical to today.
- **`capture --update` stops dropping declared versions** — the merge that rebuilds existing manifest entries currently discards `version` and `driver`; it now preserves both (the realizer capture path already does). Under `--update --pin`, an installed app's pin is refreshed to the installed version; an app absent from the version snapshot keeps its existing pin — a pin is never blanked by absence of evidence.
- **Capability advertisement** — `--pin` appears in `commands.capture.flags`.
- **Realizer scope** — Nix/brew capture already records versions unconditionally (per `nix-package-version-capture`); `--pin` is accepted there as a no-op.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `windows-version-capture-pinning`: two ADDED requirements — `capture --pin` records installed versions into the emitted manifest; `capture --update` preserves declared versions through the merge.

## Impact

- `go-engine/internal/commands/capture.go` — snapshot seam returns the full installed-apps snapshot (one `winget list` pass yields both the display-name map and a version map); `Pin` flag; version emission in the conversion loop, the `--update` merge, and the sanitize path.
- `go-engine/cmd/endstate/main.go` — `--pin` parsing/help (protected area: this change is the explicit instruction).
- `go-engine/internal/commands/capabilities.go` — `--pin` in `commands.capture.flags`.
- `docs/contracts/cli-json-contract.md` — capture flag list + version-capture note (additive, no schema bump).
- Not affected: `capture-artifact-contract` (its "version" language is the manifest schema version), events, envelope shape, bundles, module matching, realizer capture code.
- Backward-compatible: flag is optional; omitted behavior unchanged except that `--update` no longer destroys existing `version`/`driver` fields (a silent-deletion bug under the non-destructive-defaults invariant).
