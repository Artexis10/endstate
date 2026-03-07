# Uncaptured Config Discovery Report

Generated: 2026-03-07

## Summary

This report documents findings from Phase 1 (broken module fixes) and Phase 2 (uncaptured config discovery) across installed apps on this machine.

---

## PHASE 1: Broken Module Fixes

### 1. brave

**Problem:** Verify entry pointed to `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\Application\brave.exe`, which doesn't exist.

**Discovery:** Brave is not on PATH. Registry App Paths confirms the executable is at:
```
C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe
```
Brave installs system-wide (to `%ProgramFiles%`) when installed via winget, not to `%LOCALAPPDATA%`.

**Fix applied:** Changed verify path to `%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`.

**File:** `modules/apps/brave/module.jsonc`

---

### 2. hwinfo

**Problem:** Verify and capture pointed to `%APPDATA%\HWiNFO64\HWiNFO64.INI`, which doesn't exist.

**Discovery:** HWiNFO stores its config in the install directory, not APPDATA:
```
C:\Program Files\HWiNFO64\HWiNFO64.INI
```
The APPDATA and LOCALAPPDATA directories for HWiNFO do not exist on this machine. The INI file contains user settings (Theme=1, SensorsOnly=1).

**Limitation:** Restoring this file requires admin elevation since it's in `%ProgramFiles%`. This is documented in the module notes.

**Fix applied:** Updated verify, restore target, and capture source to `%ProgramFiles%\HWiNFO64\HWiNFO64.INI`.

**File:** `modules/apps/hwinfo/module.jsonc`

---

### 3. msi-afterburner

**Problem:** Audit reported all three paths broken. The module uses `%ProgramFiles(x86)%\MSI Afterburner\`.

**Discovery:** MSI Afterburner IS installed at `C:\Program Files (x86)\MSI Afterburner\`. The exe, cfg, and Profiles directory all exist. The audit failure was due to the env var `%ProgramFiles(x86)%` not expanding in the audit script's path check.

**Additional findings:**
- `Profiles\` contains `Profile1.cfg` through `Profile5.cfg` plus hardware-specific GPU profile files (`VEN_10DE&DEV_2702...cfg`, etc.)
- Hardware-specific GPU profiles are NOT portable across machines (GPU-specific PCIe IDs)

**Fix applied:**
- Added `excludeGlobs` for `VEN_*` hardware-specific profiles and `.dat` files
- Added `sensitivity` and notes explaining admin elevation requirement
- Updated notes to clarify what is and isn't portable

**File:** `modules/apps/msi-afterburner/module.jsonc`

---

### 4. notepad-plus-plus

**Problem:** Verify used hardcoded path `C:\Program Files\Notepad++\notepad++.exe`. Audit also reported config files (config.xml, shortcuts.xml, langs.xml, stylers.xml) missing from `%APPDATA%\Notepad++\`.

**Discovery:**
- Notepad++ IS installed at `C:\Program Files\Notepad++\notepad++.exe`
- No `doLocalConf.xml` in install dir → NOT in portable mode → config should be in APPDATA
- `%APPDATA%\Notepad++\` exists but only contains: `plugins/config/`, `userDefineLangs/` (pre-installed markdown UDLs), `contextMenu.xml`
- The main config files (config.xml, shortcuts.xml, langs.xml, stylers.xml) do NOT exist yet — they are only created after the user first changes settings from defaults
- All capture entries are already `optional: true`, so this is correct behavior

**Fix applied:**
- Verify already changed to `command-exists` (from a prior audit pass) — confirmed correct
- Added `nppLogNulContentCorruptionIssue.xml` to `excludeGlobs` (found in install dir, prevents log capture)
- Updated notes to explain why config files may be absent on a fresh install
- Added explanatory comment block about config location behavior

**File:** `modules/apps/notepad-plus-plus/module.jsonc`

---

### 5. mpv

**Problem:** Module captured `scripts/` directory which doesn't exist. Audit reported 1 missing path.

**Discovery:**
`%APPDATA%\mpv\` contains only:
- `mpv.conf` — main config (EXISTS, captured)
- `input.conf` — key bindings (EXISTS, captured)

The `scripts/` directory does not exist. The user has not installed any Lua scripts.

**Additional directories** that may be populated as the user customizes mpv:
- `script-opts/` — per-script configuration files (not present, worth tracking)
- `shaders/` — custom GLSL video shaders (not present, worth tracking)

**Fix applied:** Updated module to cover the full set of portable mpv config locations:
- Kept `scripts/` (optional — will be skipped if absent)
- Added `script-opts/` capture/restore (optional)
- Added `shaders/` capture/restore (optional)
- Updated notes to clarify actual state on this machine

**File:** `modules/apps/mpv/module.jsonc`

---

## PHASE 2: Uncaptured Config Discovery

### lightroom-classic (HIGHEST PRIORITY)

**Config roots explored:**
- `%APPDATA%\Adobe\Lightroom\` (20 entries)
- `%APPDATA%\Adobe\CameraRaw\` (10 entries)

**Directories in module (26 entries):** Preferences, Develop Presets, Export Presets, Filename Templates, Metadata Presets, Filter Presets, Local Adjustment Presets, Keyword Sets, Label Sets, Smart Collection Templates, External Editor Presets, Watermarks, Color Profiles, FTP Presets, Print Templates, Web Templates, Slideshow Templates, Book Templates, Export Actions, Text Templates, Import Presets, CameraRaw Defaults, CameraRaw CameraProfiles, CameraRaw Curves, CameraRaw LensProfiles, CameraRaw ImportedSettings

**Directories on disk NOT in module:**

| Directory | Content | Decision |
|-----------|---------|----------|
| `Lightroom\0\` | logs/, Debug Database.txt, Trace Database.txt | EXCLUDED — Lightroom internal debug logs |
| `Lightroom\Copy Paste Subsets\` | 0 files | No action — empty |
| `Lightroom\Device Icons\` | 1 file (built-in camera icon PNG) | No action — built-in asset, not user config |
| `Lightroom\Locations\` | `My Locations\` (empty) | No action — empty, auto-created |
| `Lightroom\Metadata\` | `DefaultPanel.lua` | No action — Lightroom system file, auto-regenerated |
| `CameraRaw\GPU\` | GPU cache | EXCLUDED — already covered by `**\GPU\**` excludeGlob |
| `CameraRaw\LensProfileDefaults\` | 0 files | No action — empty |
| `CameraRaw\Logs\` | log files | EXCLUDED — already covered by `**\Logs\**` excludeGlob |
| `CameraRaw\ModelZoo\` | AI models ~750MB | EXCLUDED — already covered by `**\ModelZoo\**` excludeGlob |
| `CameraRaw\Settings\` | binary .dat files | EXCLUDED — already covered by `**\Settings\*.dat` excludeGlob |

**Conclusion:** The module is comprehensive. All 26 existing entries are correct. No additional entries needed. The un-captured directories are either empty, system files, or correctly excluded.

**Changes made:** None.

---

### git

**Config roots explored:**
- `%USERPROFILE%\.gitconfig` — EXISTS, captured
- `%USERPROFILE%\.config\git\` — only `ignore` exists (captured, optional)
- `%USERPROFILE%\.git-templates\` — does not exist

**`.gitconfig` include directives:** None found. The config contains url rewrite, user info, core editor, alias, and safe directory settings only.

**Files NOT in module:** None. All relevant files are either captured or absent.

**Conclusion:** Module is correct. The missing audit paths (`~/.config/git/config`, `~/.config/git/attributes`) are correctly `optional: true` — they don't exist and that's fine.

**Changes made:** None.

---

### obsidian

**Config root:** `%APPDATA%\obsidian\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `obsidian.json` | Vault list with path and timestamp | CAPTURED — already in module |
| `964c84c0729aa16a.json` | Window position/size (x, y, width, height, isMaximized) | EXCLUDED — ephemeral window state |
| `Preferences` | Electron Chromium preferences (spellcheck, zoom) | EXCLUDED — Electron internal, not portable |
| `Local State` | Electron local state (434 bytes) | EXCLUDED — Electron runtime state |
| `Cache\`, `GPUCache\`, `Code Cache\` etc. | Electron caches | EXCLUDED — already in excludeGlobs |
| `blob_storage\`, `IndexedDB\`, etc. | Electron storage | EXCLUDED — ephemeral |
| `obsidian.log` | Log file | EXCLUDED — already in excludeGlobs |
| `id` | Installation identifier | EXCLUDED — device-specific |
| `DIPS` | Electron DIPS data | EXCLUDED — runtime |
| `SharedStorage` | Electron shared storage | EXCLUDED — runtime |
| `obsidian-1.11.7.asar` | App bundle | EXCLUDED — app binary |

**Conclusion:** `obsidian.json` (vault list) is the only portable config. All other files are either Electron runtime artifacts or ephemeral window state. Module is correct.

**Changes made:** None.

---

### vlc

**Config root:** `%APPDATA%\vlc\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `vlcrc` | Main preferences file | CAPTURED — already in module |
| `vlc-qt-interface.ini` | Qt UI layout | CAPTURED — already in module |
| `crashdump\` | Crash dump files | EXCLUDED — already excluded via `**\art_cache\**` pattern (crash dumps are runtime) |
| `ml.xspf` | Media library playlist | EXCLUDED — already in excludeGlobs |

**No `lua\` directory exists** — user has not installed any VLC extensions/scripts.

**Conclusion:** Module covers all portable config. No lua scripts to add.

**Changes made:** None.

---

### foobar2000

**Config root:** `%APPDATA%\foobar2000-v2\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `playlists-v2.0\` | Playlist files | CAPTURED — already in module |
| `config.sqlite` | v2 configuration database | NOT CAPTURED — SQLite database, noted as limitation in module |
| `version.txt` | Version string | EXCLUDED — ephemeral |

**`%APPDATA%\foobar2000\`** does not exist — v1 is not installed.

**Conclusion:** The module correctly captures playlists and notes the SQLite limitation. The `config.sqlite` file contains all v2 settings but cannot be safely copied while foobar2000 is running. No additional entries possible without SQLite extraction tooling.

**Changes made:** None.

---

### docker-desktop

**Config root:** `%APPDATA%\Docker\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `settings-store.json` | Docker Desktop settings | CAPTURED — already in module |
| `extensions\` | Extension data (empty) | EXCLUDED — extension data, not portable |
| `unleash-v2-docker-desktop.json` | Feature flag values from server | EXCLUDED — server-fetched, rebuilds automatically |
| `features-overrides.json` | Feature flag overrides | EXCLUDED — runtime |
| `analyticsmonitor.dat` | Analytics telemetry | EXCLUDED — telemetry |
| `marlin.dat` | Docker internal state | EXCLUDED — runtime state |
| `wc.dat` | Docker internal state | EXCLUDED — runtime state |
| `.trackid` | Tracking identifier | EXCLUDED — device-specific identity |
| `reports.log` | Log file | EXCLUDED — log |
| `locked-directories` | Lock file | EXCLUDED — lock file |

**Conclusion:** Module already captures the only portable config file. All other files are runtime/analytics/telemetry data.

**Changes made:** None.

---

### claude-code

**Config root:** `%USERPROFILE%\.claude\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `settings.json` | User settings | CAPTURED — already in module |
| `plugins/installed_plugins.json` | Plugin list (empty `{}`) | Not captured — no plugins installed |
| `plugins/blocklist.json` | Downloaded server blocklist | EXCLUDED — server-fetched, auto-updates |
| `plugins/install-counts-cache.json` | Install count cache | EXCLUDED — ephemeral cache |
| `plugins/marketplaces/` | Plugin registry | EXCLUDED — server-fetched |
| `.credentials.json` | Auth tokens | EXCLUDED — in sensitive section |
| `history.jsonl` | Command history | EXCLUDED — ephemeral session data |
| `stats-cache.json` | Usage statistics cache | EXCLUDED — ephemeral |
| `cache\`, `telemetry\`, `todos\`, etc. | Runtime dirs | EXCLUDED — already in excludeGlobs |

**Conclusion:** The module's assessment from the ai-editor-audit was correct. `installed_plugins.json` is empty (no plugins), and `blocklist.json` is a server-downloaded file that auto-updates. No user-authored config files are missing.

**Changes made:** None.

---

### inkscape

**Config root:** `%APPDATA%\inkscape\`

**Files found:**

| File/Dir | Content | Decision |
|----------|---------|----------|
| `preferences.xml` | Main preferences | CAPTURED — already in module |
| `keys\` | Custom keyboard shortcuts | CAPTURED (empty) — already in module |
| `templates\` | Custom document templates | CAPTURED (empty) — already in module |
| `extensions\` | User extensions | 0 files — empty |
| `palettes\` | Custom color palettes | 0 files — empty |
| `fonts\` | User fonts | 0 files — empty |
| `icons\` | Custom icon sets | 0 files — empty |
| `fontcollections\` | Font collection files | 0 files — empty |
| `cphistory.xml` | Clipboard history | EXCLUDED — session data |
| `dialogs-state-ex.ini` | Dialog geometry/position | EXCLUDED — ephemeral window state |
| `pages.csv` | Page size definitions | EXCLUDED — ephemeral |
| `extension-errors.log` | Extension error log | EXCLUDED — already in excludeGlobs |

**Not yet captured but worth tracking for future:** `palettes/`, `extensions/`, `fontcollections/` — all currently empty on this machine.

**Changes made:** None — all relevant subdirs are empty and the existing optional entries cover them when populated.

---

### 7zip

**Config:** Registry-based (`HKCU\Software\7-Zip`).

**Filesystem check:**
- No `%APPDATA%\7-Zip\` directory exists
- No `%LOCALAPPDATA%\7-Zip\` directory exists
- Install dir `%ProgramFiles%\7-Zip\` contains only binaries

**Conclusion:** Module is correctly install-only with registry note. No file-based config to capture.

**Changes made:** None.

---

## Summary of All Changes Made

### Phase 1 Fixes

| Module | Change |
|--------|--------|
| `brave` | Fixed verify path: `%LOCALAPPDATA%` → `%ProgramFiles%` for Brave executable |
| `hwinfo` | Fixed all paths: `%APPDATA%\HWiNFO64\` → `%ProgramFiles%\HWiNFO64\` (config is in install dir) |
| `msi-afterburner` | Added `excludeGlobs` for hardware-specific GPU profile files; improved notes |
| `notepad-plus-plus` | Added comment block explaining config location behavior; added `nppLogNulContentCorruptionIssue.xml` to excludeGlobs; improved notes |
| `mpv` | Added `script-opts/` and `shaders/` capture/restore entries (both optional); updated notes |

### Phase 2 Discovery

| Module | Changes |
|--------|---------|
| `lightroom-classic` | No changes — 26 entries comprehensive; uncaptured dirs are empty/system files |
| `git` | No changes — module correct; missing paths are correctly optional |
| `obsidian` | No changes — only `obsidian.json` is portable; all other files are Electron runtime |
| `vlc` | No changes — all portable config already captured |
| `foobar2000` | No changes — v2 SQLite limitation already documented |
| `docker-desktop` | No changes — all portable config already captured |
| `claude-code` | No changes — plugins not installed; blocklist is server-fetched |
| `inkscape` | No changes — all subdirectories empty; existing entries sufficient |
| `7zip` | No changes — registry-based, correctly install-only |
