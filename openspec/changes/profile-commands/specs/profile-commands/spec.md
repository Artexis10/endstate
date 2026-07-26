## ADDED Requirements

### Requirement: Profile Command with Overlay Management Subcommands
The CLI SHALL expose an `endstate profile` command with the subcommands `new`, `exclude`, `exclude-config`, `add`, `show`, and `list`. The command SHALL be advertised in the capabilities list and in top-level help, and `endstate help profile` SHALL print usage and examples for every subcommand. Profiles SHALL be resolved against the canonical profiles directory (`Documents\Endstate\Profiles\`), which SHALL be created if it does not exist.

#### Scenario: Profile command is advertised
- **WHEN** the capabilities list or top-level help is requested
- **THEN** `profile` SHALL be listed alongside the other top-level commands

#### Scenario: Profile help lists every subcommand
- **WHEN** `endstate help profile` runs
- **THEN** usage SHALL be printed for `new`, `exclude`, `exclude-config`, `add`, `show`, and `list`
- **AND** worked examples SHALL be included

#### Scenario: Profiles directory is created on demand
- **WHEN** a profile subcommand runs and `Documents\Endstate\Profiles\` does not exist
- **THEN** the directory SHALL be created before the subcommand executes

#### Scenario: Missing engine script fails cleanly
- **WHEN** `profile-commands.ps1` cannot be resolved
- **THEN** the command SHALL fail with exit code 1
- **AND** with `--json` the envelope SHALL carry error code `ENGINE_SCRIPT_MISSING`

### Requirement: Only Bare Profiles Are Mutable
Profile mutations SHALL apply only to bare `.jsonc` profiles. When a profile resolves to a zip bundle or a folder, any mutating subcommand (`exclude`, `exclude-config`, `add`) SHALL fail without modifying anything. Mutations SHALL use the `Read-ManifestRaw` + modify + `Write-Manifest` pattern so that resolved include content is never flattened into the overlay file.

#### Scenario: Zip profile rejects mutation
- **WHEN** `endstate profile exclude <name> <id>` targets a profile that resolves to a zip bundle
- **THEN** the command SHALL fail stating that only bare `.jsonc` profiles are mutable
- **AND** the bundle SHALL be left untouched

#### Scenario: Folder profile rejects mutation
- **WHEN** a mutating subcommand targets a profile that resolves to a folder
- **THEN** the command SHALL fail stating that only bare `.jsonc` profiles are mutable

#### Scenario: Unknown profile rejects mutation
- **WHEN** a mutating subcommand targets a name that resolves to no profile
- **THEN** the command SHALL fail with `Profile not found: {name}`

#### Scenario: Mutations write raw manifest content only
- **WHEN** a mutating subcommand modifies a bare overlay that declares `includes`
- **THEN** the file SHALL be re-serialised from the raw manifest
- **AND** apps contributed by included profiles SHALL NOT be written into the overlay

### Requirement: Create a New Overlay Profile
`endstate profile new <name> [--from <base>]` SHALL create `<name>.jsonc` in the profiles directory. Without `--from`, the manifest SHALL contain `version`, `name`, and an empty `apps` array. With `--from`, the manifest SHALL additionally declare `includes` containing the base profile name, plus empty `exclude` and `excludeConfigs` arrays. The command SHALL refuse to overwrite an existing file.

#### Scenario: Create a standalone profile
- **WHEN** `endstate profile new my-laptop` runs and no such profile exists
- **THEN** `my-laptop.jsonc` SHALL be created with `version`, `name`, and an empty `apps` array
- **AND** the created path SHALL be reported

#### Scenario: Create an overlay profile from a base
- **WHEN** `endstate profile new win-laptop --from hugo-desktop` runs
- **THEN** the created manifest SHALL declare `includes` containing `hugo-desktop`
- **AND** `exclude` and `excludeConfigs` SHALL be initialised as empty arrays

#### Scenario: Refuse to overwrite an existing profile
- **WHEN** `endstate profile new <name>` targets a name whose `.jsonc` already exists
- **THEN** the command SHALL fail with `Profile already exists: <path>`
- **AND** the existing file SHALL NOT be modified

#### Scenario: Missing name is a usage error
- **WHEN** `endstate profile new` runs with no name
- **THEN** the command SHALL exit 1
- **AND** with `--json` the envelope SHALL carry error code `MISSING_NAME`

### Requirement: Append Exclusions, Config Exclusions, and Apps Idempotently
`endstate profile exclude <name> <id>...` SHALL append winget IDs to `exclude`, `endstate profile exclude-config <name> <id>...` SHALL append config module IDs to `excludeConfigs`, and `endstate profile add <name> <id>...` SHALL append app entries to `apps`. Each subcommand SHALL accept multiple IDs, SHALL skip entries already present, and SHALL report the count of newly added entries. A missing target array SHALL be initialised before appending.

#### Scenario: Append multiple exclusions
- **WHEN** `endstate profile exclude win-laptop Seagate.SeaTools Apple.AppleSoftwareUpdate` runs
- **THEN** both IDs SHALL be appended to `exclude`
- **AND** the reported added count SHALL be 2

#### Scenario: Duplicate entries are skipped
- **WHEN** an ID already present in `exclude` is passed again
- **THEN** it SHALL NOT be appended a second time
- **AND** it SHALL NOT be counted as added

#### Scenario: Append config exclusions
- **WHEN** `endstate profile exclude-config win-laptop powertoys windows-terminal` runs
- **THEN** both module IDs SHALL be appended to `excludeConfigs`

#### Scenario: Added app entry carries id and windows ref
- **WHEN** `endstate profile add win-laptop Adobe.Lightroom` runs
- **THEN** an entry SHALL be appended to `apps` with `id` set to `Adobe.Lightroom`
- **AND** `refs.windows` set to `Adobe.Lightroom`

#### Scenario: App already present by windows ref is skipped
- **WHEN** `endstate profile add` names an ID that an existing entry already declares as `refs.windows`
- **THEN** no duplicate entry SHALL be appended

#### Scenario: Missing arguments are a usage error
- **WHEN** a mutating subcommand runs without a profile name or without at least one ID
- **THEN** the command SHALL exit 1
- **AND** with `--json` the envelope SHALL carry error code `MISSING_ARGS`

#### Scenario: Missing array is initialised before appending
- **WHEN** the target profile has no `exclude` field at all
- **THEN** `exclude` SHALL be initialised to an empty array before the IDs are appended

### Requirement: Show a Profile Summary
`endstate profile show <name>` SHALL report a summary of the named profile containing `name`, `format`, `base`, `baseAppCount`, `excludedCount`, `excludedConfigsCount`, `addedCount`, and `netAppCount`. Overlay fields SHALL be read from the raw manifest, while `netAppCount` SHALL be derived from the fully resolved manifest so composition is reflected. `show` SHALL work for any resolvable profile format, not only bare profiles.

#### Scenario: Summary reports composition counts
- **WHEN** `endstate profile show win-laptop` runs against an overlay with a base, exclusions, and local apps
- **THEN** `base` SHALL be the first `includes` entry
- **AND** `excludedCount`, `excludedConfigsCount`, and `addedCount` SHALL reflect the raw manifest arrays
- **AND** `netAppCount` SHALL reflect the resolved app list after composition

#### Scenario: Base app count is derived from the resolved list
- **WHEN** a summary is produced for a profile that declares `includes`
- **THEN** `baseAppCount` SHALL be the resolved app count minus the locally declared app count
- **AND** SHALL never be negative

#### Scenario: Show works for non-bare profiles
- **WHEN** `endstate profile show <name>` targets a zip or folder profile
- **THEN** the summary SHALL be produced with `format` set accordingly

#### Scenario: Show of an unknown profile fails
- **WHEN** `endstate profile show <name>` names a profile that cannot be resolved
- **THEN** the command SHALL fail with `Profile not found: {name}`

### Requirement: List Discoverable Profiles
`endstate profile list` SHALL enumerate every profile in the profiles directory across all three storage formats — zip bundles, folders containing `manifest.jsonc`, and bare `.jsonc` files. Each entry SHALL report `name`, `type`, `format`, and `path`. When one name exists in more than one format, the zip, folder, bare precedence SHALL apply and the name SHALL be listed once. Results SHALL be sorted by name.

#### Scenario: All three formats are discovered
- **WHEN** the profiles directory holds a zip bundle, a folder with `manifest.jsonc`, and a bare `.jsonc`
- **THEN** all three SHALL be listed with formats `zip`, `folder`, and `bare` respectively

#### Scenario: Duplicate names collapse by precedence
- **WHEN** the same profile name exists as both a zip and a bare `.jsonc`
- **THEN** the name SHALL appear once, reported as the zip bundle

#### Scenario: Bare profiles are typed as overlay or bare
- **WHEN** a bare `.jsonc` profile declares a non-empty `includes` array
- **THEN** its `type` SHALL be `overlay`
- **AND** a bare profile without `includes` SHALL have type `bare`

#### Scenario: Unreadable bare profile does not abort the listing
- **WHEN** a bare `.jsonc` profile cannot be parsed
- **THEN** it SHALL still be listed as type `bare` with an app count of 0
- **AND** the remaining profiles SHALL still be listed

#### Scenario: Missing profiles directory yields an empty list
- **WHEN** the profiles directory does not exist
- **THEN** `list` SHALL report an empty result rather than failing

#### Scenario: Results are sorted by name
- **WHEN** multiple profiles are discovered
- **THEN** they SHALL be returned sorted by name

### Requirement: Profile Subcommands Emit JSON Envelopes
With `--json`, every profile subcommand SHALL emit a standard JSON envelope instead of human-readable output. Success envelopes SHALL carry the subcommand-specific data payload and exit 0; failures SHALL carry a structured error with a stable code and exit 1. The envelope command name SHALL identify the subcommand (for example `profile new`, `profile exclude`).

#### Scenario: Successful mutation emits a success envelope
- **WHEN** `endstate profile exclude win-laptop Seagate.SeaTools --json` succeeds
- **THEN** the envelope SHALL report success with data containing `name`, `added`, and `ids`
- **AND** the process SHALL exit 0

#### Scenario: Successful creation emits path and base
- **WHEN** `endstate profile new win-laptop --from hugo-desktop --json` succeeds
- **THEN** the envelope data SHALL contain `name`, `path`, and `from`

#### Scenario: Failed mutation emits a structured error envelope
- **WHEN** a profile mutation fails because the profile is not bare
- **THEN** the envelope SHALL report failure with a stable error code such as `PROFILE_EXCLUDE_FAILED`
- **AND** the process SHALL exit 1

#### Scenario: List and show emit structured data
- **WHEN** `endstate profile list --json` or `endstate profile show <name> --json` runs
- **THEN** the envelope data SHALL be the structured profile array or summary object respectively
