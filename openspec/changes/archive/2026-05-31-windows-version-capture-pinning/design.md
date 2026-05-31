## Context

Phases 1–5 shipped. Today on Windows:
- `driver.Driver` is `Name`/`Detect(ref)→(installed,displayName,err)`/`Install(ref)`; optional
  `BatchDetector`/`Uninstaller`. None exposes a version.
- `internal/snapshot.TakeSnapshot()` already parses `winget list` into `SnapshotApp{Name, ID,
  Version, Source}` — the **Version is already captured**, but `DetectBatch` copies only `Name` into
  `DetectResult{Installed, DisplayName}` and drops the version.
- `provision.ProvItem` has `Version string` (best-effort, always `""` on winget today). The spec
  already requires generations to record "version when the backend exposes it".
- `manifest.App` has `Version string` (`json:"version,omitempty"`) — **declared in the schema,
  never read**. `AI_CONTRACT.md` Non-Goal #3 states "No package version pinning — MVP does not
  compare or pin versions".
- The Nix realizer pins exact versions via its ref (`nixpkgs/<rev>#pkg`); it has no per-app version
  concept.

## Goals / Non-Goals

**Goals:**
- Record the installed version of each winget package in the Provisioning Generation (capture).
- Honor a declared `App.Version` by installing that exact version on the winget path (pin).
- Zero default/Unix regression; reuse existing parsing and classification.

**Non-Goals:**
- **No convergent pinning** — `App.Version` pins only when *installing*; an already-installed
  different version is left as `present` (no compare, no downgrade). Convergence is future work.
- **No fallback** — an unavailable pinned version is a per-item `INSTALL_FAILED`, never a silent
  install of a different version.
- **No nix version capture / nix `App.Version`** — Nix reproducibility is the ref pin; the realizer
  ignores `App.Version`. (Documented asymmetry.)
- **No version drift detection in `verify`** — capture makes drift *visible* in the generation; a
  verifier is a later phase.

## Decisions (maintainer, this session)

- **(a) Scope = capture + pinning** in one phase.
- **(b) Pin-on-install only** (not convergent) — smallest change that delivers reproducible *fresh*
  rebuilds; keeps the "compare versions" half of the old non-goal out.
- **(c) Hard fail** when the pin is unavailable — reproducibility means the exact version or an
  error; also winget's native behavior, so the existing classifier already maps it to
  `INSTALL_FAILED`.
- **(d) Pin interface = optional `VersionedInstaller`** (type-asserted), matching
  `BatchDetector`/`Uninstaller`/`Pruner`/`Rollbacker`. `Install(ref)` and `mockDriver` stay
  untouched.

## Design

### Capture

`DetectResult` gains `Version string`. `DetectBatch` sets it from the snapshot's already-parsed
`SnapshotApp.Version`. In the driver `apply` plan loop, the per-app `ApplyAction` gains
`Version string`, set from `batchResults[ref].Version` for **present** packages. After a successful
install:
- **pinned install** → `action.Version = app.Version` (the version we asked winget to install, now
  committed);
- **unpinned fresh install** → `action.Version = ""` (winget exposes no version on `install`;
  best-effort, consistent with the existing `ProvItem.version` contract).

`writeProvisioningGeneration` copies `ApplyAction.Version` into `ProvItem.Version`. nix-path actions
never set `Version`, so nix generations keep `version: ""` (capture is winget-scoped this phase).

### Pinning

New optional interface in `internal/driver`:

```go
type VersionedInstaller interface {
    InstallVersion(ref, version string) (*InstallResult, error)
}
```

The winget driver implements it identically to `Install` plus `--version <version>`; the same
exit-code switch classifies the result, so an unavailable version (winget non-zero, not the
already-installed HRESULT) → `StatusFailed`/`ReasonInstallFailed`.

Driver `apply` install loop:

```go
var result *driver.InstallResult
if entry.app.Version != "" {
    if vi, ok := d.(driver.VersionedInstaller); ok {
        result, installErr = vi.InstallVersion(entry.ref, entry.app.Version)
    } else {
        result, installErr = d.Install(entry.ref) // no versioned path on this driver
    }
} else {
    result, installErr = d.Install(entry.ref)
}
```

The Nix realizer path (`apply_realizer.go`) is unchanged — it never reads `App.Version`.

### Edge cases

- `App.Version` set, driver is **not** a `VersionedInstaller`: no such driver exists today (winget
  implements it); the fallback installs latest. (A warning is optional; not specified, to avoid
  scope.)
- `App.Version` set on the **nix** path: ignored by design (nix pins via ref).
- Capture parsing failure (missing/garbled Version column): `Version = ""`, never fails the run —
  identical best-effort posture to display-name parsing.

## Risks / Verification

- **No real winget on the Linux dev box.** Hermetic tests inject `ExecCommand`/snapshot fixtures
  and the `mockDriver`; `GOOS=windows go build`/`vet` gate cross-compilation. The real-winget
  pin+capture smoke (install `Vendor.App --version X` → generation records that version; unavailable
  version → INSTALL_FAILED) is the maintainer's Windows step.
- **CI gotcha (Phase 4 lesson):** any `RunApply` test overriding `newDriverFn` must also override
  `newRealizerFn` (and vice-versa), or windows-latest exercises the wrong backend.
