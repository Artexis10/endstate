# Endstate Semver System

## 1. Version Topology

Three independent version tracks, two repos.

| Version | Scope | Format | Source of Truth | Current |
|---------|-------|--------|-----------------|---------|
| Engine CLI | `endstate` repo | `MAJOR.MINOR.PATCH` | `VERSION` (root) | `0.1.0` |
| JSON Schema | `endstate` repo | `MAJOR.MINOR` | `SCHEMA_VERSION` (root) | `1.0` |
| GUI | `endstate-gui` repo | `MAJOR.MINOR.PATCH` | `package.json` | `0.1.0` |

**Coupling rule:** A schema major bump forces a CLI major bump. GUI tracks its own version independently but declares a compatible schema range.

---

## 2. Engine Repo (`endstate`)

### Version Files

```
endstate/
├── VERSION              # "0.1.0" — single source of truth
├── SCHEMA_VERSION       # "1.0" — schema version, separate track
├── CHANGELOG.md         # Conventional changelog
└── scripts/
    └── bump-version.ps1 # Bump automation
```

**`VERSION`** — plain text, one line, no newline. Read by the Go engine at runtime to populate `cliVersion` in the JSON envelope.

**`SCHEMA_VERSION`** — plain text, one line. Read at runtime for `schemaVersion` in envelope. Separate file because schema version has independent bump semantics.

### Runtime Integration

The Go engine envelope builder (`go-engine/internal/envelope/`) reads both files at build/runtime:

```go
cliVersion := strings.TrimSpace(readFile(filepath.Join(repoRoot, "VERSION")))
schemaVersion := strings.TrimSpace(readFile(filepath.Join(repoRoot, "SCHEMA_VERSION")))
```

These replace any hardcoded version strings currently in the codebase.

### Bump Script: `scripts/bump-version.ps1`

```
Usage:
  .\scripts\bump-version.ps1 -Bump <patch|minor|major>
  .\scripts\bump-version.ps1 -Bump schema-minor
  .\scripts\bump-version.ps1 -Bump schema-major
  .\scripts\bump-version.ps1 -SetVersion "1.2.3"
```

**What it does:**
1. Reads current `VERSION` (and `SCHEMA_VERSION` for schema bumps)
2. Computes new version
3. Writes updated file(s)
4. Prepends entry to `CHANGELOG.md` with `## [x.y.z] - YYYY-MM-DD` header
5. Creates git commit: `chore: bump version to x.y.z`
6. Creates git tag: `v{x.y.z}` (CLI) or `schema-v{x.y}` (schema)

**Schema bump rules enforced by script:**
- `schema-major` also bumps CLI major (forced coupling per contract)
- `schema-minor` does NOT bump CLI version (additive changes)
- Regular `patch`/`minor`/`major` never touch SCHEMA_VERSION

### Git Tags

| Tag Format | Example | Meaning |
|------------|---------|---------|
| `v{semver}` | `v0.2.0` | CLI release |
| `schema-v{major.minor}` | `schema-v1.0` | Schema version marker |

---

## 3. GUI Repo (`endstate-gui`)

### Version Files

```
endstate-gui/
├── package.json                    # version field — source of truth
├── src-tauri/
│   ├── tauri.conf.json             # version field — must match
│   └── Cargo.toml                  # version field — must match
├── src/lib/compat.ts               # ENGINE_SCHEMA_COMPAT range
├── CHANGELOG.md
└── scripts/
    └── bump-version.mjs            # Bump automation
```

Three files must stay in sync: `package.json`, `tauri.conf.json`, `Cargo.toml`.

### Schema Compatibility Declaration

`src/lib/compat.ts`:

```typescript
/**
 * Compatible engine schema version range.
 * Updated when GUI is tested against new engine schema versions.
 */
export const ENGINE_SCHEMA_COMPAT = {
  min: "1.0",
  max: "1.0",
} as const;
```

This is what the GUI checks during the capabilities handshake. It's code, not config, because it's a compile-time contract.

### Bump Script: `scripts/bump-version.mjs`

```
Usage:
  node scripts/bump-version.mjs <patch|minor|major>
  node scripts/bump-version.mjs --set 1.2.3
  node scripts/bump-version.mjs --schema-compat "1.0:2.0"
```

**What it does:**
1. Reads current version from `package.json`
2. Computes new version
3. Writes to all three files atomically:
   - `package.json` → `version` field
   - `src-tauri/tauri.conf.json` → `version` field
   - `src-tauri/Cargo.toml` → `version` field (under `[package]`)
4. Prepends entry to `CHANGELOG.md`
5. Creates git commit: `chore: bump version to x.y.z`
6. Creates git tag: `gui-v{x.y.z}`

**`--schema-compat`** updates `src/lib/compat.ts` with new min:max range.

### Git Tags

| Tag Format | Example | Meaning |
|------------|---------|---------|
| `gui-v{semver}` | `gui-v0.2.0` | GUI release |

---

## 4. Changelog Format

Both repos use the same format. Manual entries, not auto-generated from commits.

```markdown
# Changelog

## [0.2.0] - 2026-03-06

### Added
- Thing that was added

### Changed
- Thing that changed

### Fixed
- Bug that was fixed

## [0.1.0] - 2026-01-01

Initial release.
```

The bump script creates the header and empty sections. You fill in the entries before pushing.

---

## 5. Cross-Repo Compatibility

### Compatibility Matrix (maintained in both repos)

**Engine `docs/COMPATIBILITY.md`:**

```markdown
# Compatibility Matrix

| Engine Version | Schema Version | GUI Version (min) |
|---------------|----------------|-------------------|
| 0.1.x         | 1.0            | gui-v0.1.0+       |
```

**GUI `docs/COMPATIBILITY.md`:**

```markdown
# Compatibility Matrix

| GUI Version | Required Schema | Tested Engine |
|-------------|----------------|---------------|
| 0.1.x       | 1.0            | 0.1.x         |
```

### Coordinated Bumps

When a change spans both repos:

1. Engine: bump version, push
2. GUI: bump version, update `ENGINE_SCHEMA_COMPAT` if needed, push
3. Update compatibility matrix in both repos

For **schema-breaking** changes (rare):

1. Engine: `.\scripts\bump-version.ps1 -Bump schema-major` (also bumps CLI major)
2. GUI: update `ENGINE_SCHEMA_COMPAT`, bump GUI major
3. Both: update compatibility matrices

---

## 6. npm Scripts (Both Repos)

### Engine `package.json` scripts:

```json
{
  "scripts": {
    "version:bump": "pwsh -File scripts/bump-version.ps1",
    "version:show": "pwsh -Command \"Get-Content VERSION\"",
    "version:schema": "pwsh -Command \"Get-Content SCHEMA_VERSION\""
  }
}
```

### GUI `package.json` scripts:

```json
{
  "scripts": {
    "version:bump": "node scripts/bump-version.mjs",
    "version:show": "node -e \"console.log(require('./package.json').version)\"",
    "version:check": "node scripts/check-version-sync.mjs"
  }
}
```

`version:check` verifies all three GUI version files are in sync. Add to pre-push hook.

---

## 7. Validation & Safety

### Pre-push hook additions

**Engine** (add to lefthook.yml):
```yaml
pre-push:
  commands:
    version-valid:
      run: pwsh -Command "if (-not ((Get-Content VERSION -Raw).Trim() -match '^\d+\.\d+\.\d+$')) { exit 1 }"
```

**GUI** (add to lefthook.yml):
```yaml
pre-push:
  commands:
    version-sync:
      run: node scripts/check-version-sync.mjs
```

### What `check-version-sync.mjs` validates:

1. `package.json` version matches `tauri.conf.json` version
2. `package.json` version matches `Cargo.toml` version
3. All three are valid semver
4. `ENGINE_SCHEMA_COMPAT` has valid format

---

## 8. Workflow Summary

### Regular engine change:

```
# Make changes, then:
.\scripts\bump-version.ps1 -Bump patch
# Edit CHANGELOG.md with details
git add -A && git commit --amend --no-edit
git push
```

### Regular GUI change:

```
# Make changes, then:
node scripts/bump-version.mjs patch
# Edit CHANGELOG.md with details
git add -A && git commit --amend --no-edit
git push
```

### Schema-breaking change:

```
# Engine first:
.\scripts\bump-version.ps1 -Bump schema-major
# (Also bumps CLI major automatically)

# GUI second:
node scripts/bump-version.mjs major
node scripts/bump-version.mjs --schema-compat "2.0:2.0"
```

---

## 9. What NOT to Do

- Don't auto-generate changelogs from commits (too noisy for a single-maintainer project)
- Don't use conventional commit enforcement (overhead > value at this scale)
- Don't create release branches (tag-based releases are sufficient)
- Don't version-bump on every commit (bump when shipping)
- Don't put version in filenames (use git tags)

---

## 10. Implementation Checklist

### Engine repo:
- [ ] Create `VERSION` file with `0.1.0`
- [ ] Create `SCHEMA_VERSION` file with `1.0`
- [ ] Create `CHANGELOG.md` with initial entry
- [ ] Create `scripts/bump-version.ps1`
- [ ] Create `docs/COMPATIBILITY.md`
- [ ] Wire `VERSION`/`SCHEMA_VERSION` into envelope builder (replace hardcoded strings)
- [ ] Add npm scripts to `package.json`
- [ ] Add version validation to `lefthook.yml`
- [ ] Add tests: version file format, runtime version injection

### GUI repo:
- [ ] Verify/set `package.json` version to `0.1.0`
- [ ] Sync `tauri.conf.json` and `Cargo.toml` versions
- [ ] Create `src/lib/compat.ts` with schema range
- [ ] Create `CHANGELOG.md` with initial entry
- [ ] Create `scripts/bump-version.mjs`
- [ ] Create `scripts/check-version-sync.mjs`
- [ ] Create `docs/COMPATIBILITY.md`
- [ ] Wire compat check into capabilities handshake
- [ ] Add npm scripts to `package.json`
- [ ] Add version sync check to `lefthook.yml`
- [ ] Add tests: version sync, compat range validation
