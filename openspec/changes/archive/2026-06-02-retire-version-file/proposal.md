## Why

The repo-root `VERSION` file is a **vestigial orphan** frozen at `2.0.0` while the project is at
`2.12.1`. Nothing reads its content for the engine version anymore — the single source of truth
migrated to `.release-please-manifest.json`:

- `config.ReadVersion` (`go-engine/internal/config/version.go`) returns the compile-time ldflags
  version (release builds) or falls back to `.release-please-manifest.json` (dev/`doctor`) — it never
  reads `VERSION`.
- The engine release workflow embeds the version from the **git tag**, and reads only `SCHEMA_VERSION`.
- The GUI (`endstate-gui`) `rebuild-engine.cjs` was deliberately changed to read the manifest, with a
  comment citing this exact problem (engine issue **#54**); it never reads `VERSION` either.

So `VERSION` is read by nothing functional in either repo. What still *touches* it is stale tooling:
the `version-format` pre-push hook (format check only — and a silent no-op on Linux/WSL, see below),
`scripts/bump-version.ps1` (a manual bump tool that is now **incompatible with release-please** — it
would mint competing tags/CHANGELOG), and the `package.json` `version:bump`/`version:show` scripts.

Worse, the `version-envelope-injection` OpenSpec spec still declares *"VERSION file is the source of
truth for cliVersion,"* which has **already drifted** from the implementation. This change makes the
spec match reality and removes the orphan. It **Closes #54**.

## What Changes

- **Retire `VERSION`.** Delete the file. `SCHEMA_VERSION` stays — it is genuinely consumed (engine
  `release.yml` ldflags + the GUI's `rebuild-engine.cjs`).
- **Retire `scripts/bump-version.ps1`** and the `version:bump` npm script (release-please owns CLI
  versioning; the manual bumper conflicts with it). Repoint `version:show` to read the manifest; keep
  `version:schema`.
- **Fix the `version-format` pre-push hook.** Today it inlines `$v`/`$s` which the POSIX shell that
  lefthook uses on Linux/WSL expands to empty *before* `pwsh` runs — so the check is a silent no-op
  there (it errors non-fatally and exits 0). Move it to `scripts/version_format.ps1` invoked via
  `pwsh -NoProfile -File` (matching the working `openspec-validate` hook), and validate
  `SCHEMA_VERSION` only.
- **Spec:** update `version-envelope-injection` so the source of truth for `cliVersion` is the
  release-please manifest (+ compile-time ldflags), not the `VERSION` file; remove the now-obsolete
  `VERSION`-file and `bump-version.ps1` requirements. `SCHEMA_VERSION` requirements are unchanged.
- **Docs/comments:** add a current-state note to `docs/SEMVER_SYSTEM.md`; fix the stale `doctor.go`
  comment ("reads … from the VERSION file" → from the manifest).
- **Housekeeping:** delete the stray tracked `openspec/specs/version-envelope-injection.bak/`
  directory (an accidental backup duplicating the now-stale spec).

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `version-envelope-injection`: the source of truth for `cliVersion` becomes the release-please
  manifest (+ compile-time ldflags from the git tag) rather than the `VERSION` file; the
  `VERSION`-file and `bump-version.ps1`-bump requirements are removed. `SCHEMA_VERSION` as the source
  of truth for `schemaVersion` is unchanged.

## Impact

- `VERSION` — **deleted**.
- `scripts/bump-version.ps1` — **deleted**; `scripts/version_format.ps1` — **new** (hook helper).
- `lefthook.yml` — `version-format` hook → external script, `SCHEMA_VERSION`-only, WSL-safe.
- `package.json` — drop `version:bump`, repoint `version:show` to the manifest, keep `version:schema`.
- `go-engine/internal/commands/doctor.go` — fix stale comment (no behavior change).
- `docs/SEMVER_SYSTEM.md` — current-state note.
- `openspec/specs/version-envelope-injection.bak/` — **deleted** (stray tracked backup).
- **Zero engine behavior change.** Nothing reads `VERSION` at runtime; `go test ./...` and the
  released/dev version reporting are unaffected. Closes #54.
