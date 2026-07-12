# Proposal: unigetui-import

## Why

UniGetUI (~25k stars, now under the Devolutions org) is the most popular package-manager UI on Windows, and its backup feature stores only the package list — by its own docs, "not your personal data or settings." Its community explicitly asks for the missing layer (Discussion #3443: app-config transfer was "95% of the effort" of a migration; maintainer: WinGet-Config support won't come "in the near future"; issues #2377, #3890 ask for settings backup/sync). Importing a UniGetUI backup as an Endstate manifest gives those users a one-step bridge into capture/restore/verify — and is the concrete substance behind the planned public interop proposal to the UniGetUI project (2026-07 market brief: borrow adjacency to an existing audience; the interop thread is the acquisition mechanism).

## What Changes

- New CLI command: `endstate import --from unigetui --path <file>` consuming a UniGetUI bundle/backup (`.ubundle`, JSON `export_version: 3`) and emitting a JSONC manifest.
- Mapping: winget-source packages become manifest apps (`Id` → `refs.windows`, `Name` → `displayName`); with `--pin`, the bundle's `Version` (or a per-package `InstallationOptions.Version` pin, which always wins) is written to the app's `version` field.
- Every non-imported package is reported with a reason — non-winget managers (chocolatey, npm, pip, PSGallery, scoop) and `incompatible_packages` are listed, never silently dropped.
- Import is a pure transform: no network calls, no installs, deterministic output.
- New dispatcher entry in `cmd/endstate/main.go` (protected area — explicit user instruction for this change was given 2026-07-10).

## Capabilities

### New Capabilities

- `manifest-import`: importing external package-list formats into Endstate manifests — source-format parsing, mapping fidelity, skip transparency, and output-manifest validity. UniGetUI `.ubundle` is the first supported source; a future DSC-configuration import extends this same capability.

### Modified Capabilities

<!-- none -->

## Impact

- **New code**: `go-engine/internal/importer/` (ubundle parser + winget mapper), `go-engine/internal/commands/import.go`.
- **Reused code**: manifest types and JSONC conventions (`internal/manifest`); envelope construction for `--json`.
- **Touched (protected)**: `go-engine/cmd/endstate/main.go` dispatcher.
- **Dependencies**: none added — `.ubundle` is plain JSON.
- **Contracts**: `--json` output follows the existing envelope; no event-schema changes.
- **Non-goals (v0)**: no UniGetUI settings import, no chocolatey/scoop driver support (report-only), no reverse export to `.ubundle`, no cloud-backup (Gist) fetching — file input only.
