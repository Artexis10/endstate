# Claude Code Prompt: Module Audit & Enhancement

## Context

You are working in the Endstate repository (`C:\Users\hugoa\Desktop\projects\endstate`). Endstate is a declarative Windows machine provisioning tool. Config modules live in `modules/apps/<app-id>/module.jsonc` and define how to verify, capture, and restore application configurations.

Read `docs/ai/AI_CONTRACT.md` and `docs/ai/PROJECT_SHADOW.md` before starting.

## Objective

Audit every existing config module against the **actual filesystem of this machine**, cross-reference against installed apps, and enhance modules where real config paths exist but aren't captured. This machine is the author's primary dev/creative workstation — it's the ground truth for what paths actually look like.

## Phase 1: Inventory

### 1a. Installed Apps

Run `winget list --disable-interactivity` and save the output to `scripts/audit/winget-inventory.txt`.

### 1b. Module Path Audit

For every module in `modules/apps/*/module.jsonc`:

1. Parse the module (strip JSONC comments before parsing)
2. Expand environment variables (`%APPDATA%`, `%LOCALAPPDATA%`, `%USERPROFILE%`/`~`, `%PROGRAMDATA%`, `%PROGRAMFILES%`) to actual paths
3. Check whether each path in `verify`, `capture.files[].source`, and `restore[].target` **actually exists** on this machine
4. Record which paths exist and which don't
5. For paths that exist, note if they're files or directories, and approximate size

Save a structured report to `scripts/audit/module-audit-report.json` with this shape:

```json
{
  "generatedAt": "ISO timestamp",
  "modules": {
    "vscode": {
      "installed": true,
      "wingetMatch": "Microsoft.VisualStudioCode",
      "verifyPaths": { "%APPDATA%\\Code\\User\\settings.json": { "exists": true, "type": "file", "sizeBytes": 1234 } },
      "capturePaths": { ... },
      "missingPaths": [...],
      "suggestions": ["Consider adding keybindings.json capture"]
    }
  }
}
```

Also produce a human-readable summary at `scripts/audit/module-audit-summary.md`.

## Phase 2: Discovery — Find Uncaptured Config

For each module where the app IS installed on this machine, explore the actual config directories to find files/folders the module doesn't currently capture but should. Specifically:

1. **For each installed app's module**, look at what actually exists under the app's config root (e.g., `%APPDATA%\Code\User\` for VS Code, `%LOCALAPPDATA%\Microsoft\PowerToys\` for PowerToys)
2. List files/directories that exist but are NOT covered by any `capture.files[].source` entry
3. Filter out obvious excludes (Cache, Logs, Temp, GPUCache, Crashpad, *.log, *.lock, crash dumps, telemetry)
4. Flag potentially interesting uncaptured config (settings files, presets, templates, profiles, themes)

Save discovery results to `scripts/audit/uncaptured-config.json`.

## Phase 3: Lightroom Classic Deep Dive

Lightroom Classic is a priority. Do a thorough audit:

1. Check if Lightroom Classic is installed (look for `lightroom.exe`, check Adobe directories)
2. Explore `%APPDATA%\Adobe\Lightroom\` — list ALL subdirectories and files, noting sizes
3. Explore `%APPDATA%\Adobe\CameraRaw\` — same
4. Cross-reference against the existing module's `capture.files` entries
5. Check the verify path — the current module references `Lightroom Classic CC 7 Preferences.agprefs`. Find the ACTUAL preferences filename on this machine (Adobe has changed naming across versions)
6. Report any paths the module references that don't exist, and any real paths that should be added

## Phase 4: Module Enhancement

Based on the audit, enhance modules that need it. For each module you modify:

1. Fix any incorrect paths (e.g., wrong preference filenames, outdated directory names)
2. Add capture/restore entries for real config that exists but isn't covered
3. Keep all entries `"optional": true` for capture and restore (graceful on machines where paths don't exist)
4. Preserve existing module structure and style
5. Don't add paths for cache, logs, temp, crash dumps, telemetry, or credential/token files
6. Respect the `sensitive` section — never add credential/token paths to capture or restore
7. Update `notes` field if adding significant new coverage

**Priority modules to enhance** (if installed and config exists):
- `lightroom-classic` — Hugo's priority, get this right
- `obsidian` — check if vault-level `.obsidian/` settings should be mentioned in notes
- `vscode` — check for profiles, tasks.json, launch.json
- `git` — check for `.gitconfig` includes, hooks
- `windows-terminal` — check settings.json location
- Any other module where you find significant uncaptured config

## Phase 5: Missing Module Candidates

Check winget inventory for installed apps that have NO module yet. For each, report:
- App name and winget ID
- Whether config directories exist in standard locations (%APPDATA%, %LOCALAPPDATA%, %USERPROFILE%)
- Whether a module would be worthwhile (apps with meaningful config vs apps with no portable config)

Don't create new modules in this pass — just report candidates to `scripts/audit/missing-module-candidates.md`.

## Constraints

- Follow `docs/ai/AI_CONTRACT.md` — smallest changes, no unrelated refactors
- Follow `docs/ai/PROJECT_SHADOW.md` — respect all invariants
- Module files are in `modules/apps/<id>/module.jsonc` — JSONC format with comments
- Use existing module style/structure as template (see vscode, git, powertoys modules)
- Never capture: credentials, tokens, browser profiles, license files, databases, caches
- All capture sources and restore targets use environment variables, not hardcoded paths
- Keep `excludeGlobs` patterns consistent with existing modules
- Run `.\scripts\test-unit.ps1` after modifications to verify nothing breaks (only if you modified engine code; module JSONC changes don't need test runs)

## Output

When complete, provide:
1. Summary of findings (which modules are live, which have gaps)
2. List of modules enhanced with what changed
3. Lightroom Classic specific findings
4. Missing module candidates worth creating
5. Any landmines discovered (incorrect paths, broken assumptions in existing modules)
