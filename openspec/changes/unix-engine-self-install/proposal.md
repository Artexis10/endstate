# Proposal: Unix engine self-install + multi-platform release artifacts

## Problem

Endstate's engine already provisions Linux and macOS (the Nix realizer and the macOS Homebrew lane),
but the distribution and self-install story is still Windows-only:

1. **Releases publish only `endstate.exe`.** The `Release` workflow builds a single Windows binary,
   so a Linux or macOS user has no published artifact to download — they must build from source.
2. **`endstate bootstrap` is Windows-only.** The self-install command installs to
   `%LOCALAPPDATA%\Endstate\bin`, writes a `.cmd` shim, and edits the user PATH via `setx`. On
   Linux/macOS the same command would attempt a Windows-only layout and PATH mutation, so the
   "install the running binary onto my PATH" keystone does not exist off Windows.

For the platforms the engine already supports, both gaps make a clean install harder than it should
be. This change closes them.

## What Changes

- **Split `endstate bootstrap` by platform.** `go-engine/internal/commands/bootstrap.go` becomes a
  shared file (flags, the `BootstrapData` payload, the `copyFile` helper). The current Windows
  behavior moves byte-for-byte into `bootstrap_windows.go` (`//go:build windows`). A new
  `bootstrap_unix.go` (`//go:build !windows`) implements Linux/macOS:
  - install dir `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin`, binary copied to
    `<installDir>/lib/endstate` and chmod'd `0755` (the copy is created non-executable by default);
  - a symlink `$HOME/.local/bin/endstate` → `<installDir>/lib/endstate`, re-pointed idempotently
    (an existing symlink/file at the target is removed first, never nested or errored on);
  - **no PATH or shell-rc mutation, ever** — `addedToPath` is always `false`; when
    `$HOME/.local/bin` is not on PATH the human-readable output adds a one-line hint (the JSON
    payload shape is unchanged);
  - a self-copy guard: when the running binary already resolves to the install target (a
    re-bootstrap of the installed copy through its own symlink), the copy is skipped rather than
    truncating the running binary.
- **Publish per-platform Unix release artifacts.** The `Release` workflow gains a
  `publish-unix-artifacts` job on `ubuntu-latest` that cross-compiles with `CGO_ENABLED=0` for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, producing
  `endstate-{linux,darwin}-{amd64,arm64}` plus a sibling `.sha256` (lowercase hex), uploaded to the
  same release with the same version-embedding ldflags as the Windows job. The Windows
  `publish-artifacts` job is unchanged.
- **Document distribution** in `docs/COMPATIBILITY.md` (one paragraph): Windows `endstate.exe` plus
  per-platform Unix binaries on GitHub Releases, and `endstate bootstrap` self-install on all three
  platforms.

The CI workflow change carries no behavior spec of its own (it is infrastructure), but it is part of
the same capability story: a Unix user can now both **download** a published binary and **self-install**
it with `endstate bootstrap`.

## Capabilities

### Modified Capabilities
- `bootstrap-full-sync`: the bootstrap behavior, previously Windows-only, is now platform-aware. The
  Windows requirements are restated with explicit Windows scope; new requirements cover the Unix
  install layout, the symlink, the no-PATH-mutation rule, idempotent re-pointing, and the self-copy
  guard.

## Impact

- Modified: `go-engine/internal/commands/bootstrap.go` (now shared helpers/types only).
- Added: `go-engine/internal/commands/bootstrap_windows.go`, `bootstrap_unix.go`,
  `bootstrap_unix_test.go`, `bootstrap_windows_test.go`.
- Modified: `.github/workflows/release.yml` (new `publish-unix-artifacts` job; Windows job
  unchanged). Protected area — maintainer-authorized for this change.
- Modified: `docs/COMPATIBILITY.md` (distribution paragraph).
- Backward compatible: the Windows bootstrap behavior and the existing `installPath`/`shimPath`/
  `addedToPath` payload shape are preserved on every platform.
