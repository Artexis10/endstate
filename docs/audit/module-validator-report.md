# Module Validator Audit Report
## Date: 2026-02-22

## Summary
- Modules scanned: 70
- Schema valid: 62
- Schema errors: 8
- Safety violations: 0
- Symmetry mismatches: 3
- Path issues: 4
- Duplicate IDs: 0

---

## Schema Errors

| Module | Issue |
|--------|-------|
| `apps.premiere-pro` | Missing `matches.winget` field (only has `exe` and `uninstallDisplayName`) |
| `apps.after-effects` | Missing `matches.winget` field (only has `exe` and `uninstallDisplayName`) |
| `apps.evga-precision-x1` | Missing `matches.winget` field (only has `exe` and `uninstallDisplayName`) |
| `apps.powertoys` | `restore[0].source` uses `./apps/powertoys` instead of `./payload/apps/powertoys` (wrong path prefix) |
| `apps.powertoys` | `restore[0]` missing `optional` field |
| `apps.powertoys` | `restore[0]` has non-standard `exclude` array field instead of relying on `capture.excludeGlobs` |
| `apps.msi-afterburner` | `restore[].source` uses `./configs/msi-afterburner/` instead of `./payload/apps/msi-afterburner/` (wrong path prefix) |
| `apps.msi-afterburner` | `capture` section missing `excludeGlobs` field |
| `apps.lightroom-classic` | Missing `sensitive` section (large config module with 26 directories should declare sensitive files) |

### Notes on missing `matches.winget`
Three Adobe/vendor-only modules (`premiere-pro`, `after-effects`, `evga-precision-x1`) are missing the `winget` key entirely from `matches`. Other vendor-only modules (`lightroom-classic`, `capture-one`, `dxo-photolab`, `davinci-resolve`, `ableton-live`) correctly include `"winget": []` (empty array). The three flagged modules should add `"winget": []` for schema consistency.

---

## Safety Violations

No safety violations found. All modules follow security guardrails:
- No browser profile data captured (brave, discord, signal, telegram correctly block/exclude)
- No credential stores captured in any module
- All `sensitive` sections use either `"restorer": "warn-only"` or `"restorer": "block"`
- High-sensitivity modules (brave, discord, signal, telegram, wireguard) correctly use empty restore/capture or block restorer

---

## Capture/Restore Symmetry Mismatches

### 1. `apps.ableton-live` -- Capture splits what restore treats as whole

**Restore** copies `./payload/apps/ableton-live/User Library` to `%USERPROFILE%\Documents\Ableton\User Library` (whole directory).

**Capture** has two separate entries:
- `%USERPROFILE%\Documents\Ableton\User Library\Presets` -> `apps/ableton-live/User Library/Presets`
- `%USERPROFILE%\Documents\Ableton\User Library\Defaults` -> `apps/ableton-live/User Library/Defaults`

This means capture grabs only `Presets/` and `Defaults/` subdirectories, but restore would overwrite the entire `User Library/` directory. On a round-trip, any other `User Library/` subdirectories would be lost. Either restore should target the two subdirectories separately, or capture should grab the entire `User Library/`.

### 2. `apps.wireguard` -- Capture without corresponding restore

**Capture** includes `%APPDATA%\WireGuard\wireguard.exe.config`.
**Restore** is empty (`[]`).

The captured app config file has no restore entry. While tunnel `.conf` files are correctly excluded for security, the app-level config could have a restore entry with `backup: true` and `optional: true`. This may be intentional (install-only intent) but the capture entry suggests config is worth preserving.

### 3. `apps.claude-code` -- Capture includes file not in restore (by design)

**Capture** includes `%USERPROFILE%\.claude.json` (dest: `apps/claude-code/claude-root.json`).
**Restore** has no entry for this file.

Documented as intentional in the module notes -- the file contains mixed OAuth session data alongside MCP server config. Captured for reference but not auto-restored. This is a **design decision, not a bug**, but noted for completeness.

---

## Path Issues

### 1. `apps.msi-afterburner` -- Hardcoded absolute paths

Both `verify`, `restore`, and `capture` use hardcoded `C:\Program Files (x86)\MSI Afterburner\` paths instead of environment variable equivalents. Should use `%ProgramFiles(x86)%\MSI Afterburner\` or equivalent.

**verify:**
```json
{ "type": "file-exists", "path": "C:\\Program Files (x86)\\MSI Afterburner\\MSIAfterburner.exe" }
```

**restore sources:**
```json
"target": "C:\\Program Files (x86)\\MSI Afterburner\\MSIAfterburner.cfg"
"target": "C:\\Program Files (x86)\\MSI Afterburner\\Profiles"
```

**capture sources:**
```json
"source": "C:\\Program Files (x86)\\MSI Afterburner\\MSIAfterburner.cfg"
"source": "C:\\Program Files (x86)\\MSI Afterburner\\Profiles"
```

### 2. `apps.msi-afterburner` -- Wrong source path prefix in restore

Uses `./configs/msi-afterburner/` instead of `./payload/apps/msi-afterburner/`. All other modules use `./payload/apps/<id>/` convention.

### 3. `apps.powertoys` -- Wrong source path prefix in restore

Uses `./apps/powertoys` instead of `./payload/apps/powertoys`. All other modules use `./payload/apps/<id>/` convention.

### 4. `apps.notepad-plus-plus` -- Hardcoded path in verify (minor)

Verify uses `C:\Program Files\Notepad++\notepad++.exe`. Could use `%ProgramFiles%\Notepad++\notepad++.exe`. However, several other modules also use `%ProgramFiles%` which maps to `C:\Program Files` -- this is acceptable but inconsistent with the stated rule of no hardcoded `C:\` paths.

### Other modules with `%ProgramFiles%` in verify paths (acceptable pattern):
These modules use `%ProgramFiles%` env var correctly in verify: vlc, everything, handbrake, blender, inkscape, qbittorrent, neovim, calibre, rainmeter, imageglass, thunderbird, winscp. This is acceptable since `%ProgramFiles%` is an environment variable, not a hardcoded user path.

### Modules with `C:\Program Files` in verify only (borderline):
- `apps.autohotkey`: `C:\Program Files\AutoHotkey\v2\AutoHotkey64.exe`
- `apps.notepad-plus-plus`: `C:\Program Files\Notepad++\notepad++.exe`
- `apps.wireguard`: `C:\Program Files\WireGuard\wireguard.exe`

These should use `%ProgramFiles%` for consistency, though verify paths are read-only operations and less critical than restore/capture paths.

---

## Consistency Issues

### No duplicate module IDs
All 70 modules have unique `id` values matching the `apps.<dirname>` convention.

### No duplicate winget IDs
All winget IDs across modules are unique. No conflicts detected.

### Missing `curation` section
The following modules lack the `curation` section that most curated modules have:

| Module | Has capture? | Notes |
|--------|-------------|-------|
| `apps.msi-afterburner` | Yes | Legacy module -- needs full overhaul |
| `apps.powertoys` | Yes | Legacy module -- needs standardization |
| `apps.filezilla` | Yes | Should have curation for seed/snapshot |
| `apps.gimp` | Yes | Should have curation for seed/snapshot |
| `apps.docker-desktop` | Yes | Minor -- simple single-file module |
| `apps.musicbee` | Yes | Should have curation |
| `apps.vlc` | Yes | Minor -- simple module |
| `apps.everything` | Yes | Minor -- simple module |
| `apps.handbrake` | Yes | Minor -- simple module |
| `apps.blender` | Yes | Should have curation |
| `apps.inkscape` | Yes | Should have curation |
| `apps.qbittorrent` | Yes | Minor -- simple module |
| `apps.neovim` | Yes | Should have curation |
| `apps.calibre` | Yes | Should have curation |
| `apps.rainmeter` | Yes | Should have curation |
| `apps.flow-launcher` | Yes | Should have curation |
| `apps.imageglass` | Yes | Minor -- simple module |
| `apps.espanso` | Yes | Should have curation |
| `apps.thunderbird` | Yes | Should have curation |
| `apps.winscp` | Yes | Minor -- single-file module |
| `apps.paint-net` | Yes | Should have curation |
| `apps.notepad-plus-plus` | Yes | Should have curation |
| `apps.autohotkey` | Yes | Should have curation |

Install-only modules without curation (acceptable -- no config to curate):
`apps.brave`, `apps.discord`, `apps.spotify`, `apps.signal`, `apps.telegram`, `apps.7zip`, `apps.putty`

### Restore entries missing `backup: true`
All non-empty restore entries across all modules include `backup: true`. No violations.

### Non-standard restore entry fields
- `apps.powertoys`: `restore[0]` includes `"exclude"` array -- this is not a standard restore entry field. Standard pattern is to rely on `capture.excludeGlobs` for filtering. The exclude should be part of capture-level configuration, not restore-level.

---

## Per-Module Results

| # | Module | Schema | Symmetry | Safety | Paths | Curation | Result |
|---|--------|--------|----------|--------|-------|----------|--------|
| 1 | `apps.7zip` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 2 | `apps.ableton-live` | PASS | FAIL | PASS | PASS | PASS | FAIL |
| 3 | `apps.affinity-photo` | PASS | PASS | PASS | PASS | PASS | PASS |
| 4 | `apps.after-effects` | FAIL | PASS | PASS | PASS | PASS | FAIL |
| 5 | `apps.audacity` | PASS | PASS | PASS | PASS | PASS | PASS |
| 6 | `apps.autohotkey` | PASS | PASS | PASS | WARN | MISSING | WARN |
| 7 | `apps.blender` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 8 | `apps.brave` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 9 | `apps.calibre` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 10 | `apps.capture-one` | PASS | PASS | PASS | PASS | PASS | PASS |
| 11 | `apps.claude-code` | PASS | WARN (by design) | PASS | PASS | PASS | PASS |
| 12 | `apps.claude-desktop` | PASS | PASS | PASS | PASS | PASS | PASS |
| 13 | `apps.cursor` | PASS | PASS | PASS | PASS | PASS | PASS |
| 14 | `apps.davinci-resolve` | PASS | PASS | PASS | PASS | PASS | PASS |
| 15 | `apps.directory-opus` | PASS | PASS | PASS | PASS | PASS | PASS |
| 16 | `apps.discord` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 17 | `apps.ditto` | PASS | PASS | PASS | PASS | PASS | PASS |
| 18 | `apps.docker-desktop` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 19 | `apps.dxo-photolab` | PASS | PASS | PASS | PASS | PASS | PASS |
| 20 | `apps.espanso` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 21 | `apps.evga-precision-x1` | FAIL | PASS | PASS | PASS | PASS | FAIL |
| 22 | `apps.everything` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 23 | `apps.fastrawviewer` | PASS | PASS | PASS | PASS | PASS | PASS |
| 24 | `apps.filezilla` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 25 | `apps.fl-studio` | PASS | PASS | PASS | PASS | PASS | PASS |
| 26 | `apps.flow-launcher` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 27 | `apps.foobar2000` | PASS | PASS | PASS | PASS | PASS | PASS |
| 28 | `apps.gimp` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 29 | `apps.git` | PASS | PASS | PASS | PASS | PASS | PASS |
| 30 | `apps.handbrake` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 31 | `apps.hwinfo` | PASS | PASS | PASS | PASS | PASS | PASS |
| 32 | `apps.imageglass` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 33 | `apps.inkscape` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 34 | `apps.intellij-idea` | PASS | PASS | PASS | PASS | PASS | PASS |
| 35 | `apps.keepassxc` | PASS | PASS | PASS | PASS | PASS | PASS |
| 36 | `apps.kodi` | PASS | PASS | PASS | PASS | PASS | PASS |
| 37 | `apps.lightroom-classic` | WARN | PASS | PASS | PASS | PASS | WARN |
| 38 | `apps.logseq` | PASS | PASS | PASS | PASS | PASS | PASS |
| 39 | `apps.mpv` | PASS | PASS | PASS | PASS | PASS | PASS |
| 40 | `apps.msi-afterburner` | FAIL | PASS | PASS | FAIL | MISSING | FAIL |
| 41 | `apps.musicbee` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 42 | `apps.neovim` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 43 | `apps.notepad-plus-plus` | PASS | PASS | PASS | WARN | MISSING | WARN |
| 44 | `apps.obs-studio` | PASS | PASS | PASS | PASS | PASS | PASS |
| 45 | `apps.obsidian` | PASS | PASS | PASS | PASS | PASS | PASS |
| 46 | `apps.openrgb` | PASS | PASS | PASS | PASS | PASS | PASS |
| 47 | `apps.paint-net` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 48 | `apps.plex` | PASS | PASS | PASS | PASS | PASS | PASS |
| 49 | `apps.powertoys` | FAIL | PASS | PASS | FAIL | MISSING | FAIL |
| 50 | `apps.premiere-pro` | FAIL | PASS | PASS | PASS | PASS | FAIL |
| 51 | `apps.putty` | PASS | PASS (install-only) | PASS | PASS | PASS | PASS |
| 52 | `apps.pycharm` | PASS | PASS | PASS | PASS | PASS | PASS |
| 53 | `apps.qbittorrent` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 54 | `apps.rainmeter` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 55 | `apps.reaper` | PASS | PASS | PASS | PASS | PASS | PASS |
| 56 | `apps.sharex` | PASS | PASS | PASS | PASS | PASS | PASS |
| 57 | `apps.signal` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 58 | `apps.spotify` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 59 | `apps.sublime-text` | PASS | PASS | PASS | PASS | PASS | PASS |
| 60 | `apps.telegram` | PASS | PASS (install-only) | PASS | PASS | N/A | PASS |
| 61 | `apps.thunderbird` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 62 | `apps.totalcommander` | PASS | PASS | PASS | PASS | PASS | PASS |
| 63 | `apps.vlc` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 64 | `apps.vscodium` | PASS | PASS | PASS | PASS | PASS | PASS |
| 65 | `apps.vscode` | PASS | PASS | PASS | PASS | PASS | PASS |
| 66 | `apps.webstorm` | PASS | PASS | PASS | PASS | PASS | PASS |
| 67 | `apps.windsurf` | PASS | PASS | PASS | PASS | PASS | PASS |
| 68 | `apps.windows-terminal` | PASS | PASS | PASS | PASS | PASS | PASS |
| 69 | `apps.winscp` | PASS | PASS | PASS | PASS | MISSING | WARN |
| 70 | `apps.wireguard` | PASS | FAIL | PASS | PASS | PASS | FAIL |

### Result summary:
- **PASS**: 42 modules
- **WARN**: 21 modules (missing curation or minor path issues)
- **FAIL**: 7 modules (schema errors, symmetry mismatches, or path violations)

---

## Priority Fixes

### P0 -- Must Fix (broken functionality)
1. **`apps.msi-afterburner`**: Full overhaul needed. Hardcoded `C:\Program Files (x86)` paths, wrong `./configs/` source prefix, missing `excludeGlobs` in capture, missing `curation`. This module will fail restore on machines with non-standard Program Files locations.
2. **`apps.powertoys`**: `restore[0].source` uses `./apps/powertoys` instead of `./payload/apps/powertoys`. Restore will fail because source path doesn't match payload layout. Non-standard `exclude` field on restore entry.

### P1 -- Should Fix (correctness)
3. **`apps.ableton-live`**: Capture/restore asymmetry -- capture grabs two subdirectories but restore writes entire directory. Round-trip loses data.
4. **`apps.premiere-pro`**: Add `"winget": []` to matches for schema consistency.
5. **`apps.after-effects`**: Add `"winget": []` to matches for schema consistency.
6. **`apps.evga-precision-x1`**: Add `"winget": []` to matches for schema consistency.

### P2 -- Nice to Fix (consistency)
7. **`apps.wireguard`**: Add restore entry for `wireguard.exe.config` or remove capture entry.
8. **`apps.notepad-plus-plus`**, **`apps.autohotkey`**, **`apps.wireguard`**: Replace `C:\Program Files` with `%ProgramFiles%` in verify paths.
9. **`apps.lightroom-classic`**: Add `sensitive` section for completeness.
10. **23 modules missing `curation`**: Add `curation` section for consistency (see Consistency Issues table).

---

*Report generated by ModuleValidator agent, 2026-02-22*
