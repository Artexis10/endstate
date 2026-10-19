## ADDED Requirements

### Requirement: Capture can be limited to a subset of detected apps

`capture --only <id[,id,...]>` SHALL limit the capture to the listed selection. Filtering SHALL be applied after capture IDs are final and before any merge with an existing manifest, so every downstream stage — duplicate warnings, version-pin warnings, item events, counts, the manifest write, and config module matching — operates on the selected set. Omitting `--only` SHALL leave behavior unchanged.

`totalFound` SHALL continue to report what was detected on the machine, and deselected apps SHALL be counted as skipped, so `totalFound` equals `included` plus `skipped`.

#### Scenario: Only the selected app is captured

- **WHEN** two apps are detected and `capture --only <first app id>` runs
- **THEN** the written manifest contains exactly that app
- **AND** `totalFound` reports two while `included` reports one

#### Scenario: Selection narrows a run, never an existing manifest

- **WHEN** `capture --only <app> --update --manifest <existing>` runs against a manifest declaring an unrelated app
- **THEN** the unrelated app remains in the manifest
- **AND** the selected app is added
- **AND** other detected but unselected apps are absent

#### Scenario: Realizer hosts honour the selection

- **WHEN** `capture --only <app>` runs on a host using a platform realizer
- **THEN** the captured app set is filtered the same way

### Requirement: Selection tokens are namespaced by kind

A bare `--only` token SHALL select a detected app by its capture id. A token prefixed `apps.` SHALL select a config module by its catalog id. A bare token SHALL NOT be interpreted as a config module id.

#### Scenario: A bare token selects an app

- **WHEN** `--only vscode` is parsed
- **THEN** `vscode` is treated as an app id and not as a config module id

#### Scenario: A prefixed token selects a config module

- **WHEN** `--only git-git,apps.vscode` is parsed
- **THEN** `git-git` is an app selection and `apps.vscode` is a config module selection

### Requirement: Capture selection is validated before anything is written

A selection SHALL be rejected with `MANIFEST_VALIDATION_ERROR` before any manifest or bundle is written when: it names app ids that were not detected; it names config module ids absent from the catalog; it is empty after normalisation; or it names config modules without naming any app.

A capture must contain at least one app, so a module-only selection has no valid result.

#### Scenario: An undetected app id is rejected

- **WHEN** `capture --only <detected app>,<undetected app>` runs
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR` naming the undetected id
- **AND** no manifest file is written

#### Scenario: An unknown config module id is rejected

- **WHEN** `capture --only <detected app>,apps.<unknown module>` runs
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR` naming the unknown module id

#### Scenario: A module-only selection is rejected

- **WHEN** `capture --only apps.<module>` runs with no app token
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR`
- **AND** no manifest file is written

#### Scenario: A blank selection is rejected

- **WHEN** `capture --only "  ,  "` runs
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR`

### Requirement: A selection scopes which config modules attach

Under an active selection, a config module SHALL attach only when a selected app matches it by package reference, or when the module is named outright. A module matched solely by `matches.pathExists` SHALL NOT attach.

The path-existence matcher tests the filesystem independently of the app list, so a selection that scoped apps alone would attach configs belonging to apps the user did not select — disclosing them to anyone the resulting artifact is given to.

#### Scenario: A filesystem-matched module stays out of a selection

- **WHEN** `capture --only <app>` runs with a catalog containing a module that matches only via an existing path
- **THEN** that module's config is not attached

#### Scenario: A module can be requested by name

- **WHEN** `capture --only <app>,apps.<path-matched module>` runs
- **THEN** that module's config is attached

#### Scenario: Unfiltered capture is unaffected

- **WHEN** capture runs without `--only`
- **THEN** path-existence matching continues to attach modules as before

### Requirement: Rebuild propagates a selection to apply

`rebuild --only <id[,id,...]>` SHALL pass the selection to the underlying apply so installs, config restore, and verification are scoped alike, letting a recipient take part of a shared capture bundle.

#### Scenario: Rebuild scopes to the selection

- **WHEN** `rebuild --from <bundle> --only <app> --dry-run` runs
- **THEN** the apply result reflects only the selected app

#### Scenario: A valueless selection is rejected

- **WHEN** `rebuild --from <bundle> --only` is invoked with no value
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR`

### Requirement: Subset support is advertised as a capability

The capabilities envelope SHALL list `--only` in `commands.capture.flags` and `commands.rebuild.flags` so clients can gate a selection UI on engine support. It SHALL NOT advertise flags the CLI does not accept.

#### Scenario: Capabilities advertises the flag

- **WHEN** a client invokes `capabilities --json`
- **THEN** `commands.capture.flags` and `commands.rebuild.flags` each include `--only`

#### Scenario: Unhandled flags are not advertised

- **WHEN** a client invokes `capabilities --json`
- **THEN** `commands.restore.flags` does not include `--filter`, which the CLI never accepted
