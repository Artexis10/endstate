## Why

`apply` and `verify` already work on the **Nix realizer** (Linux/macOS) — but `capture` does not.
`capture` is winget-only: it enumerates winget-managed packages via `winget export` and errors with
`WINGET_NOT_AVAILABLE` on a host without winget. So Endstate's core promise — *snapshot this machine
→ manifest → rebuild elsewhere* — is **half-open on Unix**: you can apply a Nix manifest, but you
cannot produce one from the current Nix profile.

This change closes that loop. `capture` gains a realizer path, parallel to the existing winget export
path, that reads the installed Nix profile and emits each element as a manifest app with a host-keyed
ref. The round-trip is the acceptance bar: **capture → apply re-installs the same set**, and the
emitted manifest is portable to another machine/OS.

## What Changes

- **Realizer fork in `capture` (always on, Linux/macOS).** `RunCapture` gains a realizer fork at the
  top, mirroring `RunVerify`/`RunApply`: when `newRealizerFn()` succeeds (Nix on linux/darwin) it
  takes a new Nix capture path; otherwise (Windows → `ErrNoRealizer`) it falls through to the
  existing winget path, byte-identical.
- **Nix capture path.** Reads the current set via `realizer.Realizer.Current()` and, for each
  element, emits `manifest.App{ID: <element name>, Refs: {runtime.GOOS: <ref>}}`. The output manifest
  is written exactly like the winget path (same `version`/`name`/`captured`/`apps` shape, same output
  path resolution, same non-empty-after-write check), sorted by id for deterministic output.
- **Ref form = bare attr.** Each element is emitted as `Refs[runtime.GOOS] = element.Name` (e.g.
  `"linux": "ripgrep"`). On `apply`, `Backend.ResolveInstallable` expands the bare attr against the
  pin (`nixpkgs#ripgrep`); `verify`/`plan` present-detection matches by leaf attr. This is the most
  portable form — no system tuple (`x86_64-linux`) is baked in, so a manifest captured on Linux
  re-applies on macOS — and it round-trips through all three commands.
- **Packages only.** The Nix path does **not** synthesize config modules, manual apps, or a zip
  bundle (those are the Windows app catalog). It emits a minimal, valid packages manifest.
- **Known limitation.** A package installed from a non-pin flake source (not the configured
  nixpkgs pin) may not round-trip identically — its bare attr resolves against the pin. This is
  acceptable and documented; preserving original flakerefs is out of scope.

## Capabilities

### New Capabilities

- `nix-package-capture`: `capture` works on realizer backends (Nix on linux/darwin). It emits the
  installed Nix package set as a manifest whose host-keyed refs round-trip through `apply` to the
  same set. Config/settings capture is out of scope. The winget capture path is unchanged.

### Modified Capabilities

- None. This extends `capture` to a second backend within the existing capture contract
  (the manifest output shape and write/verify invariants are reused unchanged). The winget path is
  byte-identical and Windows behavior does not change.

## Impact

- `internal/commands/capture.go` — add the realizer fork at the top of `RunCapture` (the only edit
  to the existing file; the winget path below the fork is untouched).
- `internal/commands/capture_realizer.go` — **new**: `runCaptureRealizer` (the Nix capture path).
- `internal/commands/capture_realizer_test.go` — **new**: hermetic, host-aware tests (fake realizer
  injected via the shared `newRealizerFn` seam).
- **Zero winget / Windows regression.** On Windows `newRealizerFn` returns `ErrNoRealizer`, so the
  fork is a no-op and the winget path runs unchanged. Proven by host-aware tests + `GOOS=windows`
  build/vet. The real-nix round-trip smoke (apply → capture → apply into a fresh profile) is run on
  the Linux dev box.
