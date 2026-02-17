# /opsx:new — Capture Bundle: Zip-Based Profile Packaging

## Context

Capture currently produces a bare `.jsonc` manifest. Config export is a separate command (`export-config`). This creates a two-step workflow that breaks the user mental model of "package my machine, transfer, restore."

**Design decision:** Capture should produce a single portable zip artifact containing both the app manifest and all available config module payloads. This becomes the unit of portability.

## Acceptance Criteria

### AC-1: Capture produces a zip
- `endstate capture --profile "Hugo-Desktop"` produces `Documents\Endstate\Profiles\Hugo-Desktop.zip`
- Zip contains:
  ```
  Hugo-Desktop.zip
  ├── manifest.jsonc          # App list (existing capture output)
  ├── configs/                # Config module payloads
  │   ├── vscode/
  │   │   ├── settings.json
  │   │   └── keybindings.json
  │   ├── claude-desktop/
  │   │   └── claude_desktop_config.json
  │   └── ...
  └── metadata.json           # Capture timestamp, machine name, schema version
  ```

### AC-2: Config bundling is automatic
- After app scan, engine checks each captured app against config module catalog (`modules/apps/*/module.jsonc`)
- If a config module matches (via `matches.winget` or `matches.exe`), its `capture.files` are copied into the zip under `configs/<module-id>/`
- No `--include-config` flag needed — configs are always included when available
- If no config modules match any apps, zip still contains manifest + metadata (install-only profile — valid and successful)
- Sensitive files listed in module `sensitive.files` are NEVER included
- `capture.excludeGlobs` are respected

### AC-3: Metadata file
- `metadata.json` contains:
  ```json
  {
    "schemaVersion": "1.0",
    "capturedAt": "2026-02-16T20:00:00Z",
    "machineName": "DESKTOP-ABC123",
    "endstateVersion": "0.1.0",
    "configModulesIncluded": ["vscode", "claude-desktop", "claude-code"],
    "configModulesSkipped": [],
    "captureWarnings": []
  }
  ```

### AC-4: Profile discovery supports three formats
- Discovery in `Documents\Endstate\Profiles\` recognizes:
  1. `<name>.zip` — zip bundle (new, preferred)
  2. `<name>\manifest.jsonc` — loose folder
  3. `<name>.jsonc` — bare manifest (legacy, install-only)
- Resolution order for `--profile "Name"`: zip → folder → bare manifest
- First match wins

### AC-5: Apply accepts zip profiles
- `endstate apply --profile "Hugo-Desktop.zip"` extracts to temp dir, reads manifest, installs apps
- Config restore from zip requires `--enable-restore` (unchanged safety model)
- Temp extraction cleaned up after apply completes
- Apply also still works with loose folders and bare manifests

### AC-6: JSON output updated
- Capture success response gains new fields:
  ```json
  {
    "data": {
      "outputPath": "C:\\Users\\...\\Profiles\\Hugo-Desktop.zip",
      "outputFormat": "zip",
      "configsIncluded": ["vscode", "claude-desktop"],
      "configsSkipped": ["git"],
      "configsCaptureErrors": []
    }
  }
  ```
- Existing fields (`counts`, `appsIncluded`, etc.) unchanged

### AC-7: Existing `export-config` command preserved
- `export-config` continues to work as a standalone power-user command
- No breaking changes to existing commands

## Invariants

### INV-BUNDLE-1: Capture zip is self-contained
- The zip MUST contain everything needed to restore on another machine
- No external file references — all config payloads are embedded

### INV-BUNDLE-2: Sensitive files never bundled
- Files listed in `module.sensitive.files` MUST NOT appear in the zip
- Credentials, tokens, session data — always excluded
- This is enforced at capture time, not at restore time

### INV-BUNDLE-3: Config capture failures don't block app capture
- If a config file is missing or inaccessible, capture still succeeds
- Failed config captures are reported in `configsCaptureErrors` and `metadata.json`
- The manifest is always written

### INV-BUNDLE-4: Install-only is always valid
- A zip with only `manifest.jsonc` + `metadata.json` (no configs) is a valid profile
- No warnings, no errors — matches UX guardrail "install-only profiles are successful outcomes"

### INV-BUNDLE-5: Zip is inspectable
- Users can unzip and review/edit contents before applying
- Unzipped folder is itself a valid loose-folder profile (AC-4 format 2)

## Non-Goals (This Change)

- No `--zip` / `--no-zip` flags — zip is always the output
- No encryption or signing of zips
- No remote transfer protocol (user copies the zip manually)
- No GUI changes in this PR — GUI will be updated separately to handle zip profiles
- No changes to `restore` or `revert` commands (they work from extracted state)

## Implementation Notes

### Engine Changes
- `engine/capture.ps1` — after manifest generation, run config collection and zip packaging
- New helper: `engine/bundle.ps1` (or similar) — handles config module matching, file collection, zip creation
- Profile discovery: `engine/manifest.ps1` or new `engine/profiles.ps1` — resolution logic for zip/folder/bare

### Config Module Matching
- For each app in captured manifest, check `modules/apps/*/module.jsonc`
- Match via `matches.winget` array against captured app's winget ID
- If match found, iterate `capture.files` and copy source → `configs/<module-id>/<dest>`
- Skip files matching `capture.excludeGlobs`
- Skip files in `sensitive.files`

### Zip Creation
- Use PowerShell `Compress-Archive` or .NET `System.IO.Compression.ZipFile`
- Stage files in temp directory, then zip to final location
- Atomic: write to temp zip, then move to Profiles directory

## Contracts to Update
- `docs/contracts/capture-artifact-contract.md` — new zip output format
- `docs/contracts/profile-contract.md` — three-format discovery
- `docs/contracts/cli-json-contract.md` — updated capture response schema
- `docs/ai/PROJECT_SHADOW.md` — profile format change

## OpenSpec

### New spec: `openspec/specs/capture-bundle-zip.md`
### Change: `openspec/changes/capture-bundle-zip/`

---

# /opsx:ff — Fast-Forward Checklist

Before implementation, verify:
- [ ] Existing capture tests still pass
- [ ] `export-config` command unaffected
- [ ] Profile discovery handles all three formats without regression
- [ ] Config module matching logic handles missing modules gracefully
- [ ] Sensitive file exclusion works for all modules with `sensitive` blocks

---

# /opsx:apply — Implementation Order

1. **OpenSpec change** — create `openspec/changes/capture-bundle-zip/` with proposal, design, tasks
2. **OpenSpec spec** — create `openspec/specs/capture-bundle-zip.md` with invariants
3. **Contract updates** — update profile-contract.md, capture-artifact-contract.md, cli-json-contract.md
4. **Engine: config module matcher** — function to match captured apps → config modules
5. **Engine: config collector** — function to copy capture.files, respecting excludes and sensitive
6. **Engine: zip bundler** — stage manifest + configs + metadata, produce zip
7. **Engine: capture.ps1 integration** — wire bundler into capture pipeline
8. **Engine: profile discovery** — update resolution to handle zip/folder/bare
9. **Engine: apply.ps1 integration** — extract zip to temp, apply, cleanup
10. **Tests** — unit tests for matcher, collector, bundler, discovery
11. **PROJECT_SHADOW.md** — update profile format documentation

---

# /opsx:verify — Verification Plan

```powershell
# Unit tests
.\scripts\test-unit.ps1

# Manual smoke test
endstate capture --profile "Test-Bundle" --json
# Verify: zip exists at Documents\Endstate\Profiles\Test-Bundle.zip
# Verify: zip contains manifest.jsonc, metadata.json, configs/ (if modules matched)
# Verify: no sensitive files in zip

# Apply from zip
endstate apply --profile "Test-Bundle" --dry-run --json
# Verify: reads manifest from zip, plans installs correctly

# Legacy compat
endstate apply --manifest .\some-bare-manifest.jsonc --dry-run --json
# Verify: still works unchanged
```

---

# /opsx:archive — Post-Merge

- Update GUI issue tracker: "Support zip profile format in profile discovery and apply flow"
- Update README capture documentation
- Consider: `endstate profile inspect <name>` command to list zip contents without extracting
