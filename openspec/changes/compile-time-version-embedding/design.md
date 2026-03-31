## Context

The `version-envelope-injection` spec established `VERSION` and `SCHEMA_VERSION` files as the single sources of truth for CLI and schema versions. This works in development (`go run` from the repo root) but breaks in two production scenarios:

1. **Bootstrapped binaries** -- copied to `%LOCALAPPDATA%\Endstate\bin\` with no VERSION file alongside them, so `ReadVersion` falls through to `"0.0.0-dev"`.
2. **Stale dev checkouts** -- a developer on an old branch gets the old VERSION file content even if their binary was built from a newer tag.

Go's standard `ldflags -X` mechanism solves both by baking version strings into the binary at compile time.

## Goals / Non-Goals

**Goals:**
- Embed `cliVersion` and `schemaVersion` into compiled binaries via `-ldflags -X`
- Preserve full backward compatibility: `go run` without ldflags still works via file fallback
- Keep the resolution chain explicit and testable: ldflags > file > fallback constant
- Expose accessor functions (`EmbeddedVersion`, `EmbeddedSchemaVersion`) for testing and introspection

**Non-Goals:**
- Changing the JSON envelope shape or field names
- Modifying build scripts or CI workflows (separate change)
- Removing VERSION/SCHEMA_VERSION files from the repo
- Auto-detecting whether the binary is "production" vs "dev"

## Decisions

1. **Package-level `var` for ldflags injection** -- Two unexported package-level variables (`version` and `schemaVersion`) in `go-engine/internal/config/version.go` are set via `-ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.version=X.Y.Z -X github.com/Artexis10/endstate/go-engine/internal/config.schemaVersion=A.B"`. Empty string (the zero value) means "not set by ldflags."

2. **Resolution order is ldflags > file > constant** -- `ReadVersion(repoRoot)` first checks the ldflags var; if non-empty, returns it immediately without touching the filesystem. If empty, falls through to the existing file-read logic (which itself falls back to the constant). Same pattern for `ReadSchemaVersion`.

3. **Accessor functions for embedded values** -- `EmbeddedVersion()` and `EmbeddedSchemaVersion()` return the raw ldflags-injected value (empty string if unset). These enable tests and diagnostics to distinguish "ldflags set" from "fell back to file."

4. **No exported setter** -- The ldflags vars are unexported. Tests that need to simulate ldflags-set behavior will test through `ReadVersion`/`ReadSchemaVersion` with file-based fallback, or use `EmbeddedVersion` to observe the zero value. This avoids coupling tests to internal state mutation.

## Risks / Trade-offs

- [Risk] Build script forgets `-ldflags` -> Mitigation: Falls back to file read, which produces the correct version for dev builds; production CI should lint for this
- [Risk] ldflags value diverges from VERSION file content -> Mitigation: Build scripts should read VERSION file to populate ldflags, keeping them in sync; VERSION file remains the canonical version record in the repo
- [Risk] Unexported vars cannot be set in external test packages -> Mitigation: `EmbeddedVersion()` accessor exposes the current state; file-based fallback tests cover the primary code path
- [Trade-off] Adding two package-level vars increases config package surface slightly -> Acceptable: the vars are unexported and the accessors are read-only
