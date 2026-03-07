# Tasks: Include Non-Winget Modules in Capture Zip Bundle

## Implementation

- [x] Add pathExists matching to `Get-MatchedConfigModulesForApps` in `engine/bundle.ps1`
- [x] Add `pathExists` entry to `modules/apps/mpv/module.jsonc`
- [x] Add `pathExists` entry to `modules/apps/claude-desktop/module.jsonc`
- [x] Add `pathExists` entry to `modules/apps/claude-code/module.jsonc`
- [x] Extend `Build-ConfigModuleMap` in `engine/config-modules.ps1` for non-winget modules

## Testing

- [x] Add pathExists matching tests to `tests/unit/Bundle.Tests.ps1`
- [x] Add non-winget map tests to `tests/unit/ConfigModuleMap.Tests.ps1`
- [ ] Run full unit test suite
- [ ] Manual verification: capture with `--json` and inspect output
