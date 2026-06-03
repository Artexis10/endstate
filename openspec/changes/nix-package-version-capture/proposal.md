## Why

`capture` on the Nix realizer path (Linux/macOS) already reads the installed set and emits each
element as a manifest app. It does NOT capture versions. On the winget path, `DetectBatch` populates
`DetectResult.Version` from the `winget list` output → `ApplyAction.Version` → `ProvItem.Version`,
and the captured manifest app can carry a `version` field so an `apply` can pin the exact version.

The Nix store path already encodes the package version — every element's `StorePaths` contains
paths of the form `/nix/store/<32-char-hash>-<name>-<version>[-output]`. This version is
machine-readable without any additional Nix CLI calls.

This change gives the Nix capture path version-capture parity with winget:
- Parse the installed version from the element's `StorePaths` (pure, no CLI, best-effort).
- Record it in `ProvItem.Version` when `apply` records the Provisioning Generation.
- Emit it into the captured manifest's `App.Version` during `capture` so a re-apply can pin the
  same version.

## What Changes

- **Pure store-path version parser** in `internal/realizer/nix/` (`versions.go`). Given an
  element's `Name` and its `StorePaths`, returns the best-effort version string. Unparseable →
  empty string (never fails capture).
- **`capture_realizer.go`**: populate the captured app's `Version` field from the parsed store-path
  version, matching the winget capture shape.
- **`apply_realizer.go`**: populate `ApplyAction.Version` for installed and present packages using
  the same parser on the post-apply `Current()` set, so `ProvItem.Version` is recorded in the
  Provisioning Generation.
- **Unit tests**: thorough parser tests with realistic store-path fixtures; capture command tests
  asserting version is recorded in the manifest and generation.

## Capabilities

### New Capabilities

- `nix-package-version-capture`: the Nix capture path parses the installed version from each
  element's store path and records it in the captured manifest app AND in the Provisioning
  Generation, giving version-capture parity with winget. Version is best-effort; an unparseable
  store path never fails the run.

### Modified Capabilities

- None. The winget capture path, Windows behavior, and home-manager paths are unchanged.

## Impact

- `internal/realizer/nix/versions.go` — **new**: `StorePathVersion(name string, storePaths []string) string`.
- `internal/realizer/nix/versions_test.go` — **new**: comprehensive unit tests.
- `internal/commands/capture_realizer.go` — populate `Version` on each `capturedApp`.
- `internal/commands/apply_realizer.go` — populate `ApplyAction.Version` from the post-realize set.
- `internal/commands/capture_realizer_test.go` — update existing test + add version assertions.
- `internal/commands/apply_realizer_test.go` — add version-in-generation assertion.
- **Zero winget / Windows regression.** The parser is Nix-only; winget capture is unchanged.
