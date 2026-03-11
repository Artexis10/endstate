## ADDED Requirements

### Requirement: Go engine implements capture command
The Go engine SHALL implement the `capture` command that takes a system snapshot via `winget list`, converts installed apps to manifest format, and writes the result to a JSONC file. The command SHALL support `--discover`, `--sanitize`, `--update`, `--name`, `--out`, `--profile`, `--include-runtimes`, `--include-store-apps`, and `--minimize` flags. Output SHALL follow the JSON envelope contract and capture-artifact-contract invariants.

#### Scenario: Capture with discover produces manifest
- **WHEN** user runs `endstate capture --discover --json`
- **THEN** the engine takes a system snapshot, converts to manifest format, and returns a success envelope with `outputPath` and `appCount` in the data payload

#### Scenario: Capture with sanitize strips internal fields
- **WHEN** user runs `endstate capture --discover --sanitize --json`
- **THEN** the output manifest has apps sorted alphabetically by id and all underscore-prefixed fields removed

#### Scenario: Capture emits artifact event
- **WHEN** user runs `endstate capture --discover --events jsonl`
- **THEN** an artifact event with `phase: "capture"`, `kind: "manifest"`, and the output path is emitted to stderr

#### Scenario: Capture verifies output file exists (INV-CAPTURE-2)
- **WHEN** capture completes but the manifest file does not exist or is empty
- **THEN** the engine returns `success: false` with error code `MANIFEST_WRITE_FAILED`

### Requirement: Go engine implements plan command
The Go engine SHALL implement the `plan` command that loads a manifest, detects each app's installation status via the driver, and returns a structured plan showing which apps need installation and which are already present.

#### Scenario: Plan shows mixed status
- **WHEN** user runs `endstate plan --manifest <path> --json` with some apps installed and some missing
- **THEN** the envelope data contains `actions` with `status: "skip"` for installed apps and `status: "install"` for missing apps, plus a `summary` with counts

#### Scenario: Plan with empty manifest
- **WHEN** user runs `endstate plan --manifest <path> --json` with a manifest that has zero apps
- **THEN** the envelope data contains an empty actions array and summary with all zeros

### Requirement: Go engine implements report command
The Go engine SHALL implement the `report` command that reads run history from the state directory and returns structured report data. The command SHALL support `--latest`, `--last N`, and `--run-id` filters.

#### Scenario: Report with --latest returns most recent run
- **WHEN** user runs `endstate report --latest --json` with run history present
- **THEN** the envelope data contains a `reports` array with exactly one entry for the most recent run

#### Scenario: Report with no history returns empty
- **WHEN** user runs `endstate report --latest --json` with no run history
- **THEN** the envelope data contains an empty `reports` array

### Requirement: Go engine implements doctor command
The Go engine SHALL implement the `doctor` command that checks system prerequisites (winget availability, PowerShell availability, profiles directory, state directory, engine version) and returns structured results.

#### Scenario: Doctor returns check results
- **WHEN** user runs `endstate doctor --json`
- **THEN** the envelope data contains a `checks` array with status `"pass"`, `"fail"`, or `"warn"` for each check, and a `summary` with total/pass/fail/warn counts

### Requirement: Go engine implements profile subcommands
The Go engine SHALL implement `profile list`, `profile path`, and `profile validate` subcommands per profile-contract.md. Discovery SHALL follow the priority order: zip bundle > loose folder > bare manifest. Display labels SHALL resolve in order: `.meta.json` displayName > manifest `name` field > filename stem.

#### Scenario: Profile list discovers valid profiles
- **WHEN** user runs `endstate profile list --json` with profiles in the profiles directory
- **THEN** the envelope data contains a `profiles` array with path, name, displayName, appCount, and valid fields for each discovered profile

#### Scenario: Profile validate checks manifest validity
- **WHEN** user runs `endstate profile validate <path> --json`
- **THEN** the envelope data contains `valid` boolean, `errors` array, and `summary` with appCount/hasRestore/hasVerify fields

#### Scenario: Profile path resolves name to path
- **WHEN** user runs `endstate profile path <name> --json`
- **THEN** the envelope data contains `path` and `exists` fields

### Requirement: Go engine implements state persistence
The Go engine SHALL persist run results to `state/runs/<runId>.json` using the atomic temp+rename write pattern. State reads SHALL handle missing files gracefully (return empty/default state).

#### Scenario: State write is atomic
- **WHEN** a run result is saved to state
- **THEN** the file is written to a `.tmp` path first, then renamed to the target path

#### Scenario: Missing state returns default
- **WHEN** state is read from a non-existent path
- **THEN** a default empty state is returned without error

### Requirement: Go engine implements system snapshot
The Go engine SHALL take system snapshots by parsing `winget list --source winget` tabular output into structured app data (Name, ID, Version, Source). The snapshot SHALL handle winget unavailability, empty output, and malformed lines gracefully.

#### Scenario: Snapshot parses winget output
- **WHEN** `winget list` returns tabular output with installed apps
- **THEN** each app is parsed into a SnapshotApp with Name, ID, Version, and Source fields

#### Scenario: Snapshot handles winget unavailable
- **WHEN** winget binary is not found
- **THEN** the snapshot returns a WINGET_NOT_AVAILABLE error
