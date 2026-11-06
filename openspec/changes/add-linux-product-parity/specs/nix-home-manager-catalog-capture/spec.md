## MODIFIED Requirements

### Requirement: Capture records the engine-provisioned home-manager catalog settings
The engine SHALL recover declared Home Manager catalog settings from matching provisioning provenance. New records SHALL identify the generated configuration and a source manifest with a digest of the declared settings, without embedding the settings payload. For new records, capture SHALL recover settings only when the source declaration matches that digest and the recorded activation still owns the active configuration. Legacy embedded settings SHALL remain readable.

Output-backed declaration recovery SHALL require the observed native generation
and exact output to match the completion record. The newest failed activation
record for that output SHALL invalidate its historical completion. Output-only
ownership across native generations SHALL remain separate from completed
declaration recovery.

Linux capture SHALL project recovered declarations through selection, sanitization,
and share boundaries before its first output write. Registry program, package,
and module associations SHALL identify selected declarations without granting
live collection authority. Historical file source paths SHALL NOT become private
collection plans. Portable file references SHALL identify staged, authorized
payloads. Retained program declarations SHALL withhold competing live file plans.
Unrestricted private capture MAY retain opaque config or flake compatibility;
selected capture SHALL omit opaque declarations that it cannot scope.

#### Scenario: Selected history retains its program without unrelated paths
- **WHEN** matching history contains selected and unrelated program declarations, arbitrary file sources, and secret references
- **THEN** Linux capture SHALL preserve only the selected associated program declarations
- **AND** update SHALL NOT use retained unrelated apps to enlarge that scope
- **AND** capture SHALL NOT read historical file sources as payload authority

#### Scenario: Sanitized and shared history obey output boundaries
- **WHEN** Linux capture sanitizes or shares matching historical settings
- **THEN** sanitized capture SHALL omit the complete historical Home Manager block
- **AND** share capture SHALL omit historical inline material outside the existing payload redaction pipeline
- **AND** independent selected package and authorized payload capture SHALL remain available

#### Scenario: Captured manifest carries the declared settings
- **WHEN** a matching active generated configuration has an unchanged source declaration
- **THEN** capture SHALL include its declared `homeManager.settings`
- **AND** legacy records MAY supply their embedded declared settings through the existing compatibility path

#### Scenario: Settings take precedence over config and flake in capture
- **WHEN** the engine recovers a verified settings declaration
- **THEN** capture SHALL emit those settings instead of the generated config or flake

#### Scenario: No recorded settings falls through to config and flake
- **WHEN** provenance identifies a directly declared config or flake without settings
- **THEN** capture SHALL preserve that declared input through its existing compatibility path
- **AND** a generated machine-local flake SHALL NOT be used as a portable fallback

#### Scenario: Capture then apply re-activates the same settings configuration
- **WHEN** capture recovers a matching settings declaration and the recipient applies it with configuration enabled
- **THEN** apply SHALL resolve that declaration through the current immutable-input contract

#### Scenario: A new generation selects an older known output
- **WHEN** an uncertain failed transition selects an output with an older successful completion record
- **THEN** capture SHALL NOT treat that older native generation as proof of the selected activation's completion
- **AND** it SHALL omit the declaration with an unavailable-provenance warning

#### Scenario: Failed completion cannot revive through another generation
- **WHEN** the newest record for an active output reports a failed activation
- **THEN** capture SHALL NOT revive an older successful declaration for that output
- **AND** exact-output provenance MAY still establish native ownership

#### Scenario: Uncertain rollback retains prior completion failure
- **WHEN** a started rollback has an unreadable result and matching pre-attempt completion provenance is available
- **THEN** its failure record SHALL invalidate that historical completion without asserting an unobserved post-attempt identity
- **AND** preliminary lookup failure SHALL NOT add a refusal to an explicitly requested rollback

### Requirement: Settings capture is best-effort and non-destructive
Missing, changed, or unreadable source declarations SHALL NOT fabricate settings or export generated machine-local flakes. Capture SHALL report unavailable declaration provenance and continue with independent package and live-settings evidence. Unreadable legacy history SHALL preserve the existing best-effort behavior.

#### Scenario: Provisioning history unreadable — settings omitted, capture succeeds
- **WHEN** provisioning history is unreadable and useful package or live-settings evidence exists
- **THEN** capture SHALL omit the unavailable Home Manager declaration and preserve that useful evidence

#### Scenario: No settings in history — config or flake preserved
- **WHEN** history records a directly declared config or flake without settings
- **THEN** capture SHALL preserve that declared input through the existing compatibility path

#### Scenario: Source settings changed after activation
- **WHEN** a settings source is absent or no longer matches the recorded digest
- **THEN** capture SHALL omit that declaration and report its unavailable provenance
- **AND** it SHALL NOT substitute a newer declaration or the generated machine-local flake

### Requirement: Apply records the declared settings in the provisioning generation
After a generated settings activation, new provisioning records SHALL retain its source reference, settings digest, platform/config identity, effective inputs, and native activation identity. They SHALL NOT embed the settings payload. Existing records with embedded settings SHALL remain readable.

#### Scenario: Generation records the declared settings on a settings apply
- **WHEN** apply activates a generated settings configuration
- **THEN** its generation SHALL record the declaration reference and digest with its effective activation identity
- **AND** it SHALL omit the declared settings payload

#### Scenario: Config and flake generation records are unchanged
- **WHEN** apply activates a directly declared config or flake
- **THEN** its generation SHALL retain the declared input and distinguish it from generated runtime state
- **AND** it SHALL omit an embedded settings payload
