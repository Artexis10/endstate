## Context

Phase 1–2 of the Go engine rewrite established the CLI framework, JSON envelope, NDJSON events, manifest loading (JSONC), winget driver, snapshot, and commands for capabilities, apply, verify, capture, plan, report, doctor, and profile. All code lives in `go-engine/` using manual os.Args parsing, no external CLI framework. The PowerShell engine (`engine/`, `restorers/`, `verifiers/`, `modules/`) remains authoritative for behavior reference.

Phase 3 completes the rewrite: restore engine, config modules, bundles, verifiers, export, revert, and bootstrap. After this, the Go binary implements every command the PowerShell engine does.

## Goals / Non-Goals

**Goals:**
- Implement all four restore strategies (copy, merge-json, merge-ini, append) matching PowerShell behavior
- Backup-before-overwrite with SHA256 up-to-date detection
- Restore journaling and journal-based revert per config-portability-contract.md
- Config module catalog loading and configModules expansion in manifests
- Bundle zip creation/extraction with path rewriting (./payload/apps/ → ./configs/)
- Three verifier types (file-exists, command-exists, registry-key-exists)
- Export-config, validate-export, revert, bootstrap commands
- Wire --enable-restore into apply pipeline (after install, before verify)
- Full test coverage for all new packages

**Non-Goals:**
- GUI integration (GUI reads JSON envelopes — no GUI code changes)
- Changing any PowerShell files
- New behavior not in the PowerShell engine
- Cross-platform support beyond Windows (registry checks are Windows-only by design)
- Performance optimization of restore (correctness first)

## Decisions

### D1: Restore strategy dispatch via type field
Each restore entry has a `type` field (copy, merge-json, merge-ini, append). The restore orchestrator dispatches to the correct strategy function. No interface/plugin system — direct switch dispatch, matching the simplicity of Phase 1–2.

**Alternative:** Strategy interface pattern with registration. Rejected: over-engineered for four fixed strategies.

### D2: Backup storage layout
Backups go to `state/backups/<timestamp>/` with files identified by SHA256 hash of target path. This matches the PowerShell `restorers/helpers.ps1` pattern.

### D3: Config module JSONC parsing
Reuse `manifest.StripJsoncComments()` (already exported from Phase 1) for module.jsonc files. Module types are separate from manifest types but share the same JSONC parsing.

### D4: Bundle zip via archive/zip stdlib
Use Go's `archive/zip` stdlib — no external dependency. The PowerShell engine uses `Compress-Archive`; the Go stdlib is more capable and handles the configs/ layout natively.

### D5: Registry verifier via golang.org/x/sys/windows/registry
The registry-key-exists verifier needs Windows registry access. Use `golang.org/x/sys/windows/registry` — the standard Go approach. Guard with `runtime.GOOS == "windows"` and return a clear error on other platforms.

### D6: Bootstrap copies the running binary
`os.Executable()` gives the path to the running binary. Bootstrap copies it to `%LOCALAPPDATA%\Endstate\bin\lib\endstate.exe` and creates a CMD shim. Simpler than PowerShell's approach since we're already a compiled binary.

### D7: Source resolution follows Model B
Per config-portability-contract.md §4: when ExportRoot is set, resolve from ExportRoot first, fallback to ManifestDir. When not set, resolve from ManifestDir only. This is implemented in the restore orchestrator, not in individual strategies.

## Risks / Trade-offs

- **[Risk] Locked file handling on Windows** → Mitigation: Copy strategy catches sharing violations, adds warning, continues. Matches PowerShell behavior. Does not fail the overall restore.
- **[Risk] Registry API is Windows-only** → Mitigation: Build tag or runtime.GOOS guard. Tests on non-Windows return "not supported" result.
- **[Risk] Module catalog may have malformed JSONC** → Mitigation: Skip invalid modules with warning, don't fail catalog load. Matches PowerShell's graceful degradation.
- **[Risk] Bundle path rewriting is a known bug surface** → Mitigation: Dedicated test cases for ./payload/apps/ → ./configs/ rewriting. This was a real bug in the PowerShell engine.
- **[Trade-off] New go.mod dependency (golang.org/x/sys)** → Acceptable: it's the official Go extended library, widely used, minimal attack surface.
