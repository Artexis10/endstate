## ADDED Requirements

### Requirement: Go binary produces valid JSON envelopes on stdout
The Go binary SHALL produce single-line compressed JSON envelopes on stdout when the `--json` flag is present. The envelope MUST contain all fields defined in cli-json-contract.md: schemaVersion, cliVersion, command, runId, timestampUtc, success, data, error.

#### Scenario: Capabilities envelope
- **WHEN** the user runs `endstate capabilities --json`
- **THEN** stdout contains exactly one line of valid JSON with schemaVersion="1.0", cliVersion matching VERSION file, command="capabilities", success=true, and data containing the capabilities response

#### Scenario: Verify envelope with missing manifest
- **WHEN** the user runs `endstate verify --manifest nonexistent.jsonc --json`
- **THEN** stdout contains a JSON envelope with success=false and error.code="MANIFEST_NOT_FOUND"

#### Scenario: RunId format
- **WHEN** any command produces an envelope
- **THEN** the runId field matches the pattern `<command>-YYYYMMDD-HHMMSS-<HOSTNAME>`

### Requirement: Go binary emits NDJSON events on stderr
The Go binary SHALL emit NDJSON events (one JSON object per line) to stderr when the `--events jsonl` flag is present. Each event MUST contain version (integer 1), runId, timestamp (RFC3339), and event (type string) fields.

#### Scenario: Events emitted during apply
- **WHEN** the user runs `endstate apply --manifest profile.jsonc --dry-run --json --events jsonl`
- **THEN** stderr contains NDJSON lines, each parseable as JSON, with phase events preceding item events and summary events closing each phase

#### Scenario: No events without flag
- **WHEN** the user runs `endstate apply --manifest profile.jsonc --dry-run --json` without `--events jsonl`
- **THEN** stderr contains no NDJSON event lines

### Requirement: JSONC manifest loading with includes
The Go binary SHALL load JSONC manifests by stripping `//` line comments and `/* */` block comments before JSON parsing. It SHALL resolve `includes` arrays by loading referenced files relative to the manifest's directory and merging apps arrays. It SHALL detect circular includes and return MANIFEST_PARSE_ERROR.

#### Scenario: Load commented JSONC manifest
- **WHEN** a manifest contains `//` and `/* */` comments
- **THEN** the manifest loads successfully with comments stripped

#### Scenario: Circular includes detected
- **WHEN** manifest A includes manifest B and manifest B includes manifest A
- **THEN** loading returns an error with code MANIFEST_PARSE_ERROR and a message indicating circular includes

#### Scenario: Includes resolution merges apps
- **WHEN** a manifest includes another manifest that has additional apps
- **THEN** the loaded manifest contains apps from both the parent and included manifest

### Requirement: Capabilities command matches gui-integration-contract
The `capabilities` command SHALL return a response matching gui-integration-contract.md exactly, including supportedSchemaVersions (min/max), commands map with supported and flags, features map, and platform info.

#### Scenario: Full capabilities response
- **WHEN** the user runs `endstate capabilities --json`
- **THEN** data contains supportedSchemaVersions with min="1.0" and max="1.0", commands map with entries for capabilities/apply/verify/capture/plan/restore/report/doctor, features with streaming/parallelInstall/configModules/jsonOutput, and platform with os="windows" and drivers=["winget"]

### Requirement: Verify command checks app installation status
The `verify` command SHALL load a manifest, check each app's installation status via the winget driver, and return a results array with type/id/ref/status/message for each app and a summary with total/pass/fail counts.

#### Scenario: Verify with all apps missing
- **WHEN** the user runs `endstate verify --manifest profile.jsonc --json` and no apps are installed
- **THEN** the envelope data contains results where each app has status="fail" and summary.fail equals total app count

#### Scenario: Verify emits events per app
- **WHEN** verify runs with `--events jsonl`
- **THEN** stderr contains a phase event for "verify", item events for each app, and a summary event with counts

### Requirement: Apply command with plan/apply/verify phases
The `apply` command SHALL execute three phases: plan (detect missing apps), apply (install missing apps via winget), verify (re-check all apps). Each phase SHALL emit phase/item/summary events. The envelope SHALL contain dryRun, manifest info, summary, and actions array.

#### Scenario: Dry-run skips apply phase
- **WHEN** the user runs `endstate apply --manifest profile.jsonc --dry-run --json`
- **THEN** the envelope has dryRun=true, the plan phase runs, the apply phase is skipped, and actions show status="to_install" for missing apps

#### Scenario: Apply installs missing apps
- **WHEN** the user runs `endstate apply --manifest profile.jsonc --json` and some apps are not installed
- **THEN** winget install is invoked for each missing app, and the envelope actions reflect install results (installed/failed/skipped)

### Requirement: Profile validation per profile-contract
The Go binary SHALL validate manifest profiles: file must exist, must be parseable JSON/JSONC, version field must be number 1, apps field must be array. Validation errors SHALL use codes from profile-contract.md.

#### Scenario: Missing version field
- **WHEN** a manifest has no version field
- **THEN** validation returns error code MISSING_VERSION

#### Scenario: String version rejected
- **WHEN** a manifest has version as string "1" instead of number 1
- **THEN** validation returns error code INVALID_VERSION_TYPE

### Requirement: Winget driver detect and install
The winget driver SHALL detect installed apps via `winget list --id <ref> -e` (exit code 0 = installed) and install apps via `winget install --id <ref> --accept-source-agreements --accept-package-agreements -e --silent`. It SHALL only activate on Windows (runtime.GOOS == "windows").

#### Scenario: Detect installed app
- **WHEN** `winget list --id <ref> -e` returns exit code 0
- **THEN** Detect returns true

#### Scenario: Install already-installed app
- **WHEN** `winget install` returns exit code -1978335189
- **THEN** Install returns status="present" reason="already_installed"

#### Scenario: Winget not available
- **WHEN** winget binary is not found on the system
- **THEN** operations return WINGET_NOT_AVAILABLE error
