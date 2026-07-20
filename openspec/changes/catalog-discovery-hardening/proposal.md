# Proposal: catalog-discovery-hardening

## Why

**A PATH-invoked Endstate silently captures apps with none of their settings.**

`config.ResolveRepoRoot` resolves only via `ENDSTATE_ROOT` or by walking up from the
executable for `.release-please-manifest.json`. `RunBootstrap` copies the binary and the
`.cmd` shim and writes no such marker, so an install at `%LOCALAPPDATA%\Endstate\bin\`
resolves **no root at all**. Capture then short-circuits its catalog load, matches zero config
modules, and writes an app-list-only manifest — with no warning, no envelope note, nothing
distinguishing it from a full capture.

Config portability is the entire difference between Endstate and `winget export`. For CLI
users it has been silently absent.

Verified end-to-end against a real binary in a synthetic install layout, `ENDSTATE_ROOT`
unset:

| | `doctor` state-dir check |
|---|---|
| Before | `"status":"fail"` — `"cannot resolve repo root"` |
| After | `"status":"pass"` — `<install>\bin\state` |

Contributing findings:

- The GUI lane is unaffected — it spawns the engine with `ENDSTATE_ROOT=<exe_dir>\engine`,
  and that tree holds the current 357 modules. The bug is CLI-specific.
- `bin\modules\apps` on an existing install holds **76** modules, a leftover of the retired
  PowerShell installer. Nothing resolved `bin\` as a root, so it was dead weight rather than
  an active wrong-catalog hazard — but once `bin\` becomes resolvable it must be refreshed,
  not adopted as-is.
- `repo-root.txt` in the install dir has **zero consumers** in Go, PowerShell, or TypeScript.
- Development masks all of this: a repo checkout has the marker, and dev shells set
  `ENDSTATE_ROOT`.

## What Changes

- **Resolution gains an installed-layout step.** After the marker walk fails, walk up again
  for a directory containing `modules/apps`. From `<install>\bin\lib\endstate.exe` this
  resolves `<install>\bin`. Ordering is deliberate — the new step runs only where resolution
  previously returned `""`, so `ENDSTATE_ROOT` and repo checkouts are unaffected and existing
  behavior is byte-identical.
- **Bootstrap installs the catalog.** `modules/` and `payload/` are copied into the install
  directory from the resolved source root, replacing rather than merging so a refresh drops
  modules deleted upstream. Absent source trees are skipped, not fatal: a bare binary
  downloaded outside a repo or GUI layout still gets its shim and PATH entry. Re-bootstrapping
  from an already-installed binary resolves the install as its own source; that is detected
  and treated as already-current rather than deleting the tree being copied.
- **Capture warns instead of degrading silently.** A new `module_catalog_unavailable` warning
  fires when the root is unresolvable or the catalog fails to load.
- **`BootstrapData.catalogInstalled`** reports which trees landed, so the absence of a catalog
  is visible to clients rather than inferred.

A catalog that loads successfully with zero modules is deliberately **not** a warning
condition. That is a correctly wired install that matched nothing; conflating it with a broken
one would fire the warning on ordinary captures and train users to ignore it.

## Capabilities

### New Capabilities
- `catalog-discovery`: repo-root resolution finds an installed layout, and capture reports
  when no catalog is reachable.

### Modified Capabilities
<!-- none — bootstrap's existing spec covers install mechanics; catalog installation is
     additive to its data payload -->

## Impact

- `go-engine/internal/config/paths.go` — resolution step 3; `walkUpFor` extracted so both
  walks share one implementation and are unit-testable without faking `os.Executable`.
- `go-engine/internal/commands/bootstrap.go` — `installCatalog`, `copyTree`, `samePath`;
  `BootstrapData.CatalogInstalled`.
- `go-engine/internal/commands/bootstrap_windows.go` — catalog install step, non-fatal.
- `go-engine/internal/commands/capture.go` — `module_catalog_unavailable` warning; the
  catalog-wired check is now distinct from the catalog-non-empty check.
- Backward-compatible. No envelope schema bump: the warning uses the existing
  `CommandWarning` channel and `catalogInstalled` is an additive field.
- Not affected: the GUI lane, event schema, module format, restore, apply.

## Risk

The new resolution step makes any ancestor directory containing `modules/apps` a candidate
root. In practice the walk starts from the executable's directory, so this only triggers for
binaries deliberately installed beside a catalog. `ENDSTATE_ROOT` remains the override for
anything ambiguous.
