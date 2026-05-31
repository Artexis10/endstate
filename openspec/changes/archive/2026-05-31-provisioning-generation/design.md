## Context

After `nix-realizer-backend` (Phase 1), the engine has two package backends: the winget
`driver.Driver` (per-package `Detect`/`Install`) and the Nix `realizer.Realizer`
(whole-set `Plan`/`Realize`/`Current`). Both converge a package set, but neither leaves a
durable, inspectable record of what was committed. `apply` already threads a `runID` and
writes run history (`state.SaveRunHistory` → `state/runs/<runID>.json`) via the atomic
temp-file + rename idiom (`internal/state`), and resolves paths via
`config.ResolveRepoRoot()` / `state.StateDir()`.

This change adds an engine-owned **Provisioning Generation** above both backends — the
unification layer. It is install-only and additive; it does not touch the config/restore
layers and introduces no rollback.

## Goals / Non-Goals

**Goals:**
- One numbered, versioned, install-only Generation record written after a successful
  `apply`, identically shaped for both backends.
- Backends differ only in advertised **capabilities** (discovered by type-assertion, like
  `driver.BatchDetector`); the engine owns the unified state and the `generations` UX.
- Pin the Generation format now so a later rollback phase writes against a stable schema.
- **Zero Windows behavior regression**, provable by host-aware tests + `GOOS=windows` build.

**Non-Goals:**
- **No rollback** (`Rollbacker` is declared, not implemented).
- **No folding** of winget into a single converge interface — dual `Driver`+`Realizer`
  stays; only state/UX is unified.
- **No config/restore changes** — the Generation never reads/writes `state/backups/` or the
  revert journal (enforced by the `separation-of-concerns` delta + a guard test).
- **No version pinning** — `Items.version` is best-effort; recorded where the backend
  exposes it (Nix), empty on winget until the driver is enriched.

## Decisions

### The record (`internal/provision`)

```go
const SchemaVersion = "1.0" // OWN version, independent of manifest/envelope

type ProvItem struct {
    ID      string `json:"id"`
    Ref     string `json:"ref"`
    Status  string `json:"status"`            // installed | present
    Version string `json:"version,omitempty"` // best-effort
}

type Generation struct {
    SchemaVersion string     `json:"schemaVersion"`
    Number        int        `json:"number"`
    RunID         string     `json:"runId"`
    Timestamp     string     `json:"timestamp"`
    Backend       string     `json:"backend"`   // "nix" | "winget"
    Items         []ProvItem `json:"items"`
    AddedRefs     []string   `json:"addedRefs"` // status=installed this run only
    Native        string     `json:"native,omitempty"` // nix gen number; "" otherwise
    Partial       bool       `json:"partial"`
}

type Capabilities struct{ AtomicSet, NativeRollback, Transactional, BatchInstall bool }
type CapabilityReporter interface{ Capabilities() Capabilities }
type Rollbacker interface{ Rollback(to int) error } // declared; implemented in a later phase
```

### Persistence
- Location: `provision.Dir()` = `filepath.Join(state.StateDir(), "generations")` — resolved
  via the existing resolver, **never hardcoded**.
- File per generation: `state/generations/<zero-padded-number>.json` (e.g. `000001.json`),
  so lexical sort == numeric sort.
- Write: `json.MarshalIndent` → `<path>.tmp` → `os.Rename` (the `state.SaveRunHistory`
  idiom; dirs `0755`, files `0644`).
- `List()` reads the dir, excludes `.tmp`, returns newest-first (mirrors
  `state.ListRunHistory`). Missing dir → empty slice, no error.
- `nextNumber()` = highest existing `Number` + 1 (engine-owned; independent of `Native`).

### Write timing (the only behavioral fork)
| Backend | When a generation is written | `Partial` | `Native` |
|---------|------------------------------|-----------|----------|
| Nix (atomic) | only when the profile generation advanced (`Result.Advanced`) | `false` | `ToGeneration` |
| winget (non-atomic) | when ≥1 ref was `installed` this run | `true` iff any attempted install failed | `""` |

A generation is **never** written when nothing was newly installed (idempotent re-run).
This matches Nix's no-advance-on-no-op semantics and keeps generations meaningful as
(future) rollback points; run history already records every `apply`.

### `AddedRefs`
Only refs whose per-item status this run was `installed`. `present`/`skipped`/`failed`
refs are excluded. `Items` carries the full host-matching declared set with status.

### Capabilities
- Nix realizer reports `{AtomicSet:true, NativeRollback:true, Transactional:true,
  BatchInstall:true}`.
- winget driver reports all-false.
- Discovered via `if cr, ok := backend.(provision.CapabilityReporter); ok { ... }`. No
  Phase-2 consumer beyond tests; previews Phase-3 rollback eligibility.

### `generations` command
Read-only list, modeled on `report.go` / `state.ListRunHistory`. Returns a result struct
for the JSON envelope (`data.generations`) and a human summary. Empty list when none.

## Separation of concerns (inviolable)
`internal/provision` MUST NOT import `internal/restore` and MUST NOT reference
`state/backups/` or the revert journal. A guard test asserts the package's import set
excludes `restore`. `rollback` (packages, future) and `revert` (configs) remain distinct
commands over distinct logs.

## Risks / limitations (documented, not blockers)
- winget exposes no installed **version** today → `Items.version` is empty on Windows until
  the driver is enriched (future).
- GUI is Windows-only and not wired to `generations` in this phase (CLI-first by design).
- Retention: **keep all** generations; no auto-pruning in this phase (records are tiny and
  must persist as future rollback points). A prune policy can be added later.

## Migration
Purely additive. No manifest/envelope schema-version bump (the record carries its own
version; the command and its fields are additive per `schema-versioning`). Existing state,
run history, and Windows behavior are unchanged.
