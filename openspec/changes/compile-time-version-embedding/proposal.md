## Why

Production binaries (bootstrap install, GUI sidecar) have no `VERSION` file nearby, so they fall back to `"0.0.0-dev"`. Dev builds silently use stale `VERSION` files if the local checkout is behind. Both cases produce misleading version output that undermines the version-envelope-injection contract.

## What Changes

- Compile-time `ldflags`-injected values become the **primary** source for `cliVersion` and `schemaVersion`
- `VERSION` and `SCHEMA_VERSION` file reads are retained as a **dev-mode fallback** (covers `go run` without ldflags)
- Resolution order: ldflags value > file read > fallback constant
- No change to the JSON envelope shape, schema version, or any external contract

## Capabilities

### New Capabilities

- `compile-time-version-embedding`: Engine binary embeds version at build time via `-ldflags -X`, eliminating runtime dependency on VERSION/SCHEMA_VERSION files for production builds

### Modified Capabilities

- `version-envelope-injection`: VERSION/SCHEMA_VERSION files are demoted from "sole source of truth" to "dev-mode fallback"; ldflags injection is the primary source

## Impact

- **File:** `go-engine/internal/config/version.go` -- add package-level vars, update `ReadVersion` / `ReadSchemaVersion` to check ldflags values first
- **File:** Build scripts (`scripts/`, `Makefile`, CI workflows) -- must pass `-X` ldflags when building release binaries
- **Behavior:** `go run` without ldflags continues working identically (file fallback)
- **No API/contract changes:** Envelope shape, field names, and schema version are unchanged
- **No schema version bump** -- behavioral change is internal to version resolution; external output is identical
