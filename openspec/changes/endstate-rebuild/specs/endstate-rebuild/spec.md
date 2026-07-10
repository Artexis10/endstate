## ADDED Requirements

### Requirement: Rebuild from a bundle runs the full fresh-machine pipeline

`endstate rebuild --from <bundle.zip>` SHALL compose the existing pipeline in one command: extract the bundle to a temporary directory, install the manifest's apps, restore configuration (unless disabled), verify the resulting state, and remove the temporary directory after the full pipeline completes. The temporary extraction directory SHALL remain available for the entire install and restore sequence and SHALL be removed only after verification finishes.

#### Scenario: Bundle rebuild installs, restores, and verifies
- **WHEN** `rebuild --from MyProfile.zip --confirm` is run against a bundle whose module payloads were captured under `configs/<module>/`
- **THEN** the bundle SHALL be extracted to a temporary directory
- **AND** the manifest's apps SHALL be installed
- **AND** configuration SHALL be restored from the extracted payloads
- **AND** the resulting state SHALL be verified
- **AND** the result data SHALL carry the apply and verify summaries

#### Scenario: Temporary extraction directory is cleaned up after the pipeline
- **WHEN** a bundle rebuild finishes (whether the run succeeded or an install error occurred mid-pipeline)
- **THEN** the temporary extraction directory SHALL no longer exist

### Requirement: Rebuild from a bare manifest runs the pipeline without extraction

`endstate rebuild --from <manifest.jsonc>` SHALL run the same install → restore → verify pipeline directly against the given manifest, with no bundle extraction. The result SHALL indicate that no bundle was extracted.

#### Scenario: Bare manifest rebuild installs without extraction
- **WHEN** `rebuild --from machine.jsonc --confirm` is run against a bare manifest
- **THEN** the manifest's apps SHALL be installed and the state verified
- **AND** the result SHALL report no extracted bundle

### Requirement: A live rebuild requires confirmation before any mutation

A rebuild that is neither `--dry-run` nor `--no-restore` and does not pass `--confirm` SHALL fail with error code `CONFIRMATION_REQUIRED` before extracting, planning, installing, or restoring anything. The remediation SHALL direct the operator to re-run with `--confirm` or `--dry-run`.

#### Scenario: Live rebuild without confirmation is refused with no side effects
- **WHEN** `rebuild --from MyProfile.zip` is run without `--confirm`, `--dry-run`, or `--no-restore`
- **THEN** the run SHALL fail with `CONFIRMATION_REQUIRED`
- **AND** no apps SHALL be installed and no bundle SHALL be extracted

#### Scenario: Dry-run and no-restore lanes need no confirmation
- **WHEN** `rebuild --from MyProfile.zip --dry-run` or `rebuild --from MyProfile.zip --no-restore` is run without `--confirm`
- **THEN** the run SHALL proceed without a confirmation error

### Requirement: The no-restore lane installs without touching configuration

`rebuild --from <path> --no-restore` SHALL install apps and verify state but SHALL NOT restore configuration. The result SHALL report restore as disabled.

#### Scenario: No-restore leaves configuration targets untouched
- **WHEN** `rebuild --from MyProfile.zip --no-restore` is run
- **THEN** apps SHALL be installed
- **AND** configuration restore targets SHALL NOT be written
- **AND** the result SHALL report restore as disabled

### Requirement: The dry-run lane previews without executing or verifying

`rebuild --from <path> --dry-run` SHALL preview the plan without installing, restoring, or verifying. The result SHALL carry no verify summary.

#### Scenario: Dry-run performs no installs and no verification
- **WHEN** `rebuild --from MyProfile.zip --dry-run` is run
- **THEN** no apps SHALL be installed
- **AND** the result SHALL carry no verify summary

### Requirement: Restored payloads resolve from the extracted bundle

Restore during a bundle rebuild SHALL resolve each restore `source` (rewritten by capture to `./configs/<module>/<leaf>`) relative to the extracted manifest directory, so the restored file content matches the content captured into the bundle.

#### Scenario: Captured content round-trips to the restore target
- **WHEN** a bundle is created from a module whose captured file has known content, then `rebuild --from <bundle.zip> --confirm` is run
- **THEN** the restore target SHALL contain exactly the captured content

### Requirement: URL input is rejected in v0

`rebuild --from <value>` where the value contains a URL scheme (`://`) SHALL fail with error code `NOT_SUPPORTED` and remediation directing the operator to download the bundle and pass a local path. A missing local path SHALL fail with `MANIFEST_NOT_FOUND`; an empty `--from` SHALL fail with `MANIFEST_VALIDATION_ERROR`; a `.zip` with no `manifest.jsonc` SHALL fail with `MANIFEST_PARSE_ERROR`.

#### Scenario: URL input is refused
- **WHEN** `rebuild --from https://example.com/MyProfile.zip` is run
- **THEN** the run SHALL fail with `NOT_SUPPORTED`

#### Scenario: Missing input path is a not-found error
- **WHEN** `rebuild --from ./does-not-exist.zip` is run
- **THEN** the run SHALL fail with `MANIFEST_NOT_FOUND`

### Requirement: Verify failures are data, not command errors

When post-install verification reports drift (missing apps, failed assertions, or version drift), `rebuild` SHALL return a success envelope with exit code 0 and record the failures in the verify summary. Only infrastructure or input errors SHALL flip the envelope to failure.

#### Scenario: A drifted app after install yields a success envelope
- **WHEN** a rebuild installs apps but an app remains undetected at verification
- **THEN** the command SHALL return a success envelope
- **AND** the verify summary SHALL report at least one failure

### Requirement: Rebuild support is advertised as a capability

The capabilities envelope SHALL list `rebuild` in `commands` as supported, with its flag set including `--from`, so clients can gate a one-click rebuild affordance on engine support.

#### Scenario: Capabilities advertises rebuild
- **WHEN** a client invokes `capabilities --json`
- **THEN** `commands.rebuild.supported` SHALL be true
- **AND** `commands.rebuild.flags` SHALL include `--from`
