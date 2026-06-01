## Why

Phase 6 (`windows-version-capture-pinning`) let a manifest pin a Windows app to an exact version
(`"version": "1.2.0"` → `winget install --version`) and recorded the installed version in the
Provisioning Generation. But pinning is **pin-on-install only**: if the app is already installed at
a different version, the engine leaves it, and `verify` reports it as installed regardless of
version. So a pin can **drift** silently — an app auto-updates away from the declared version and
nothing notices, and there is no way to snap it back.

This change is **Phase 7 — version drift enforcement**. It makes a declared `App.Version`
*enforceable*: `verify` reports drift, and `apply` can converge it on request.

## What Changes

- **Detect (always on, `verify`).** When a manifest app declares `version` and the installed
  version (captured by Phase 6 into `driver.DetectResult.Version`) differs, `verify` reports that
  item as **fail** with a new reason **`version_drift`** and a message like `installed 1.1.0, want
  1.2.0`, and exposes `version` (installed) and `expected` (declared) fields. Comparison is **exact**
  (whitespace-trimmed; older *or* newer both count as drift). Drift is evaluated only for apps that
  declare a version; unpinned apps are unaffected. When the backend exposes no installed version,
  no drift is flagged (best-effort, mirroring Phase 6 capture).
- **Converge (opt-in, `apply --repin`).** A new **`apply --repin`** flag reinstalls the declared
  version over a drifted one via `winget install --version <v> --force`. It is **`--confirm`-gated**
  (it is a reinstall / possible downgrade) and **`--dry-run`-previewable**. Default `apply` is
  unchanged — without `--repin`, a drifted-but-present app is left alone (still pin-on-install),
  honoring `non-destructive-defaults`.
- **Mechanism.** Extend the Phase-6 `driver.VersionedInstaller` interface with
  `ReinstallVersion(ref, version)` (winget `install --version --force`). The apply driver path
  type-asserts it and converges drifted pinned apps; a converged app is recorded `installed` at the
  declared version (reuses Phase-6 capture). Add reason `version_drift`.
- **Backend-scoped.** Detection and convergence are **winget-only**. The Nix realizer pins exact
  versions through its ref, so it has no per-app version to drift; the realizer verify path is
  untouched and the realizer apply path ignores `--repin` (not an error).
- **Contract:** reword `AI_CONTRACT.md` Non-Goal #3 again — Phase 6 set it to "never auto-compares
  versions or enforces drift"; Phase 7 adds opt-in comparison (read-only `verify`) and confirmed
  convergence (`apply --repin`), preserving "no *silent/automatic* version changes".

## Capabilities

### New Capabilities

- `version-drift-enforcement`: `verify` reports a declared-version-vs-installed mismatch as a
  `version_drift` failure; `apply --repin --confirm` reinstalls the declared version over a drifted
  one (opt-in, `--dry-run`-previewable). Winget-scoped; the Nix realizer ignores it (ref-pinned).

### Modified Capabilities

- None. `verification-first` already requires `verify` to report observable state; this surfaces a
  finer-grained mismatch within that contract. `non-destructive-defaults` is honored — convergence
  requires `--repin --confirm`.

## Impact

- `internal/driver/driver.go` — `ReasonVersionDrift = "version_drift"`; extend `VersionedInstaller`
  with `ReinstallVersion(ref, version)`.
- `internal/driver/winget/` — `force` param on the shared `install` helper; `ReinstallVersion`
  (`--force`); update the compile-time interface assertion.
- `internal/commands/verify.go` — `VerifyItem.Version`/`.Expected`; drift comparison in the winget
  pass branch.
- `internal/commands/apply.go` — `ApplyFlags.Repin`; drift→converge action in the driver plan/apply
  loop; `--repin` refusal without `--confirm`; `--dry-run` preview; realizer path ignores it.
- `cmd/endstate/main.go` — **PROTECTED (maintainer-approved, additive)**: parse `--repin`; usage.
- `docs/ai/AI_CONTRACT.md` — **PROTECTED (maintainer-approved)**: reword Non-Goal #3.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**: `verify`
  `version_drift` + `version`/`expected`; `apply --repin`.
- **Zero default / Unix regression.** Without a declared version, nothing changes; without `--repin`,
  apply is byte-identical; the Nix realizer is untouched. Proven by host-aware tests + `GOOS=windows`
  build/vet. The real-winget `--force` reinstall smoke is maintainer-side on Windows.
