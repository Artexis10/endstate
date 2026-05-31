## Why

Phases 1–5 give the engine a unified, declarative package model across Windows / macOS / Linux:
Nix/winget install (P1/P2), a numbered Provisioning Generation (P2), native Unix rollback (P3),
best-effort winget rollback (P4), and convergence-to-exact-set (P5). One asymmetry remains —
**reproducibility**. On Nix the version is locked for free by the pinned `nixpkgs` rev, so a
rebuild is byte-identical. On Windows it is not: the winget driver records **no version**
(`ProvItem.version` is always empty), and the manifest already carries an **`App.Version` field**
that is **never used**. So a Windows Provisioning Generation cannot even tell you which version was
installed, let alone reproduce it.

This change is **Phase 6 — Windows version capture + pinning**: the winget path now (1) **captures**
the installed version into the Provisioning Generation, and (2) **pins** to a declared
`App.Version` by installing that exact version. Both close the documented Windows reproducibility
gap while keeping the engine's default behavior unchanged.

## What Changes

- **Capture (always on).** Add a `Version` field to `driver.DetectResult`. The winget snapshot
  already parses the `Version` column of `winget list` (`internal/snapshot`); propagate it through
  `DetectBatch` so the driver `apply` path records the installed version in `ProvItem.version`. No
  new winget call. nix version capture is **out of scope** (Nix reproducibility comes from the ref
  pin, not a per-package version).
- **Pinning (opt-in, declarative via `App.Version`).** Add an optional
  **`driver.VersionedInstaller`** interface (`InstallVersion(ref, version string) (*InstallResult,
  error)`), discovered by type-assertion like `BatchDetector`/`Uninstaller`. The winget driver
  implements it via `winget install --version <v> …`. The driver `apply` path calls it when the
  manifest declares `App.Version` **and** the driver implements it; otherwise it installs latest as
  today.
- **Pin-on-install only.** `App.Version` sets the version used **when installing a missing
  package**. An already-installed package of any version stays `present`/skipped — no version
  comparison, no downgrade. (Drift stays visible via capture, for a future convergent phase.)
- **Hard fail on unavailable pin.** If the pinned version is not available, `winget --version`
  exits non-zero and the item resolves as `INSTALL_FAILED` (the version is surfaced in the
  message). No silent fallback to a different version.
- **Backend-scoped.** Pinning is winget-only. The **Nix realizer ignores `App.Version`** — Nix
  pins via its ref (`nixpkgs/<rev>#pkg`), so a per-app version is redundant there.
- **Contract:** rewrite `AI_CONTRACT.md` Non-Goal #3 ("No package version pinning") to allow
  **opt-in, declared** pinning (mirrors the Non-Goal #4 rewording in Phase 3); the
  no-*automatic*-version-management spirit is preserved. Document `App.Version` pinning and the
  now-populated `version` field in `cli-json-contract.md`.

## Capabilities

### New Capabilities

- `windows-version-capture-pinning`: the winget backend records the installed version of each
  package in the Provisioning Generation, and honors a declared `App.Version` by installing that
  exact version (pin-on-install). An unavailable pinned version is a per-item install failure.
  Pinning is winget-scoped; the Nix realizer pins via its ref and ignores `App.Version`.

### Modified Capabilities

- None. `provisioning-generation` already requires generations to record "version when the backend
  exposes it" — this change makes the winget backend *expose* it, fulfilling the existing
  best-effort requirement rather than altering the spec.

## Impact

- `internal/driver/driver.go` — add `DetectResult.Version`; add the optional `VersionedInstaller`
  interface.
- `internal/driver/winget/detect.go` — `DetectBatch` propagates the snapshot's already-parsed
  version into `DetectResult.Version`.
- `internal/driver/winget/install_version.go` (new) — `InstallVersion(ref, version)` via `winget
  install --version`; reuses the existing exit-code classification (unavailable → install failed).
- `internal/commands/apply.go` (driver path) — add `ApplyAction.Version`; populate it from the
  batch capture (present) and the honored pin (installed); install via `VersionedInstaller` when
  `App.Version` is set.
- `internal/commands/apply_generation.go` — copy `ApplyAction.Version` into `ProvItem.version`.
- `docs/ai/AI_CONTRACT.md` — **PROTECTED (maintainer-approved)**: reword Non-Goal #3 to permit
  opt-in declared version pinning.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**: document
  `App.Version` pinning and the populated `version` field on winget generations.
- **Zero default / Unix regression.** Without `App.Version` the install path is byte-identical;
  the Nix realizer is untouched. Proven by host-aware tests + `GOOS=windows` build/vet. The
  real-winget pin/capture smoke is maintainer-side on Windows (no winget on the Linux dev box).
