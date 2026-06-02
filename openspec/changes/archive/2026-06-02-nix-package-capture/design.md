## Context

The realizer abstraction and apply/verify forks already exist; this change only adds a capture path.
Today:
- `RunCapture` (`internal/commands/capture.go`) is winget-only: `takeSnapshotFn = snapshot.WingetExport`
  enumerates packages, each app is keyed `Refs["windows"] = sApp.ID`, and a host without winget errors
  `WINGET_NOT_AVAILABLE`. The output manifest is `captureManifestOutput{version:1, name, captured, apps}`
  written with `json.MarshalIndent` + `os.WriteFile` to `resolveOutputPath(flags)`, with an
  INV-CAPTURE-2 non-empty-after-write check.
- `RunVerify`/`RunApply` already fork on `newRealizerFn()` (shared seam, `verify.go:37`): a successful
  realizer takes the whole-set path, else control falls through to the winget driver path. On Windows
  `selectRealizer` returns `ErrNoRealizer`.
- `realizer.Realizer.Current() (Set, error)` returns `Set{Generation, Elements map[string]Element}`,
  `Element{Name, AttrPath, StorePaths}` (parsed from `nix profile list --json`). For a nixpkgs install
  the element key/`Name` is the bare attr leaf (e.g. `ripgrep`).
- `apply` reads `app.Refs[runtime.GOOS]` **strictly** (`apply_realizer.go`), then
  `Backend.ResolveInstallable` expands a bare attr against the pin (`nixpkgs#ripgrep`); `verify`/`plan`
  present-detection matches by **leaf attr** (`presentInSet`/`isPresent`).

## Goals / Non-Goals

**Goals:**
- `capture` produces a manifest from the current Nix profile on linux/darwin.
- The emitted manifest **round-trips**: `apply` of it re-installs the same `Current()` set.
- Zero winget/Windows regression; reuse the existing manifest output shape and write/verify invariants.

**Non-Goals:**
- **No config/settings capture** — that is the separate home-manager paradigm (a future effort).
- **No config modules / manual apps / zip bundle** on the Nix path — those are the Windows app catalog.
- **No version capture** — no `App.Version` on the Nix path; store-path version parsing belongs with
  the separate nix-version-capture effort.
- **No original-flakeref recovery** — power-user flake sources are a documented round-trip limitation.

## Decisions (maintainer, confirmed)

- **Ref form = bare attr** (`Refs[runtime.GOOS] = element.Name`). Justification: apply's
  `ResolveInstallable` expands it against the pin; present-detection matches by leaf; it carries no
  system tuple, so it round-trips through apply/verify/plan **and** is portable linux↔darwin. The
  `AttrPath` form (`legacyPackages.x86_64-linux.ripgrep`) was rejected — it bakes in the arch and
  breaks cross-OS rebuild. Explicit `nixpkgs#attr` was rejected — strictly less portable (hardcodes
  the pin name) with no benefit over bare attr. Confirmed via round-trip analysis + smoke.
- **Version out of scope** — packages only; keeps the change minimal and avoids fragile store-path
  parsing.
- **Mirror the verify fork exactly** — reuse `newRealizerFn` so the same Windows no-op + the same test
  injection (`withFakeRealizer`) apply, and exactly one of `newDriverFn`/`newRealizerFn` drives a host.

## Design

### Fork (capture.go — only edit to the existing file)

At the very top of `RunCapture`, after the emitter is built and **before** `EmitPhase("capture")`:

```go
func RunCapture(flags CaptureFlags) (interface{}, *envelope.Error) {
    runID := buildRunID("capture")
    emitter := events.NewEmitter(runID, flags.Events == "jsonl")

    // Realizer path (Nix on linux/darwin). On Windows newRealizerFn returns
    // ErrNoRealizer and control falls through to the winget path below,
    // byte-identical to prior behavior.
    if rz, rerr := newRealizerFn(); rerr == nil {
        return runCaptureRealizer(flags, rz, emitter)
    }

    emitter.EmitPhase("capture")   // existing winget path, unchanged from here down
    ...
}
```

`EmitPhase("capture")` stays the first event on the winget path; `runCaptureRealizer` emits its own.

### Nix capture path (capture_realizer.go — new)

```go
func runCaptureRealizer(flags CaptureFlags, r realizer.Realizer, emitter *events.Emitter) (interface{}, *envelope.Error) {
    driverName := r.Name()
    emitter.EmitPhase("capture")

    cur, cerr := r.Current()
    if cerr != nil {
        if rerr, ok := cerr.(*realizer.Error); ok && isSystemic(rerr.Code) {
            return nil, realizerEnvelopeError(rerr)   // reuse apply_realizer helpers
        }
        // non-systemic read issue: treat as empty (capture an empty set)
    }

    // Deterministic order: sort element names.
    // For each element: capturedApp{ID: name, Refs: {runtime.GOOS: el.Name}, Name: name};
    // emit a captured ItemEvent.
    // --update + --manifest: merge with existing manifest, host-keyed (dedup on Refs[GOOS]).
    // Sanitize/sort, build captureManifestOutput{version:1, name, captured, apps}, write to
    // resolveOutputPath(flags) with MarshalIndent + WriteFile + INV-CAPTURE-2 non-empty check.
    // Return *CaptureResult (same type as the winget path): AppsIncluded (Source = driverName),
    // empty ConfigModules/config slices, OutputPath, OutputFormat "jsonc", Sanitized,
    // Counts{TotalFound, Included}, Manifest{Name, Path}.
}
```

Reuses existing symbols: `capturedApp`, `cleanApp`, `captureManifestOutput`, `CaptureResult`,
`resolveOutputPath`, and the realizer error helpers `isSystemic`/`realizerEnvelopeError`. The small
write block is reproduced (not refactored out of the winget path) so the winget path stays
byte-identical.

## Risks / Verification

- **Round-trip is the bar.** Hermetic tests assert the emitted refs are bare-attr host-keyed and the
  manifest shape is valid. The real proof is the **apply → capture → apply** smoke on the Linux box:
  `apply` `[jq, ripgrep]` → `capture` → `apply` the captured manifest into a **fresh**
  `ENDSTATE_NIX_PROFILE` → `nix profile list` shows the same set.
- **No winget on the Linux box / no regression.** The winget path is unchanged below the fork and is
  unreachable on Linux (realizer wins); Windows is covered by `GOOS=windows go build`/`go vet` and a
  host-aware driver-path guard test (`newRealizerFn → ErrNoRealizer` forces the winget path).
- **Non-pin flake sources** may not round-trip identically (bare attr resolves against the pin) —
  documented limitation, not a regression of the common nixpkgs case.
