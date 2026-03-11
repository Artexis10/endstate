## Context

Endstate's PowerShell engine (`bin/endstate.ps1` ~4700 lines) implements a declarative machine provisioning CLI for Windows. Consumers (GUI via Tauri, scripts, CI) interact via structured JSON envelopes on stdout and NDJSON events on stderr. The protocol is versioned and contract-tested. Phase 1 rewrites the foundation (capabilities, verify, apply) in Go while producing byte-identical protocol output.

## Goals / Non-Goals

**Goals:**
- Go binary that satisfies cli-json-contract, event-contract, gui-integration-contract, and profile-contract
- Implement `capabilities`, `verify`, and `apply` commands with identical output envelopes
- JSONC manifest loading with includes resolution and circular detection
- Winget driver (detect + install) on Windows
- NDJSON event streaming to stderr
- Table-driven Go tests covering envelope shape, manifest parsing, event format

**Non-Goals:**
- Capture, restore, state persistence, bundles, modules, report, bootstrap, profile commands (Phase 2-3)
- GUI changes or Tauri integration changes
- Any modification to existing PowerShell code
- New behaviors or protocol extensions beyond what PowerShell already implements

## Decisions

### 1. No external CLI framework
**Decision:** Manual `os.Args` parsing, no cobra/urfave.
**Rationale:** The PowerShell engine uses manual arg parsing. Endstate's CLI grammar is simple (`endstate <command> [--flags]`). An external framework adds dependency weight and API surface for no benefit. Keeps the binary small and the arg handling transparent.
**Alternative:** cobra — rejected because it imposes conventions (subcommand structs, flag binding) that don't match the existing simple dispatch model.

### 2. JSONC comment stripping before stdlib JSON unmarshal
**Decision:** Implement a `StripJsoncComments()` function that removes `//` line comments and `/* */` block comments, then pass clean JSON to `encoding/json`.
**Rationale:** Ports the existing `Read-JsoncFile` logic exactly. Avoids external JSONC libraries that may handle edge cases differently than the PowerShell implementation.

### 3. Driver interface for cross-platform extensibility
**Decision:** Define a `Driver` interface with `Detect(ref string)` and `Install(ref string)` methods. Winget implements it. Future `brew`/`apt` drivers implement the same interface.
**Rationale:** The PowerShell engine already uses a driver registry pattern (`drivers/driver.ps1`). The Go interface makes this explicit and compile-time checked.

### 4. Envelope construction centralized in `internal/envelope/`
**Decision:** Single `Envelope` struct with `NewEnvelope()` constructor and `Write(io.Writer)` method. All commands return data to be wrapped, not raw JSON.
**Rationale:** Ensures every command produces structurally identical envelopes. Matches the PowerShell pattern where `New-JsonEnvelope` is the single point of envelope creation.

### 5. Events via dedicated Emitter struct
**Decision:** `Emitter` struct holds runId and enabled flag. Methods like `EmitPhase()`, `EmitItem()` write NDJSON to stderr. Disabled emitter is a no-op.
**Rationale:** Mirrors `Enable-StreamingEvents` / `Write-StreamingEvent` pattern. The enabled flag avoids event emission when `--events jsonl` is not passed.

### 6. Version reading from repo root files
**Decision:** Read `VERSION` and `SCHEMA_VERSION` plain-text files from repo root, resolved via `ENDSTATE_ROOT` env var or walking up from binary location.
**Rationale:** Matches PowerShell behavior. Single source of truth for version across both engines during transition period.

## Risks / Trade-offs

- **[Risk] Behavioral divergence from PowerShell** → Mitigated by using existing contract tests and fixtures as acceptance gates. Both engines read the same VERSION/SCHEMA_VERSION files.
- **[Risk] Winget output format changes** → Mitigated by exit-code-based detection (not output parsing for success/fail). Document heuristic patterns as unreliable per event-contract.
- **[Risk] JSONC edge cases differ between implementations** → Mitigated by testing against all existing fixtures in `tests/fixtures/`.
- **[Trade-off] No external deps means more boilerplate** → Acceptable for Phase 1 scope. CLI parsing and JSONC stripping are small implementations.
