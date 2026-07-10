## Why

`apply` today is all-or-nothing over a manifest's app list: the only existing filter (`--restore-filter`) scopes config restore, not installs. With starter packs and shared setups becoming a first-class entry point, a recipient routinely wants *most* of a pack, not all of it ("this dev rig, but not Docker"). The current answer — hand-edit the JSONC — is fine for authors and hostile to recipients, and the GUI cannot offer a per-app picker without a contract-backed engine surface.

## What Changes

- **New `apply --only <id[,id...]>` flag** — limits the run to manifest apps whose manifest `id` is in the comma-separated list. Filtering happens at the manifest level before planning, so every downstream stage (plan, drivers, config-module expansion, restore scoping, verify, events, summary counts) behaves as if the manifest contained only the selected apps.
- **Validation before execution** — ids that match no manifest app fail the run with a validation error listing the unknown ids (typo protection); a selection that yields zero apps is likewise rejected. No partial execution on invalid input.
- **Guarded composition** — `--only` combined with `--prune` is rejected: prune converges to the exact manifest set, and pruning against a deliberate subset would classify every unselected app as drift. `--only` with `--dry-run` works and is the GUI preview path.
- **Capability advertisement** — `--only` appears in `commands.apply.flags` (the documented probe for flag support).

## Capabilities

### New Capabilities
- `apply-app-subset`: subset selection for apply — flag semantics, manifest-level filtering, validation, prune guard, and the capability advert.

### Modified Capabilities
<!-- None: planning, drivers, restore, and verify are reused unchanged over the filtered manifest. -->

## Impact

- `go-engine/internal/commands/apply.go` — filter step + validation.
- `go-engine/cmd/endstate/main.go` — flag parsing/help (protected area: this change is the explicit instruction).
- `go-engine/internal/commands/capabilities.go` — `--only` in `commands.apply.flags`.
- `docs/contracts/cli-json-contract.md` — apply synopsis + flag documentation.
- **Consumer (separate `endstate-gui` change):** per-app checkboxes in the setup-flow preview (the save flow's capture-curation checkbox pattern already exists), passing `--only` on apply.
- Backward-compatible: flag is optional; omitted behavior unchanged.
