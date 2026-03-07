# Proposal: Include Non-Winget Modules in Capture Zip Bundle

## Problem

The zip bundle capture pipeline misses modules for apps without winget IDs. Apps like Lightroom Classic, mpv, Claude Desktop, and Claude Code are detected by module matching during discovery (`Get-ConfigModulesForInstalledApps` shows them as "Config Modules Available"), but `Get-MatchedConfigModulesForApps` in the bundler only checks `matches.winget` against app `refs.windows` values. Modules matched via `pathExists` or with non-matching winget IDs never enter the bundle.

Additionally, `Build-ConfigModuleMap` only creates entries for modules with winget refs, so non-winget modules are invisible to the GUI's per-app settings indicator.

## Solution

1. **Bundler pathExists matching**: Add pathExists check to `Get-MatchedConfigModulesForApps` after the winget check. If a module wasn't matched by winget, check `module.matches.pathExists` using `Expand-ConfigPath` + `Test-Path` (same logic as `Get-ConfigModulesForInstalledApps`).

2. **Module pathExists entries**: Add `pathExists` arrays to mpv, claude-desktop, and claude-code modules pointing to config files whose existence proves the app is installed.

3. **Config module map fallback**: Extend `Build-ConfigModuleMap` to use the module's ID as the map key when the module has no winget refs, ensuring non-winget modules appear in the JSON envelope.

## Scope

- `engine/bundle.ps1` -- `Get-MatchedConfigModulesForApps`
- `engine/config-modules.ps1` -- `Build-ConfigModuleMap`
- `modules/apps/mpv/module.jsonc`
- `modules/apps/claude-desktop/module.jsonc`
- `modules/apps/claude-code/module.jsonc`
- `tests/unit/Bundle.Tests.ps1`
- `tests/unit/ConfigModuleMap.Tests.ps1`
