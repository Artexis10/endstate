## Context

`config.ReadVersion(repoRoot)` resolves the engine version in this order:
1. compile-time `version` var (set via ldflags; engine `release.yml` derives it from the **git tag**);
2. else `.release-please-manifest.json` (`{".": "2.12.1"}`);
3. else `"0.0.0-dev"`.

It never reads the `VERSION` file. `ResolveRepoRoot` uses `.release-please-manifest.json` as the
marker (not `VERSION`). The GUI build (`endstate-gui/scripts/rebuild-engine.cjs`) reads the manifest
for ldflags and reads `SCHEMA_VERSION`; it does not read `VERSION` (comment cites engine #54). So
`VERSION`'s content is dead. The `version-envelope-injection` spec, however, still mandates it.

## Goals / Non-Goals

**Goals:**
- Remove the `VERSION` orphan and the tooling that still reads/writes it, without changing any engine
  behavior or version reporting.
- Make `version-envelope-injection` match the implemented source of truth.
- Fix the `version-format` pre-push hook so it actually validates on Linux/WSL.

**Non-Goals:**
- **No change to `SCHEMA_VERSION`** — it is still read by `release.yml` ldflags and the GUI build.
- **No change to `release.yml`** (PROTECTED) — it already derives the version from the git tag and
  reads only `SCHEMA_VERSION`; nothing to do.
- **No new version-bump tool** — release-please owns CLI versioning; `SCHEMA_VERSION` is edited by
  hand (it changes rarely).

## Decisions

- **`cliVersion` source of truth = release-please manifest (+ ldflags).** Already true in code; the
  spec is updated to say so.
- **Retire `bump-version.ps1` rather than repoint it.** It is not in any CI/release path (only
  `npm run version:bump`), and running it now would conflict with release-please (competing tag +
  CHANGELOG). The spec's `bump-version.ps1` "schema-major forces CLI-major" requirement is removed.
- **Hook fix = external `.ps1` via `-File`** (not inline `$`-escaping). lefthook runs `run:` strings
  through POSIX `sh` on Linux/WSL, which expands `$v`/`$s` to empty before `pwsh` sees them. A
  `-File` script has no `$var` on the shell command line, so `sh` can't touch it — the same reason the
  sibling `openspec-validate` hook already works on WSL. Validate `SCHEMA_VERSION` format only.
- **Delete the stray `version-envelope-injection.bak/` spec.** It is a tracked accidental backup that
  duplicates the (now-updated) spec; OpenSpec was treating it as a real capability.

## Spec delta (`version-envelope-injection`)

- **REMOVED** "VERSION file is the source of truth for cliVersion" (file retired) and "Schema major
  bump forces CLI major bump" (`bump-version.ps1` retired; release-please owns CLI versioning).
- **ADDED** "Release-please manifest is the source of truth for cliVersion" — `config.ReadVersion`
  returns the ldflags version (release) or the manifest `.` value (dev), never a hardcoded string.
- **MODIFIED** "No hardcoded version strings in envelope construction" — `cliVersion` traces back to
  `config.ReadVersion` (manifest/ldflags), not a `VERSION`-file read.
- Unchanged: `SCHEMA_VERSION` source of truth, capture-bundle shared version functions, capabilities
  `supportedSchemaVersions` from `SCHEMA_VERSION`.

## Risks / Verification

- **Engine unaffected:** nothing in `go-engine` reads `VERSION`; `go test ./...` + `GOOS=windows`
  build/vet stay green. `doctor`/dev builds still report the manifest version.
- **Hook dogfooded:** the push that opens the PR runs the *new* `version-format` hook; confirm
  `scripts/version_format.ps1` validates `SCHEMA_VERSION` and exits non-zero on a malformed value
  (tested locally on WSL, the environment where the old hook silently passed).
- **OpenSpec:** `openspec validate retire-version-file --strict` + `npm run openspec:validate`
  (the `.bak` removal lowers the spec count by one).
