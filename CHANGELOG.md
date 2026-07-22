# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [2.27.5](https://github.com/Artexis10/endstate/compare/v2.27.4...v2.27.5) (2026-07-22)


### Bug Fixes

* **capture:** human-readable config folder names in bundles ([#188](https://github.com/Artexis10/endstate/issues/188)) ([4315829](https://github.com/Artexis10/endstate/commit/43158290553db2e7257c92cee89cbaaf8946ffbe))

## [2.27.4](https://github.com/Artexis10/endstate/compare/v2.27.3...v2.27.4) (2026-07-21)


### Bug Fixes

* **capture:** keep one winner on config target collisions instead of shipping both ([#187](https://github.com/Artexis10/endstate/issues/187)) ([56c19ae](https://github.com/Artexis10/endstate/commit/56c19ae66dbce6821b6de675245dcc0edd6ed373))
* **capture:** retry transient winget community enumeration before degrading ([#183](https://github.com/Artexis10/endstate/issues/183)) ([f0bc2c8](https://github.com/Artexis10/endstate/commit/f0bc2c80397061e6b101302a0eafe3a65640abc9)), closes [#176](https://github.com/Artexis10/endstate/issues/176)
* **capture:** validate captured config payloads and warn on unrestorable sets ([#184](https://github.com/Artexis10/endstate/issues/184)) ([461a0f4](https://github.com/Artexis10/endstate/commit/461a0f4c8bff1b17c27c2ce3ad591811ad987fc5))
* **catalog:** partition the three grandfathered restore-target overlaps + add Sigma File Manager module ([#186](https://github.com/Artexis10/endstate/issues/186)) ([3f1bbd8](https://github.com/Artexis10/endstate/commit/3f1bbd80f9fa59aa4201cea679f9872ebbac3427))

## [2.27.3](https://github.com/Artexis10/endstate/compare/v2.27.2...v2.27.3) (2026-07-21)


### Bug Fixes

* **capture:** stop false duplicate warning for one Store app across winget sources ([#177](https://github.com/Artexis10/endstate/issues/177)) ([363eb8c](https://github.com/Artexis10/endstate/commit/363eb8cc8c997274996bb1910306fd33b25b8cd6))
* **catalog:** partition overlapping restore ownership (wsl, shell dotfiles) ([#180](https://github.com/Artexis10/endstate/issues/180)) ([a76054e](https://github.com/Artexis10/endstate/commit/a76054eee5d6cd9fc21285ed26caca19ba12d38b))

## [2.27.2](https://github.com/Artexis10/endstate/compare/v2.27.1...v2.27.2) (2026-07-21)


### Bug Fixes

* **catalog:** declare Lightroom Classic preferences as sensitive ([#172](https://github.com/Artexis10/endstate/issues/172)) ([8d664ec](https://github.com/Artexis10/endstate/commit/8d664ec794177078b1a53d6646bf75e8ad250b65))

## [2.27.1](https://github.com/Artexis10/endstate/compare/v2.27.0...v2.27.1) (2026-07-21)


### Bug Fixes

* classify user-cancelled installs distinctly from generic failures ([#169](https://github.com/Artexis10/endstate/issues/169)) ([fd95f6e](https://github.com/Artexis10/endstate/commit/fd95f6e97fec5f4e586ec838c4cdc1411cd5cab2))
* recover Microsoft Store display names during capture ([#170](https://github.com/Artexis10/endstate/issues/170)) ([e773521](https://github.com/Artexis10/endstate/commit/e773521ff53b9b84244349c2f4b996ff84ae7a06))

## [2.27.0](https://github.com/Artexis10/endstate/compare/v2.26.0...v2.27.0) (2026-07-21)


### Features

* **capture:** add progress and Store source lifecycle ([#154](https://github.com/Artexis10/endstate/issues/154)) ([c273f66](https://github.com/Artexis10/endstate/commit/c273f661b705d6d5f0171025591a8bb92638b8d7))

## [2.26.0](https://github.com/Artexis10/endstate/compare/v2.25.1...v2.26.0) (2026-07-21)


### Features

* selective app+config transfer — capture --only, share mode, and the fixes underneath ([#160](https://github.com/Artexis10/endstate/issues/160)) ([50952c5](https://github.com/Artexis10/endstate/commit/50952c542fa0bf6e4b07656cb4020bfe8058b4ce))

## [2.25.1](https://github.com/Artexis10/endstate/compare/v2.25.0...v2.25.1) (2026-07-21)


### Bug Fixes

* **restore:** tolerate vanishing siblings during target resolution ([#165](https://github.com/Artexis10/endstate/issues/165)) ([1cee14d](https://github.com/Artexis10/endstate/commit/1cee14d017d71a25b7d4356de396e856f6188f88))

## [2.25.0](https://github.com/Artexis10/endstate/compare/v2.24.2...v2.25.0) (2026-07-20)


### Features

* **import:** UniGetUI .ubundle importer — manifest-import capability ([#147](https://github.com/Artexis10/endstate/issues/147)) ([4f15cd4](https://github.com/Artexis10/endstate/commit/4f15cd4980c5b330e61d98b6dae3de0e4e3ec3c5))


### Bug Fixes

* **apply:** scope restoreModulesAvailable to profile contents ([#161](https://github.com/Artexis10/endstate/issues/161)) ([a650f8f](https://github.com/Artexis10/endstate/commit/a650f8f22ffef9ea6207ad07a96b31b030394000))

## [2.24.2](https://github.com/Artexis10/endstate/compare/v2.24.1...v2.24.2) (2026-07-18)


### Bug Fixes

* **capture:** restore package event contract ([#158](https://github.com/Artexis10/endstate/issues/158)) ([32caafb](https://github.com/Artexis10/endstate/commit/32caafb8c1cc8112993428055bedd939b2ac90c6))

## [2.24.1](https://github.com/Artexis10/endstate/compare/v2.24.0...v2.24.1) (2026-07-18)


### Bug Fixes

* **bundle:** rewrite legacy directory-root capture sources ([cee36df](https://github.com/Artexis10/endstate/commit/cee36dfc42b157f76a9a0d769d35e5ab1c1e01e0))

## [2.24.0](https://github.com/Artexis10/endstate/compare/v2.23.0...v2.24.0) (2026-07-17)


### Features

* **config:** add first-class configuration generations ([#149](https://github.com/Artexis10/endstate/issues/149)) ([5977987](https://github.com/Artexis10/endstate/commit/59779875eeb75cbe84195ea80695ff554d7c9df7))
* **engine:** warn on cross-driver package ownership ([#151](https://github.com/Artexis10/endstate/issues/151)) ([d30a59f](https://github.com/Artexis10/endstate/commit/d30a59fe889d62d3e7832f70b580de7f5623e031))

## [2.23.0](https://github.com/Artexis10/endstate/compare/v2.22.0...v2.23.0) (2026-07-16)


### Features

* add multi-driver Chocolatey support ([#148](https://github.com/Artexis10/endstate/issues/148)) ([6ad067f](https://github.com/Artexis10/endstate/commit/6ad067fdfe143f27eb2f5287ee5dd3e534f52e43))

## [2.22.0](https://github.com/Artexis10/endstate/compare/v2.21.0...v2.22.0) (2026-07-10)


### Features

* **apply:** --only flag for app-subset installs ([#140](https://github.com/Artexis10/endstate/issues/140)) ([4f04400](https://github.com/Artexis10/endstate/commit/4f04400c35ef3053787886a602cfead5117270d6))
* **capabilities:** advertise hostedBackup.ifChanged capability gate ([#138](https://github.com/Artexis10/endstate/issues/138)) ([ffea93d](https://github.com/Artexis10/endstate/commit/ffea93dbf343f33e51aa5d7a6b730c03beaf1cb3))
* **capture:** --pin flag writes installed versions into the manifest ([#145](https://github.com/Artexis10/endstate/issues/145)) ([947b733](https://github.com/Artexis10/endstate/commit/947b7337239858ebd02b558efed82ffab3bc2d49))
* **manifests:** six curated starter-pack profiles + README Setups section ([#137](https://github.com/Artexis10/endstate/issues/137)) ([fb990a3](https://github.com/Artexis10/endstate/commit/fb990a3c58d087d92848ddb667aa2e8f66bcc280))
* **rebuild:** one-command fresh-machine flow from a capture bundle or manifest ([#146](https://github.com/Artexis10/endstate/issues/146)) ([9769869](https://github.com/Artexis10/endstate/commit/97698696629e1422be1656d619637d31417725c9))
* **schedule:** scheduled drift check — endstate schedule enable|disable|status|run ([#139](https://github.com/Artexis10/endstate/issues/139)) ([e86505b](https://github.com/Artexis10/endstate/commit/e86505b576f8bec29baa9eafd6d68b94121eff9c))


### Bug Fixes

* **capabilities:** advertise brew alongside nix in darwin drivers list ([#144](https://github.com/Artexis10/endstate/issues/144)) ([18c1330](https://github.com/Artexis10/endstate/commit/18c13302ed8229b6b10c12e58a891c9fcf4098d1))

## [2.21.0](https://github.com/Artexis10/endstate/compare/v2.20.0...v2.21.0) (2026-06-27)


### Features

* **backup:** macOS Keychain + Linux Secret Service keychain backends ([#129](https://github.com/Artexis10/endstate/issues/129)) ([5ea8df5](https://github.com/Artexis10/endstate/commit/5ea8df541d5878dd8a75713d011ca56779c163e1))
* **bootstrap:** Unix self-install + multi-platform release artifacts ([#130](https://github.com/Artexis10/endstate/issues/130)) ([f22cd32](https://github.com/Artexis10/endstate/commit/f22cd32ce8afc807b152bf70fb8c2b4da21bb213))
* **catalog:** add 42 Windows config modules across 11 categories ([#133](https://github.com/Artexis10/endstate/issues/133)) ([1c971ca](https://github.com/Artexis10/endstate/commit/1c971caa1db99d822ea0f119fe37db4fd0527419))
* **home-manager:** data-driven cross-platform config catalog + 11 programs ([#135](https://github.com/Artexis10/endstate/issues/135)) ([45b5a5f](https://github.com/Artexis10/endstate/commit/45b5a5f11af03250b08ade08f9f5a5a306a51202))
* **windows-settings:** value-level Windows OS-settings tier (8 groups) ([#134](https://github.com/Artexis10/endstate/issues/134)) ([d073cc7](https://github.com/Artexis10/endstate/commit/d073cc73588323c830c2931d3f172053befd09b7))


### Bug Fixes

* **ci:** bump Go 1.22 to 1.26 (fixes macOS LC_UUID dyld crash) ([#136](https://github.com/Artexis10/endstate/issues/136)) ([851f996](https://github.com/Artexis10/endstate/commit/851f996861dfb2e4ccc1d412220dde226af78bde))

## [2.20.0](https://github.com/Artexis10/endstate/compare/v2.19.0...v2.20.0) (2026-06-06)


### Features

* **bootstrap:** engine-installed backend bootstrap — Nix/Homebrew zero-prerequisites keystone ([#125](https://github.com/Artexis10/endstate/issues/125)) ([59f5dba](https://github.com/Artexis10/endstate/commit/59f5dba853c0dabcf3ef98386cc5b5225d3dcdbe))

## [2.19.0](https://github.com/Artexis10/endstate/compare/v2.18.0...v2.19.0) (2026-06-05)


### Features

* **backup:** mutable backup labels (rename) + device-label create default ([#124](https://github.com/Artexis10/endstate/issues/124)) ([f1fbd53](https://github.com/Artexis10/endstate/commit/f1fbd5310793db76db49be01cdee8419187e205a))
* **engine:** best-effort brew rollback composed with the native rollback ([#121](https://github.com/Artexis10/endstate/issues/121)) ([acbf025](https://github.com/Artexis10/endstate/commit/acbf0259c39bfb00c4a2c4f897ca6e22ab019a3b))

## [2.18.0](https://github.com/Artexis10/endstate/compare/v2.17.0...v2.18.0) (2026-06-04)


### Features

* **backup:** push --name creates a distinct backup when no --backup-id ([#118](https://github.com/Artexis10/endstate/issues/118)) ([f65a4e1](https://github.com/Artexis10/endstate/commit/f65a4e1ff4655e4ac9d5ed45cef82d64aca75750))
* **engine:** home-manager env-exposed secrets (*_FILE path-reference) ([#115](https://github.com/Artexis10/endstate/issues/115)) ([e587636](https://github.com/Artexis10/endstate/commit/e5876369c86f0d9a77d6ae9eded1bccd864d90a2))
* **engine:** two-lane darwin apply wiring for the brew driver ([#117](https://github.com/Artexis10/endstate/issues/117)) ([f6a884e](https://github.com/Artexis10/endstate/commit/f6a884e5d00df0a20e80cf06b3088b81bc743a54))

## [2.17.0](https://github.com/Artexis10/endstate/compare/v2.16.0...v2.17.0) (2026-06-04)


### Features

* **capture:** defense-in-depth bloat guard — baseline excludes + oversized-installer skip ([#110](https://github.com/Artexis10/endstate/issues/110)) ([695f768](https://github.com/Artexis10/endstate/commit/695f768f1e142f6d2891d0e8d8c0f00ea17c21f3))
* **engine:** brew driver package (macOS Homebrew adapter, hermetic) ([#113](https://github.com/Artexis10/endstate/issues/113)) ([d03615e](https://github.com/Artexis10/endstate/commit/d03615e3cb4a1d940e5ad644957f6b0bce40ff30))
* **engine:** home-manager secrets — documented boundary (referenced, never embedded) ([#112](https://github.com/Artexis10/endstate/issues/112)) ([889670f](https://github.com/Artexis10/endstate/commit/889670f27abd5a95573ece0f9d78ad36b2edee36))

## [2.16.0](https://github.com/Artexis10/endstate/compare/v2.15.0...v2.16.0) (2026-06-03)


### Features

* **backup:** add `backup estimate` to size a push without uploading ([#105](https://github.com/Artexis10/endstate/issues/105)) ([d79c21b](https://github.com/Artexis10/endstate/commit/d79c21b1927d62014918791b7ebdd8cfdf8df739))
* **engine:** extend home-manager curated catalog (eza, gh, lazygit, neovim) ([#106](https://github.com/Artexis10/endstate/issues/106)) ([15f4626](https://github.com/Artexis10/endstate/commit/15f462642a92401d3ee92dd5f4ea5a5573962447))

## [2.15.0](https://github.com/Artexis10/endstate/compare/v2.14.0...v2.15.0) (2026-06-03)


### Features

* **engine:** broaden home-manager curated catalog (fzf, zoxide, bat, tmux, ssh) ([#100](https://github.com/Artexis10/endstate/issues/100)) ([9287985](https://github.com/Artexis10/endstate/commit/9287985a285c10e7a76ce690348c0af7c8ca7c73))

## [2.14.0](https://github.com/Artexis10/endstate/compare/v2.13.0...v2.14.0) (2026-06-03)


### Features

* **capture:** parse nix store-path versions and record them in capture and apply ([#95](https://github.com/Artexis10/endstate/issues/95)) ([1b11fdd](https://github.com/Artexis10/endstate/commit/1b11fdd9fbefb2bd3534394e405ef54c8aba2e73))
* **engine:** home-manager catalog capture (machine -&gt; homeManager.settings) ([#96](https://github.com/Artexis10/endstate/issues/96)) ([05c4867](https://github.com/Artexis10/endstate/commit/05c486703e6699144148264a652ff1bb73fd8aef))
* **engine:** home-manager config capture (close the config apply↔capture loop) ([#86](https://github.com/Artexis10/endstate/issues/86)) ([4b00d18](https://github.com/Artexis10/endstate/commit/4b00d1831f539a1816b0c15fdfb9e8b2cd80cac3))
* **engine:** home-manager config rollback (rollback --enable-restore) ([#93](https://github.com/Artexis10/endstate/issues/93)) ([82c6f8e](https://github.com/Artexis10/endstate/commit/82c6f8e945e533e5d8275882ca0136d5927d7aba))
* **engine:** home-manager config-file wrapper (homeManager.config) ([#87](https://github.com/Artexis10/endstate/issues/87)) ([86913fa](https://github.com/Artexis10/endstate/commit/86913fa440304477d2b6f7c66a199cf53d2cb6f4))
* **engine:** home-manager programs catalog (homeManager.settings — zero-Nix tier) ([#91](https://github.com/Artexis10/endstate/issues/91)) ([849d6fe](https://github.com/Artexis10/endstate/commit/849d6fe6134b13d371b4251768f0a417d0161111))
* **verify:** add home-manager config generation check to realizer verify path ([#97](https://github.com/Artexis10/endstate/issues/97)) ([b231b7f](https://github.com/Artexis10/endstate/commit/b231b7f616ffdb1856ca5ee23c3575b817579ce1))


### Bug Fixes

* **engine:** capture the declared home-manager input, not the generated flake ([#89](https://github.com/Artexis10/endstate/issues/89)) ([2ef97fa](https://github.com/Artexis10/endstate/commit/2ef97fa8f006621014e103afb77b9d3006881143))

## [2.13.0](https://github.com/Artexis10/endstate/compare/v2.12.1...v2.13.0) (2026-06-02)


### Features

* **engine:** Nix home-manager orchestration core (config stage) ([#81](https://github.com/Artexis10/endstate/issues/81)) ([34cb068](https://github.com/Artexis10/endstate/commit/34cb06890952020ab0debd7ec33c2c4c4d4bc086))

## [2.12.1](https://github.com/Artexis10/endstate/compare/v2.12.0...v2.12.1) (2026-06-01)


### Bug Fixes

* **engine:** correct winget version-column parse + uninstall not-found exit code ([#76](https://github.com/Artexis10/endstate/issues/76)) ([48b5249](https://github.com/Artexis10/endstate/commit/48b5249487eafb69961a81a6e293db45c0c78dd3))

## [2.12.0](https://github.com/Artexis10/endstate/compare/v2.11.0...v2.12.0) (2026-06-01)


### Features

* **engine:** version drift enforcement (Phase 7) ([#74](https://github.com/Artexis10/endstate/issues/74)) ([eed3cc0](https://github.com/Artexis10/endstate/commit/eed3cc0d8626a81302966565829ca7ca92b0de6a))

## [2.11.0](https://github.com/Artexis10/endstate/compare/v2.10.0...v2.11.0) (2026-05-31)


### Features

* **backup:** map lastBackupAt + quota into backup status ([#59](https://github.com/Artexis10/endstate/issues/59)) ([#72](https://github.com/Artexis10/endstate/issues/72)) ([84d1b3e](https://github.com/Artexis10/endstate/commit/84d1b3e2ebc2dfc44834d9ea593085b47e47c615))

## [2.10.0](https://github.com/Artexis10/endstate/compare/v2.9.0...v2.10.0) (2026-05-31)


### Features

* **backup:** add 'backup push --if-changed' (content-hash dedup) ([#62](https://github.com/Artexis10/endstate/issues/62)) ([#70](https://github.com/Artexis10/endstate/issues/70)) ([cd0c604](https://github.com/Artexis10/endstate/commit/cd0c604bf0b682627a9f4de39c41f1be29d93c75))

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
