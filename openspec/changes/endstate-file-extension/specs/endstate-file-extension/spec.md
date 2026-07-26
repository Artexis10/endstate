## ADDED Requirements

### Requirement: Capture bundles have a first-class `.endstate` extension

A capture bundle SHALL be recognised by the extension `.endstate` as well as the legacy `.zip`.
Extension matching SHALL be case-insensitive.

`.endstate` SHALL name the same zip container the engine has always written: `manifest.jsonc` at
the archive root, plus `configs/`, `metadata.json`, and `provenance/`. The engine SHALL NOT
encrypt, obfuscate, or otherwise alter the container, so a bundle renamed to `.zip` SHALL remain
openable by any ordinary archiver.

The engine SHALL derive the bundle extension set from a single definition, so a new extension can
never be accepted in one command and rejected in another.

#### Scenario: An `.endstate` bundle loads exactly as a `.zip` does

- **WHEN** the manifest loader is given a capture bundle named `<name>.endstate`
- **THEN** it reads `manifest.jsonc` from the archive root and returns the same manifest it would
  return for the identical container named `<name>.zip`

#### Scenario: Extension matching ignores case

- **WHEN** a path ends in `.ENDSTATE`, `.EndState`, `.ZIP`, or `.Zip`
- **THEN** it is treated as a capture bundle

#### Scenario: A bundle extension inside a longer name is not a bundle

- **WHEN** a path ends in `.endstate.jsonc` or `.zip.jsonc`
- **THEN** it is treated as a manifest, not a bundle

#### Scenario: An `.endstate` that is not a zip is rejected

- **WHEN** a file named `<name>.endstate` is not a readable zip archive
- **THEN** loading it fails
- **AND** its bytes are NOT parsed as raw JSONC

### Requirement: Legacy `.zip` bundles keep working permanently

Every bundle the engine has ever written SHALL remain a valid input. `.zip` SHALL NOT be
deprecated, warned about, or scheduled for removal — this change renames the artifact, it does not
retire a format.

#### Scenario: A `.zip` bundle still loads

- **WHEN** the manifest loader is given a capture bundle named `<name>.zip`
- **THEN** it reads the manifest from the archive root and succeeds

#### Scenario: A `.zip` bundle still rebuilds

- **WHEN** `rebuild --from <name>.zip` runs
- **THEN** the bundle is extracted and the pipeline proceeds as before

#### Scenario: A `.zip` profile still resolves

- **WHEN** `--profile <name>` is resolved and only `<ProfilesDir>\<name>.zip` exists
- **THEN** that bundle is selected

### Requirement: Capture writes `.endstate` by default and honours an explicit bundle extension

A capture that produces a bundle SHALL write `.endstate` unless the caller named a different
bundle extension.

- With `--profile <name>` the bundle SHALL be `<ProfilesDir>\<name>.endstate`.
- With no `--out`, the bundle path SHALL be the manifest path with its extension replaced by
  `.endstate`.
- With an `--out` whose extension already names a bundle, the engine SHALL write exactly that
  path, preserving the caller's extension and its casing.
- With an `--out` that has no extension, or an extension that does not name a bundle, the engine
  SHALL write the default `.endstate`. A zip container written under a non-bundle name such as
  `.jsonc` would be unreadable by every loader that dispatches on extension.

The capture envelope's `outputFormat` SHALL remain `"zip"`: it names the container format, which
is unchanged.

#### Scenario: Default capture writes `.endstate`

- **WHEN** capture produces a bundle and no `--out` is supplied
- **THEN** the output path ends in `.endstate`

#### Scenario: A named profile is written as `.endstate`

- **WHEN** `capture --profile "My-Desktop"` produces a bundle
- **THEN** the output path is `<ProfilesDir>\My-Desktop.endstate`

#### Scenario: An explicit `.zip` output is honoured exactly

- **WHEN** `capture --out <path>.zip` produces a bundle
- **THEN** the output path is exactly `<path>.zip`

#### Scenario: An `--out` with no extension gets the default

- **WHEN** `capture --out <path>` produces a bundle and `<path>` has no extension
- **THEN** the output path is `<path>.endstate`

#### Scenario: An `--out` with a non-bundle extension is normalized

- **WHEN** `capture --out <path>.jsonc` produces a bundle
- **THEN** the output path is `<path>.endstate`

### Requirement: Profile resolution prefers `.endstate` over the legacy `.zip`

When resolving a named profile, the engine SHALL check `<ProfilesDir>\<name>.endstate` first, then
`<ProfilesDir>\<name>.zip`, then the loose-folder and bare-manifest formats in their existing
order. First match wins.

#### Scenario: `.endstate` wins when both bundles exist

- **WHEN** both `<ProfilesDir>\<name>.endstate` and `<ProfilesDir>\<name>.zip` exist
- **THEN** the `.endstate` bundle is selected

#### Scenario: Bundles still win over manifests

- **WHEN** a bundle and a bare manifest share a name
- **THEN** the bundle is selected
