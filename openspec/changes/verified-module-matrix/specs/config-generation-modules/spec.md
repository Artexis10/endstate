## ADDED Requirements

### Requirement: Studio One Generation Validates Its Core Settings File

The `apps.studio-one` `preferences/g1` generation SHALL validate the officially documented core settings file within its captured settings root and SHALL preserve the released pre-validation fingerprint as an explicitly accepted historical source identity.

#### Scenario: Studio One generation is loaded after validation is added

- **WHEN** the production catalog loads `apps.studio-one` generation `preferences/g1`
- **THEN** the generation SHALL declare `file-exists` validation at `settings/Studio One.settings`
- **AND** its current semantic fingerprint SHALL differ from `abc0141add2928b64ab8fd6b82319c2f57fb086c1ed4b16b776b991d22882444`
- **AND** it SHALL list that released fingerprint in `acceptsSourceFingerprints`
- **AND** generation history SHALL register both the released and current fingerprints in sorted order
