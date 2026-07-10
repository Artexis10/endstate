## ADDED Requirements

### Requirement: capture --pin records installed versions

`capture --pin` SHALL write, for each emitted winget app whose installed version the package-manager snapshot exposes, that version into the app's manifest `version` field. Version recording SHALL be best-effort: an app whose version the snapshot does not expose SHALL be emitted without a `version` field, and a missing version or a failed snapshot SHALL NOT fail the capture. Without `--pin`, a fresh capture SHALL emit no `version` fields on the winget path and its output SHALL be unchanged; `--update` merge preservation is governed by the following requirement.

#### Scenario: Pin writes installed versions into the manifest
- **WHEN** `capture --pin` runs on the winget backend and an app's installed version is known
- **THEN** the emitted manifest entry for that app carries that version in its `version` field

#### Scenario: Unknown version is omitted, not fatal
- **WHEN** `capture --pin` runs and an app appears in the export set but exposes no version in the installed-apps snapshot
- **THEN** that app is emitted without a `version` field
- **AND** the capture succeeds

#### Scenario: Snapshot failure degrades to no versions
- **WHEN** `capture --pin` runs and the installed-apps snapshot fails entirely
- **THEN** the capture succeeds with no `version` fields emitted

#### Scenario: Without the flag, output is unchanged
- **WHEN** `capture` runs without `--pin`
- **THEN** no winget app entry carries a `version` field, exactly as before

#### Scenario: Sanitized capture keeps versions
- **WHEN** `capture --pin --sanitize` runs
- **THEN** emitted apps keep their `version` fields while machine-identifying fields are stripped as before

#### Scenario: Realizer capture is unaffected by the flag
- **WHEN** `capture --pin` runs on a realizer backend that already records versions unconditionally
- **THEN** the flag is accepted and capture behavior is unchanged

### Requirement: capture --update preserves declared versions

`capture --update` SHALL preserve each existing manifest app's `version` and `driver` fields through the merge. Under `--update --pin`, the engine SHALL refresh an existing app's `version` to the installed version only when the installed-apps snapshot exposes a non-empty version for it; an existing pin SHALL NOT be blanked because the snapshot exposes no version. Newly appended apps SHALL receive versions only under `--pin`, best-effort.

#### Scenario: Update without pin preserves existing pins
- **WHEN** `capture --update` runs without `--pin` over a manifest whose apps declare `version` or `driver` values
- **THEN** those values survive the merge unchanged

#### Scenario: Update with pin refreshes to the installed version
- **WHEN** `capture --update --pin` runs and an existing app's installed version differs from its declared pin
- **THEN** the merged entry carries the installed version

#### Scenario: A pin is never blanked by a missing version
- **WHEN** `capture --update --pin` runs and an existing pinned app exposes no version in the installed-apps snapshot
- **THEN** the merged entry keeps its existing pin

#### Scenario: New apps under update with pin get versions
- **WHEN** `capture --update --pin` appends an app that was not in the existing manifest
- **THEN** the appended entry carries its installed version when known

### Requirement: Pin support is advertised as a capability

The capabilities envelope SHALL list `--pin` in `commands.capture.flags` so clients can gate a pinned-capture option on engine support.

#### Scenario: Capabilities advertises the flag
- **WHEN** a client invokes `capabilities --json`
- **THEN** `commands.capture.flags` includes `--pin`
