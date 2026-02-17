# Design: Capture Bundle — Zip-Based Profile Packaging

## Architecture

### New Engine Scripts

| Script | Purpose |
|--------|---------|
| `engine/bundle.ps1` | Config module matching, file collection, metadata generation, zip creation |

### Modified Engine Scripts

| Script | Change |
|--------|--------|
| `engine/capture.ps1` | Wire bundler into capture pipeline after manifest generation |

### Modified Entrypoint

| Script | Change |
|--------|--------|
| `bin/endstate.ps1` | Profile discovery (zip → folder → bare), apply zip extraction, capture JSON output fields |

## Data Flow

### Capture Pipeline (Updated)

```
winget export → filter → sort → Write-Manifest
                                      │
                                      ▼
                              Match config modules
                              (winget ID lookup)
                                      │
                                      ▼
                              Collect config files
                              (respect excludeGlobs, sensitive.files)
                                      │
                                      ▼
                              Generate metadata.json
                                      │
                                      ▼
                              Stage in temp dir:
                              ├── manifest.jsonc
                              ├── configs/<module-id>/<files>
                              └── metadata.json
                                      │
                                      ▼
                              Compress-Archive → <Profile>.zip
                              (atomic: temp zip → move to Profiles/)
```

### Profile Discovery (Updated)

```
Resolve-ProfilePath("Hugo-Desktop")
  │
  ├─► Check: Documents\Endstate\Profiles\Hugo-Desktop.zip
  │     └─► Found? → Return zip path
  │
  ├─► Check: Documents\Endstate\Profiles\Hugo-Desktop\manifest.jsonc
  │     └─► Found? → Return manifest path
  │
  └─► Check: Documents\Endstate\Profiles\Hugo-Desktop.jsonc
        └─► Found? → Return bare manifest path
```

### Apply from Zip

```
Invoke-ApplyFromZip("Hugo-Desktop.zip")
  │
  ├─► Extract to $env:TEMP\endstate-apply-<guid>\
  │
  ├─► Read manifest.jsonc from extracted dir
  │
  ├─► Install apps (normal apply pipeline)
  │
  ├─► If --enable-restore: restore configs from configs/ dir
  │
  └─► Cleanup temp dir
```

## Config Module Matching

For each app in captured manifest:
1. Load config module catalog (`modules/apps/*/module.jsonc`)
2. Match via `matches.winget` array against captured app's `refs.windows`
3. If match found, iterate `capture.files`:
   - Expand source path (env vars, ~)
   - Skip files matching `capture.excludeGlobs`
   - Skip files listed in `sensitive.files`
   - Copy source → `configs/<module-id>/<dest-filename>`

## Metadata Schema

```json
{
  "schemaVersion": "1.0",
  "capturedAt": "2026-02-16T20:00:00Z",
  "machineName": "DESKTOP-ABC123",
  "endstateVersion": "0.1.0",
  "configModulesIncluded": ["vscode", "claude-desktop"],
  "configModulesSkipped": [],
  "captureWarnings": []
}
```

## Invariants

| ID | Rule |
|----|------|
| INV-BUNDLE-1 | Zip is self-contained — no external file references |
| INV-BUNDLE-2 | Sensitive files never bundled — enforced at capture time |
| INV-BUNDLE-3 | Config capture failures don't block app capture |
| INV-BUNDLE-4 | Install-only is always valid (manifest + metadata, no configs) |
| INV-BUNDLE-5 | Zip is inspectable — unzipped folder is valid loose-folder profile |

## Contracts Affected

- `docs/contracts/capture-artifact-contract.md` — new zip output format
- `docs/contracts/profile-contract.md` — three-format discovery
- `docs/contracts/cli-json-contract.md` — updated capture response schema
- `docs/ai/PROJECT_SHADOW.md` — profile format change
