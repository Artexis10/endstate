# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


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
