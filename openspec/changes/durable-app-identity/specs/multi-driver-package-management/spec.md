## ADDED Requirements

### Requirement: App presence survives loss of package-manager tracking

An app that is installed on the target machine SHALL be reported `present` even when its
recorded package id no longer appears in the driver's ledger, provided the manifest carries
an install fingerprint that matches an entry in the host's installed-software inventory.

Presence SHALL be evaluated in this order:

1. the recorded package id against the driver's ledger (existing behaviour);
2. the recorded install fingerprint against the host inventory, requiring the **uninstall
   key** AND the **publisher** to match.

An app matched by (2) SHALL be planned as `none`, exactly as an app matched by (1), and the
engine SHALL NOT attempt an install for it.

No other evidence SHALL establish presence. In particular the engine SHALL NOT match on
display name alone, SHALL NOT match by prefix or substring, and SHALL NOT consult the config
module catalog's `uninstallDisplayName` patterns for this purpose.

#### Scenario: A browser that self-updated out of winget tracking

- **GIVEN** a manifest recording `Google.Chrome.EXE` with an install fingerprint of key
  `Google Chrome` and publisher `Google LLC`
- **AND** `winget export` does not list `Google.Chrome.EXE`
- **AND** the host inventory contains key `Google Chrome` with publisher `Google LLC`
- **WHEN** the profile is applied
- **THEN** the app is reported `present` with planned action `none`
- **AND** no install is attempted for it

#### Scenario: A chocolatey-installed app outside the choco ledger

- **GIVEN** a manifest recording a chocolatey ref with an install fingerprint
- **AND** `choco list --local-only` does not list that ref
- **AND** the host inventory contains a matching key and publisher
- **WHEN** the profile is applied
- **THEN** the app is reported `present` with planned action `none`

#### Scenario: A genuinely absent app is still installed

- **GIVEN** a manifest recording `Docker.DockerDesktop` with an install fingerprint
- **AND** neither the ledger nor the host inventory matches it
- **WHEN** the profile is applied
- **THEN** the app is reported `missing` with planned action `install`

#### Scenario: A profile captured before fingerprints existed

- **GIVEN** a manifest recording `Brave.Brave` with no install fingerprint
- **AND** `winget export` does not list `Brave.Brave`
- **WHEN** the profile is applied
- **THEN** the app is reported `missing`
- **AND** the engine does not infer an identity it never recorded

### Requirement: Fingerprint matching requires key and publisher together

A fingerprint match SHALL require the uninstall key and the publisher to be equal, compared
case-insensitively after trimming surrounding whitespace. Equality SHALL be full-string;
prefix, suffix and substring matches SHALL NOT satisfy it. A match on one field alone SHALL
NOT establish presence.

Version SHALL NOT participate in identity matching. It is recorded as a separate field and
remains available to version-drift evaluation.

#### Scenario: Same display name, different publisher

- **GIVEN** a manifest fingerprint with key `Everything` and publisher `voidtools`
- **AND** the host inventory contains key `Everything` with publisher `Valve Corporation`
- **WHEN** presence is evaluated
- **THEN** the app is reported `missing`

#### Scenario: A version-bearing display name does not affect the match

- **GIVEN** a manifest fingerprint with key `7-Zip`, publisher `Igor Pavlov`, version `25.01`
- **AND** the host inventory contains key `7-Zip`, publisher `Igor Pavlov`, display name
  `7-Zip 24.09 (x64)` and version `24.09`
- **WHEN** presence is evaluated
- **THEN** the app is reported `present`, because identity excludes the version

### Requirement: Inventory evidence never becomes an install target

The host installed-software inventory SHALL be used only to establish that an app is
present. The engine SHALL NOT derive an installable reference from an inventory entry, and
SHALL NOT attempt to install software identified only by an uninstall key.

#### Scenario: Inventory-only software produces no install

- **GIVEN** a host inventory entry with no corresponding package ref in any driver ledger
- **AND** no manifest entry recording that app
- **WHEN** a plan is produced
- **THEN** no install action referencing that entry is emitted

## MODIFIED Requirements

### Requirement: Capture records app identity

Capture SHALL record, for every app it includes, the package-manager reference it was
discovered under. Capture SHALL additionally record an install fingerprint — uninstall key,
publisher, and version — when the host inventory supplies one for that app.

Absence of a fingerprint SHALL NOT be an error: an app the inventory does not describe is
recorded with its package reference alone, exactly as before.

#### Scenario: Capture records both identities

- **GIVEN** `winget export` lists `Brave.Brave`
- **AND** the host inventory contains key `BraveSoftware Brave-Browser`, publisher
  `Brave Software Inc` and version `150.1.92.144`
- **WHEN** a capture is taken
- **THEN** the manifest entry records the reference `Brave.Brave`
- **AND** records that key, publisher and version as its install fingerprint

#### Scenario: No inventory entry for an app

- **GIVEN** an app present in the driver ledger but absent from the host inventory
- **WHEN** a capture is taken
- **THEN** the manifest entry records the package reference and no fingerprint
- **AND** the capture succeeds

### Requirement: Capture enumerates software outside the driver ledger

Capture SHALL enumerate the union of the driver ledger and the host installed-software
inventory. Software present on the machine but absent from every ledger SHALL NOT be omitted
from a capture.

An inventory entry that resolves to no package reference SHALL be recorded as detected but
not installable; the engine SHALL NOT invent an installable reference for it.

#### Scenario: An app that already left package-manager tracking

- **GIVEN** Chrome is installed and present in the host inventory
- **AND** `winget export` does not list it
- **WHEN** a capture is taken
- **THEN** the capture includes an entry for Chrome
- **AND** that entry carries its install fingerprint

#### Scenario: Inventory-only software is not made installable

- **GIVEN** a host inventory entry that maps to no package reference
- **WHEN** a capture is taken
- **THEN** the entry is recorded as detected but not installable
- **AND** applying the resulting profile attempts no install for it
