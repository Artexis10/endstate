## MODIFIED Requirements

### Requirement: Cross-driver capture identity is conservative
Capture SHALL use `(driver, ref)` as package identity. Display-name, ref, or version similarity across different drivers MUST NOT remove an entry. Case-insensitive exact equality of non-empty display names across drivers SHALL emit `possible_duplicate` while preserving both entries; other fuzzy similarity SHALL emit no automatic conclusion. Colliding manifest app IDs SHALL receive deterministic driver-derived suffixes.

One package manager can list one installed program under two sources, for example winget under both `winget` and `msstore`. When two rows of the same driver carry install fingerprints whose `key`, `publisher`, and `version` are all non-empty and equal, capture SHALL keep one entry: the default-source entry when one exists, because it installs without a Microsoft Store sign-in, and otherwise the first row in capture order. The kept entry SHALL count once in `totalFound` and `included`. A row without a full fingerprint SHALL NOT be collapsed, and rows from different drivers SHALL NOT be collapsed.

#### Scenario: Same ref text from different drivers remains distinct
- **WHEN** Chocolatey and Winget enumerate the same ref text
- **THEN** capture retains both entries because their driver identities differ

#### Scenario: Equal display names warn without suppression
- **WHEN** entries from different drivers have equal non-empty display names ignoring case
- **THEN** capture preserves both entries and emits `possible_duplicate`

#### Scenario: Merely similar names are not classified
- **WHEN** cross-driver display names are not exactly equal ignoring case
- **THEN** capture preserves both entries without a fuzzy duplicate conclusion

#### Scenario: One installed program across winget sources is captured once
- **WHEN** winget lists `Microsoft.PowerToys` from the `winget` source and `XP89DCGQ3K6VLD` from the `msstore` source with the same full install fingerprint
- **THEN** the manifest contains only the `Microsoft.PowerToys` entry
- **AND** `totalFound` and `included` count the program once

#### Scenario: A partial or different fingerprint keeps both rows
- **WHEN** two winget rows have fingerprints that differ in key, publisher, or version, or a fingerprint part is empty
- **THEN** capture keeps both rows
