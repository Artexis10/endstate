## Context

Phase 6 shipped winget version capture + pin-on-install. Today:
- `driver.DetectResult.Version` carries the installed version (from `winget list`); `verify`'s
  winget path reads `batchResults[ref].Version` but only checks presence (`installed → pass`).
- `manifest.App.Version` is the declared pin; `apply` honors it only on a fresh install (already-
  installed different version stays `present`).
- `driver.VersionedInstaller{InstallVersion(ref,version)}` (winget `install --version`) exists; the
  shared winget `install(ref, version)` helper builds the args.
- The Nix realizer pins via its ref and has no per-app version; `runVerifyRealizer` checks presence
  only.

## Goals / Non-Goals

**Goals:**
- `verify` flags a declared-version-vs-installed mismatch as `version_drift` (fail).
- `apply --repin` (opt-in, confirmed) reinstalls the declared version over a drifted one.
- Zero default/Unix regression; reuse Phase-6 capture + interface.

**Non-Goals:**
- **No semver** — exact match only (you pinned an exact version).
- **No auto-converge** — `--repin` is always opt-in + `--confirm`-gated; default apply leaves drift.
- **No nix involvement** — winget-only; realizer verify untouched, realizer apply ignores `--repin`.
- **No drift check in apply's post-install verify phase** — drift is the `verify` command's concern.

## Decisions (maintainer, brainstormed)

- Detect + **opt-in** converge (not detect-only, not auto-converge).
- Drift = **fail** with reason `version_drift` (keeps the verify pass/fail contract binary; the
  reason code lets the GUI distinguish "wrong version" from "missing").
- **Exact** comparison (trimmed); older or newer both drift.
- Convergence mechanism = **`winget install --version X --force`** (mechanism A). Fallback if
  `--force` won't downgrade on real winget = uninstall + InstallVersion (maintainer decides on
  Windows; documented).
- Flag name **`--repin`**.

## Design

### Detect (verify)

`VerifyItem` gains `Version string` (installed) and `Expected string` (declared), both `omitempty`.
In `RunVerify`'s winget pass branch, after `installed`:

```go
got := strings.TrimSpace(br.Version)
want := strings.TrimSpace(app.Version)
if want != "" && got != "" && got != want {
    item.Status, item.Reason = "fail", driver.ReasonVersionDrift
    item.Version, item.Expected = got, want
    item.Message = fmt.Sprintf("installed %s, want %s", got, want)
    emit failed/version_drift; failCount++
} else {
    // existing pass path (also set item.Version = got for visibility)
}
```

`runVerifyRealizer` is untouched.

### Converge (apply --repin)

`VersionedInstaller` gains `ReinstallVersion(ref, version)`; the winget shared helper takes a
`force bool` (appends `--force`); `Install`/`InstallVersion` pass `false`, `ReinstallVersion` passes
`true`.

In `apply.go` (driver path): `ApplyFlags.Repin`. The plan loop marks a **drifted pinned present**
app (`flags.Repin && installed && app.Version != "" && version != "" && version != app.Version`) as
a converge entry (`appPlan.repin = true`, action reason `version_drift`). The apply loop:
- refuses if `flags.Repin && !flags.Confirm && !flags.DryRun` → `INTERNAL_ERROR` ("requires
  --confirm"), mirroring the realizer prune refusal; install results stand.
- for a repin entry, calls `vi.ReinstallVersion(ref, app.Version)`; on success records
  `Status=installed`, `Version=app.Version` (→ generation carries the converged version).
- `--dry-run` previews the would-repin set without reinstalling.

The realizer apply path never reads `App.Version`/`Repin` — nix is unaffected.

## Risks / Verification

- **No real winget on the Linux box.** Detect is fully hermetic (mockDriver `versions` map). Converge
  dispatch is hermetic (mock asserts `ReinstallVersion` under `--repin --confirm`), but whether
  `winget install --version X --force` actually downgrades is **maintainer-verified on Windows**;
  documented fallback = uninstall + InstallVersion.
- **CI gotcha (Phase 4):** `RunApply`/`RunVerify` tests override BOTH `newDriverFn` AND
  `newRealizerFn` or windows-latest exercises the wrong backend.
