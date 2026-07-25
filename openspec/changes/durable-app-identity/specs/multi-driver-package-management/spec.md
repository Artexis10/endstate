## ADDED Requirements

### Requirement: App presence survives loss of package-manager tracking

An app that is installed on the target machine SHALL be reported `present` even
when its recorded package id no longer appears in the driver's ledger, provided
the engine holds ledger-independent evidence that the app is installed.

Presence detection SHALL consider, in order:

1. the recorded package id against the driver's ledger (existing behaviour);
2. the app's recorded installed display name, when the manifest carries one,
   against the display names the driver observes for installed software that is
   outside its ledger (on Windows, the ARP entries `winget list` reports);
3. for manifests captured before display names were recorded, the module
   catalog's `uninstallDisplayName` matchers for the module whose
   `matches.winget` (or `matches.chocolatey`) contains the recorded package id.

An app matched by (2) or (3) SHALL be planned as `none`, exactly as an app
matched by (1). The engine SHALL NOT plan an install for it.

#### Scenario: A browser that self-updated out of winget tracking

- **GIVEN** a manifest recording `Google.Chrome.EXE` with installed display name
  `Google Chrome`
- **AND** `winget export` does not list `Google.Chrome.EXE`
- **AND** `winget list` reports an entry named `Google Chrome` with id
  `ARP\Machine\X86\Google Chrome`
- **WHEN** the profile is applied
- **THEN** the app is reported `present` with planned action `none`
- **AND** no install is attempted for it

#### Scenario: A pre-existing profile carrying no display name

- **GIVEN** a manifest recording `Brave.Brave` and no installed display name
- **AND** `winget export` does not list `Brave.Brave`
- **AND** `winget list` reports an ARP entry named `Brave`
- **AND** the module `apps.brave` declares `uninstallDisplayName: ["^Brave"]`
- **WHEN** the profile is applied
- **THEN** the app is reported `present` with planned action `none`

#### Scenario: A genuinely absent app is still installed

- **GIVEN** a manifest recording `Docker.DockerDesktop`
- **AND** neither the ledger nor any observed display name matches it
- **WHEN** the profile is applied
- **THEN** the app is reported `missing` with planned action `install`

### Requirement: Display-name matching is anchored and never substring

Display-name comparison SHALL be full-string and case-insensitive after
trimming surrounding whitespace. A display name SHALL NOT match another by
prefix, suffix, or containment.

Catalog `uninstallDisplayName` values SHALL be applied as the anchored regular
expressions they are authored as, unchanged.

#### Scenario: A shorter name does not match a longer one

- **GIVEN** a manifest recording an app with installed display name `Git`
- **AND** the only installed software observed is named `GitHub Desktop`
- **WHEN** presence is evaluated
- **THEN** the app is reported `missing`

### Requirement: A ledger-independent identity never becomes an install target

Evidence obtained from a display-name match SHALL only be used to establish that
an app is present. The engine SHALL NOT derive an installable reference from a
display name, and SHALL NOT attempt to install software identified only by
display name.

#### Scenario: Presence evidence cannot produce an install

- **GIVEN** an observed installed app named `Google Chrome` with no package id
  in any driver ledger
- **AND** no manifest entry recording that app
- **WHEN** a plan is produced
- **THEN** no install action referencing `Google Chrome` is emitted

## MODIFIED Requirements

### Requirement: Capture records app identity

Capture SHALL record, for every app it includes, the package-manager reference
it was discovered under. Capture SHALL additionally record the installed display
name observed for that app when the enumeration supplies one.

The display name is recorded as observed, without normalisation beyond trimming
surrounding whitespace, so that it can be compared against what the same
enumeration reports on a different machine.

Absence of a display name SHALL NOT be an error: an app whose enumeration
supplied no name is recorded with its package reference alone, exactly as before.

#### Scenario: Capture records both identities

- **GIVEN** `winget export` lists `Brave.Brave`
- **AND** `winget list` reports its name as `Brave`
- **WHEN** a capture is taken
- **THEN** the manifest entry records the reference `Brave.Brave`
- **AND** records the installed display name `Brave`

#### Scenario: Enumeration supplies no name

- **GIVEN** an app whose enumeration supplies an empty display name
- **WHEN** a capture is taken
- **THEN** the manifest entry records the package reference and no display name
- **AND** the capture succeeds
