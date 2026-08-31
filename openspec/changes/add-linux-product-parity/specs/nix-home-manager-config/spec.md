## ADDED Requirements

### Requirement: Engine-generated Home Manager configuration uses immutable compatible inputs
Release-default generated Home Manager configurations SHALL use revision-locked, mutually compatible Nixpkgs and Home Manager inputs owned by the Endstate release. The engine SHALL record the effective inputs used for activation. Environment overrides MAY replace them for advanced use, but the effective resolved inputs SHALL remain inspectable and SHALL NOT be reported as release defaults.

#### Scenario: Release-default catalog activation
- **WHEN** Endstate compiles and activates `homeManager.settings` under release defaults
- **THEN** the generated flake SHALL reference the release's immutable Nixpkgs and Home Manager revisions
- **AND** the Provisioning Generation SHALL record those effective inputs

#### Scenario: Curated options are validated against the release pair
- **WHEN** a release includes curated Home Manager settings mappings
- **THEN** required CI SHALL evaluate and activate the full curated corpus against the exact embedded input pair
- **AND** release SHALL fail if an option mapping is incompatible

### Requirement: External Home Manager ownership is preserved
The engine SHALL distinguish an Endstate-owned generated Home Manager activation from an externally owned activation using Endstate provisioning provenance. It SHALL NOT automatically replace, edit, or compose into an externally owned configuration. A requested Endstate-generated activation on such a target SHALL return an inspectable ownership action, including an importable generated module or an explicitly supported safe-restore fallback.

#### Scenario: External activation has no Endstate provenance
- **WHEN** a live Home Manager activation exists and no matching Endstate Provisioning Generation owns it
- **THEN** the engine SHALL classify it as externally owned
- **AND** it SHALL NOT run a replacing `home-manager switch` automatically

#### Scenario: Endstate activation advances normally
- **WHEN** the current Home Manager activation is linked to Endstate provisioning provenance
- **THEN** a consented settings apply MAY advance it through the existing generated configuration path
- **AND** the new generation and effective inputs SHALL be recorded

#### Scenario: Dry run reveals ownership action
- **WHEN** a dry run targets an externally owned Home Manager configuration
- **THEN** the plan SHALL identify the ownership conflict and generated import artifact or fallback
- **AND** it SHALL make no configuration changes
