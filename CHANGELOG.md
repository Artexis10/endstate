# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [2.9.0](https://github.com/Artexis10/endstate/compare/v2.8.0...v2.9.0) (2026-05-31)


### Features

* **engine:** converge to exact set via apply --prune (Phase 5) ([#65](https://github.com/Artexis10/endstate/issues/65)) ([15a809d](https://github.com/Artexis10/endstate/commit/15a809d1ba7e3897513473c9cf2e9197274e6cc8))
* **engine:** Windows version capture + pinning (Phase 6) ([#68](https://github.com/Artexis10/endstate/issues/68)) ([189b285](https://github.com/Artexis10/endstate/commit/189b2855b2debe45e25fa318526e8e6dca002d98))

## [2.8.0](https://github.com/Artexis10/endstate/compare/v2.7.0...v2.8.0) (2026-05-31)


### Features

* **engine:** best-effort winget rollback (Phase 4) ([#63](https://github.com/Artexis10/endstate/issues/63)) ([d376b01](https://github.com/Artexis10/endstate/commit/d376b014dc327fd1a817d3882ee62429bbd4b699))
* **engine:** engine-owned provisioning generation for both backends ([#58](https://github.com/Artexis10/endstate/issues/58)) ([3d2872d](https://github.com/Artexis10/endstate/commit/3d2872ddad01baba10fb7ba7ad04d38069df2f11))
* **engine:** native Unix rollback via nix profile rollback (Phase 3) ([#60](https://github.com/Artexis10/endstate/issues/60)) ([e6cc5cb](https://github.com/Artexis10/endstate/commit/e6cc5cb0852ebe8720c4ce55cf99aaa02b04367d))


### Bug Fixes

* **capture:** enforce module secrets.files exclusion at capture time ([#56](https://github.com/Artexis10/endstate/issues/56)) ([b377fb7](https://github.com/Artexis10/endstate/commit/b377fb75b7a3dea703910f1fc3d567cf883a924e))

## [2.7.0](https://github.com/Artexis10/endstate/compare/v2.6.0...v2.7.0) (2026-05-29)


### Features

* **modules:** expand config-module catalog 76 → 315 (high-value + mainstream Windows apps) ([#51](https://github.com/Artexis10/endstate/issues/51)) ([134b646](https://github.com/Artexis10/endstate/commit/134b646271a91e65785a3006281526d12afbd7eb))

## [2.6.0](https://github.com/Artexis10/endstate/compare/v2.5.0...v2.6.0) (2026-05-29)


### Features

* **engine:** Nix package realizer backend for Linux/macOS ([#50](https://github.com/Artexis10/endstate/issues/50)) ([9a06b8e](https://github.com/Artexis10/endstate/commit/9a06b8ea0a31fa0dbc958ccc7c8a5e9ea6f91736))
* **engine:** platform-aware backend selection foundation ([#44](https://github.com/Artexis10/endstate/issues/44)) ([f84dd6c](https://github.com/Artexis10/endstate/commit/f84dd6cbc78178b3846faa6df779b4b48889f579))

## [2.5.0](https://github.com/Artexis10/endstate/compare/v2.4.0...v2.5.0) (2026-05-29)


### Features

* **backup:** cross-process refresh-token rotation lock (F5) ([#45](https://github.com/Artexis10/endstate/issues/45)) ([12bf7cf](https://github.com/Artexis10/endstate/commit/12bf7cf6e02ffbf5af4a50bff8efdc04517a8cbb))


### Bug Fixes

* **backup:** fail closed on Hydrate error inside refresh lock ([#47](https://github.com/Artexis10/endstate/issues/47)) ([0fcd3c5](https://github.com/Artexis10/endstate/commit/0fcd3c500e1db7f7c2dd0e2cb3dbad362edde4b9))

## [2.4.0](https://github.com/Artexis10/endstate/compare/v2.3.1...v2.4.0) (2026-05-26)


### Features

* **backup:** add `backup browser-session` command + contract §4-§9 sync ([#42](https://github.com/Artexis10/endstate/issues/42)) ([63349ac](https://github.com/Artexis10/endstate/commit/63349aceffad6131f1eff7ae70989ff0d2b02a39))


### Bug Fixes

* **ci:** auth release-please via GitHub App, drop dispatch shim ([#41](https://github.com/Artexis10/endstate/issues/41)) ([2ffd2c7](https://github.com/Artexis10/endstate/commit/2ffd2c7bff1972e1d307b32d1ee2a0fcd279045b))

## [2.3.1](https://github.com/Artexis10/endstate/compare/v2.3.0...v2.3.1) (2026-05-26)


### Bug Fixes

* **ci:** dispatch Release workflow from release-please job ([#39](https://github.com/Artexis10/endstate/issues/39)) ([4bdff73](https://github.com/Artexis10/endstate/commit/4bdff73ec6ceffa0b5f718726f6129795039e4cd))

## [2.3.0](https://github.com/Artexis10/endstate/compare/v2.2.1...v2.3.0) (2026-05-26)


### Features

* **backup:** emit backup-chunk events with per-attempt retry visibility ([#37](https://github.com/Artexis10/endstate/issues/37)) ([610fe4f](https://github.com/Artexis10/endstate/commit/610fe4fdbc27eff27051fc44f1c23fdfa8fca34e))

## [2.2.1](https://github.com/Artexis10/endstate/compare/v2.2.0...v2.2.1) (2026-05-25)


### Bug Fixes

* **modules:** exclude PowerToys self-update installer from captures ([#35](https://github.com/Artexis10/endstate/issues/35)) ([8c2b533](https://github.com/Artexis10/endstate/commit/8c2b533e4f1911ceac30c598e3e6da9374b97e05))

## [2.2.0](https://github.com/Artexis10/endstate/compare/v2.1.0...v2.2.0) (2026-05-24)


### Features

* **backup:** add `backup claim` subcommand for anonymous-buyer credential attachment ([#32](https://github.com/Artexis10/endstate/issues/32)) ([c48f059](https://github.com/Artexis10/endstate/commit/c48f059da217bfe37bee56abe04923f4a05bbbd8))

## [2.1.0](https://github.com/Artexis10/endstate/compare/v2.0.1...v2.1.0) (2026-05-22)


### Features

* **backup:** add `backup subscribe` checkout command ([#30](https://github.com/Artexis10/endstate/issues/30)) ([3758527](https://github.com/Artexis10/endstate/commit/3758527feefcf17afca13c49af68320944a18ac0))

## [2.0.1](https://github.com/Artexis10/endstate/compare/v2.0.0...v2.0.1) (2026-05-11)


### Bug Fixes

* **backup:** persist access token + expiry to skip per-call refresh ([#28](https://github.com/Artexis10/endstate/issues/28)) ([98bf648](https://github.com/Artexis10/endstate/commit/98bf6489b88c18f9d99a6dadcbb0759a6bae6dd2))

## [2.0.0](https://github.com/Artexis10/endstate/compare/v1.9.0...v2.0.0) (2026-05-10)


### ⚠ BREAKING CHANGES

* hosted-backup contract bumps to v2.0. Recovery flow finalized to bearer-header transport. Old engines cannot recover passphrases against new substrate; old substrate cannot respond to new engine recover-finalize calls. Coordinated rollout required.

### Features

* align cross-repo recovery flow and self-host plumbing for v2.0.0 ([#26](https://github.com/Artexis10/endstate/issues/26)) ([d10e6c9](https://github.com/Artexis10/endstate/commit/d10e6c9d1ab13bf8ef8a3690f0e5afed1401912d))

## [1.9.0](https://github.com/Artexis10/endstate/compare/v1.8.0...v1.9.0) (2026-05-08)


### Features

* **backup:** implement Hosted Backup cryptographic module ([#23](https://github.com/Artexis10/endstate/issues/23)) ([d8a01ca](https://github.com/Artexis10/endstate/commit/d8a01ca5b32dc792e70eb2938b3e98114a122605))
* **backup:** scaffold Hosted Backup auth client + version check ([#19](https://github.com/Artexis10/endstate/issues/19)) ([d65f0ff](https://github.com/Artexis10/endstate/commit/d65f0ff582d60f235b97ef8b620146f00beb36f7))
* **backup:** wire end-to-end Hosted Backup orchestration ([#24](https://github.com/Artexis10/endstate/issues/24)) ([0311939](https://github.com/Artexis10/endstate/commit/0311939ca679cec2d5d1a9e234f4fbc3f45ffda1))
* **backup:** wire Hosted Backup storage client + remaining commands ([#22](https://github.com/Artexis10/endstate/issues/22)) ([acae665](https://github.com/Artexis10/endstate/commit/acae6650f6a885ed27878cfe0aaebc9d3658ad91))


### Bug Fixes

* **backup:** surface keychain-access failures via StatusResult.keychainError ([#25](https://github.com/Artexis10/endstate/issues/25)) ([a01ac17](https://github.com/Artexis10/endstate/commit/a01ac170dba0c7018a5881da15a59b93ecdfccc4))

## [1.8.0](https://github.com/Artexis10/endstate/compare/v1.7.7...v1.8.0) (2026-05-01)


### Features

* attach endstate.exe and sha256 checksum to every GitHub release ([89234c1](https://github.com/Artexis10/endstate/commit/89234c1d8fbbae7bc3d551c943713d294e360555))


### Bug Fixes

* add missing delta specs to three existing OpenSpec changes ([cc74691](https://github.com/Artexis10/endstate/commit/cc746912de4e95c03e5180cc43dd91ceec1f8b07))

## [1.7.7](https://github.com/Artexis10/endstate/compare/v1.7.6...v1.7.7) (2026-04-08)


### Bug Fixes

* **version:** eliminate VERSION file, read from release-please manifest ([c052b02](https://github.com/Artexis10/endstate/commit/c052b0243c342c757f337dc2a8a4f6a7363d4172))

## [1.7.6](https://github.com/Artexis10/endstate/compare/v1.7.5...v1.7.6) (2026-04-08)


### Bug Fixes

* **release:** sync VERSION to 1.7.5 and remove duplicate extra-files entry ([326fa64](https://github.com/Artexis10/endstate/commit/326fa64ad9c6df82fcdf1e5fcc626beca411fb0b))

## [1.7.5](https://github.com/Artexis10/endstate/compare/v1.7.4...v1.7.5) (2026-04-01)


### Bug Fixes

* **release:** add VERSION to release-please extra-files ([d259ae2](https://github.com/Artexis10/endstate/commit/d259ae248692868e1a486b688ce170984cc84df1))

## [1.7.4](https://github.com/Artexis10/endstate/compare/v1.7.3...v1.7.4) (2026-04-01)


### Bug Fixes

* **modules:** matcher skips modules with only registryKeys capture ([5a122ca](https://github.com/Artexis10/endstate/commit/5a122caae0ac2d58f5425a2b46674cbe29b5c148))

## [1.7.3](https://github.com/Artexis10/endstate/compare/v1.7.2...v1.7.3) (2026-04-01)


### Bug Fixes

* **modules:** remove bogus winget ID from fastrawviewer, add pathExists ([4e67dee](https://github.com/Artexis10/endstate/commit/4e67deecf5c835ac4d9e16ab3910adf54f5b6e3a))
* **restore:** revert deletes freshly created registry keys ([4f453b6](https://github.com/Artexis10/endstate/commit/4f453b6e5d34d392c2faf62c5f041a4aabddda21))

## [1.7.2](https://github.com/Artexis10/endstate/compare/v1.7.1...v1.7.2) (2026-03-31)


### Bug Fixes

* remove unnecessary delete-glob entry from lightroom-classic module ([508493d](https://github.com/Artexis10/endstate/commit/508493d65da8e42a4a22d7d8405d995cf7ea2224))

## [1.7.1](https://github.com/Artexis10/endstate/compare/v1.7.0...v1.7.1) (2026-03-31)


### Bug Fixes

* anchor /state/ gitignore rule so go-engine/internal/state/ is tracked ([b4b5ade](https://github.com/Artexis10/endstate/commit/b4b5ade49ba188c999fd2d5b6864ec6c3da48dd9))

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
