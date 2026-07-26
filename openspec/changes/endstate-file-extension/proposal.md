# Proposal: endstate-file-extension

## Why

A capture bundle is the artifact Endstate exists to produce, and today it is called `.zip`. That
name costs three things:

- **Recognition.** A `.zip` in Downloads is indistinguishable from every other archive. Nothing
  tells the user, the OS, or a colleague receiving it that this file is a machine snapshot.
- **A double click does nothing useful.** Windows has no way to route the file to Endstate,
  because `.zip` already belongs to the shell's archive handler and always will.
- **It reads as a shipping detail rather than a format.** `MyProfile.zip` looks like something
  that got zipped; `MyProfile.endstate` looks like something Endstate made.

`.endstate` is a rename, not a new format. The file is byte-for-byte the same zip container with
`manifest.jsonc` at its root — renaming it to `.zip` and opening it in any archiver still works.
That transparency is deliberate and load-bearing: the promise "your setup is not locked inside our
tool" is only credible if the artifact stays inspectable with tools the user already has.

## What Changes

- **`.endstate` is accepted everywhere `.zip` is accepted.** One predicate,
  `manifest.IsBundlePath`, decides what a bundle looks like from the outside; `bundle.IsBundle`
  delegates to it rather than re-deriving the rule. Matching is case-insensitive.
- **`.zip` keeps working, permanently.** This is back-compat with no sunset: every bundle ever
  written is still a valid input to `rebuild`, `verify`, and `--profile` resolution.
- **Capture writes `.endstate` by default.** `--profile <name>` produces
  `<ProfilesDir>\<name>.endstate`; a capture without an explicit `--out` derives
  `<manifest-stem>.endstate`.
- **An explicit `--out` keeps the caller's bundle extension.** `--out foo.zip` still writes
  exactly `foo.zip`. Only a path with no extension, or one whose extension does not name a bundle
  at all, is normalized to `.endstate` — writing a zip container under a `.jsonc` name would make
  the result unreadable by every loader that dispatches on extension.
- **Profile resolution prefers `.endstate`, then falls back to `.zip`**, ahead of the loose-folder
  and bare-manifest formats as before.

`outputFormat` in the capture envelope stays `"zip"`. It names the container format, which has not
changed, and it is a published contract field consumed by the GUI.

## Capabilities

### New Capabilities
- `endstate-file-extension`: capture bundles have a first-class `.endstate` extension, written by
  default and accepted interchangeably with the legacy `.zip`.

### Modified Capabilities
<!-- none — bundle layout, metadata, and the envelope contract are unchanged -->

## Impact

- `go-engine/internal/manifest/bundle_source.go` — `BundleExt`, `LegacyBundleExt`,
  `BundleExtensions`; `IsBundlePath` matches the set.
- `go-engine/internal/bundle/extract.go` — `IsBundle` delegates to `manifest.IsBundlePath`.
- `go-engine/internal/commands/capture_config.go` — `captureBundleOutputPath` defaults to
  `.endstate` and honours an explicit bundle extension.
- `go-engine/internal/commands/profile.go` — resolution order gains `.endstate`.
- `go-engine/cmd/endstate/main.go`, `internal/commands/rebuild.go` — help and remediation text
  (PROTECTED area: modified under explicit instruction).
- Contract docs: `docs/contracts/profile-contract.md`, `docs/contracts/capture-artifact-contract.md`,
  `docs/contracts/cli-json-contract.md`.
- **Consumer (separate `endstate-gui` change):** drop zone, import transport, save/open dialog
  filters, and a Windows file association so double-clicking a `.endstate` opens Endstate.
- Backward-compatible: no schema bump, no envelope change, no bundle layout change.
