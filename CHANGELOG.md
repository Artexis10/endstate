# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [1.7.0](https://github.com/Artexis10/endstate/compare/v1.6.0...v1.7.0) (2026-03-31)


### Features

* add delete-glob restore strategy for post-restore cache cleanup ([7b59f06](https://github.com/Artexis10/endstate/commit/7b59f06a12cf160b40bb389cb7c7be4cf915fed8))

## [1.6.0](https://github.com/Artexis10/endstate/compare/v1.5.2...v1.6.0) (2026-03-30)


### Features

* **go-engine:** close 3 PS-removal blocking gaps ([3b02a25](https://github.com/Artexis10/endstate/commit/3b02a259a3f407ad639acd4b0591d2aabe38764e))

## [1.5.2](https://github.com/Artexis10/endstate/compare/v1.5.1...v1.5.2) (2026-03-30)


### Bug Fixes

* **go-engine:** populate FromModule on restore entries so --restore-filter works ([5962b39](https://github.com/Artexis10/endstate/commit/5962b39090ebf5fac228cf8ca4b44d398ecdba8a))

## [1.5.1](https://github.com/Artexis10/endstate/compare/v1.5.0...v1.5.1) (2026-03-29)


### Bug Fixes

* **module:** add missing CameraRaw Settings, Metadata, and Locations to lightroom-classic ([0e6e3ba](https://github.com/Artexis10/endstate/commit/0e6e3baa199278ae16481db539a05f9efea3a928))

## [1.5.0](https://github.com/Artexis10/endstate/compare/v1.4.0...v1.5.0) (2026-03-27)


### Features

* **go-engine:** enrich display names in apply, verify, and plan output ([7f8ae15](https://github.com/Artexis10/endstate/commit/7f8ae151350f9da1018585310c9260110c0060c6))

## [1.4.0](https://github.com/Artexis10/endstate/compare/v1.3.0...v1.4.0) (2026-03-26)

### Features

* Manual app declarations with `verifyPath`, `launch`, `instructions`, `fallback`
* Auto-synthesis of app entries from config module `pathExists` matchers
* Batch winget detection — 35x speedup (~2min → ~3.5s)
* `manualApps` capability flag
* Display name propagation for synthesized manual apps

### Bug Fixes

* Winget capture retry on 0-app results (lock contention)
* Winget export `--disable-interactivity` for Tauri sidecar context
* `os.CreateTemp` fix for winget export temp file

## [1.3.0] - 2026-03-11

### Added
- `--restore-filter` CLI flag for per-module config restore selection during apply and restore commands
- `restore` standalone command in CLI entrypoint with `--restore-filter` support
- `restoreModulesAvailable` and `restoreFilter` fields in apply JSON envelope
- `--restore-filter` in capabilities output for apply and restore commands

### Changed

### Fixed
- RestoreFilter had no effect: CLI entrypoint's Invoke-ApplyCore was missing config module expansion, so `_fromModule` was never set on restore entries
- Inline restore entries (from zip bundles) had no module ID: added source path inference (`configs/<module-id>/` pattern) as fallback for module ID derivation

## [1.2.0] - 2026-03-07

### Added
- `pathExists` matcher for config modules — enables matching apps installed outside winget (Adobe CC apps, built-in tools)
- OpenSpec spec: path-exists-matcher
- Lightroom Classic now matched during capture via pathExists
- Unit tests for pathExists matcher (10 tests)

### Changed
- Updated non-winget modules with pathExists fallback paths (lightroom-classic, after-effects, premiere-pro, ableton-live, capture-one, davinci-resolve, dxo-photolab, evga-precision-x1)
- `Test-ConfigModuleSchema` now validates pathExists arrays
- `Get-ConfigModulesForInstalledApps` checks pathExists paths via `Expand-ConfigPath` + `Test-Path`

### Fixed
- Lightroom Classic config module not matching during capture (no winget ID, exe not on PATH)

## [1.1.0] - 2026-03-07

### Added
- 6 new config modules: warp-terminal, powershell-profile, ssh-config, github-cli, dbeaver, digikam
- Config discovery audit tooling (scripts/audit/)
- Uncaptured config discovery for all installed apps
- mpv: script-opts/ and shaders/ capture/restore
- Windsurf: full memories/ directory capture (supports user-named rules files)
- Windsurf: MCP config and custom workflows capture
- Cursor: AI rules directory and MCP config capture
- VSCodium: tasks.json and extensions.json manifest capture
- VS Code: tasks.json and extensions.json manifest capture
- Claude Desktop: config.json and extensions-installations.json capture
- Docker Desktop: settings-store.json capture (correct filename)
- Notepad++: contextMenu.xml capture/restore
- MSI Afterburner: hardware-specific GPU profile exclusions (VEN_*)

### Changed
- Module catalog: 70 → 76 modules
- PowerToys: added Microsoft Store ID (XP89DCGQ3K6VLD) for detection
- Notepad++: verify changed from hardcoded path to command-exists
- Docker Desktop: verify changed to command-exists for docker
- foobar2000: updated to v2 paths (foobar2000-v2/, config.sqlite noted as limitation)
- HWiNFO: fixed config path to install directory (admin required, documented)
- Brave: fixed verify path to %ProgramFiles% location

### Fixed
- Lightroom Classic seed.ps1: wrong preferences filename (was Lightroom 6, corrected to CC 7)
- Docker Desktop: module referenced settings.json but actual file is settings-store.json
- foobar2000: all capture paths pointed to dead v1 directories
- HWiNFO: config path pointed to non-existent %APPDATA% location
- Windsurf: hardcoded global_rules.md filename instead of directory capture

## [1.0.0] - 2026-03-06

### Added
- Declarative machine provisioning via winget
- Capture, apply, verify, restore, revert, report commands
- JSON envelope contract (schema 1.0) for GUI integration
- NDJSON event streaming for real-time progress
- Config module system with 35+ validated applications
- Profile-based manifest management
- Export/restore configuration portability
- Backup-before-overwrite safety guarantee
- Parallel installation support

### Changed

### Fixed
## [0.1.0] - 2026-03-05

### Added
- Initial release with semver versioning system

### Changed

### Fixed
